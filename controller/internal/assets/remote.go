package assets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNoAccount is returned by a Remote when there is no active account (or no
// server URL/device token). The syncer idles and never deletes anything.
var ErrNoAccount = errors.New("assets: no active account")

// ManifestItem is one entry of GET /api/kiosk/assets.
type ManifestItem struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Filename  string          `json:"filename"`
	ByteSize  int64           `json:"byte_size"`
	SHA256    string          `json:"sha256"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
	// Remote marks an on-demand asset the server lists but has not fetched
	// yet (no sha256). RemoteState is "remote" or "fetching"; RemoteError is
	// why the last fetch failed. Ask for it with Remote.RequestAsset.
	Remote      bool   `json:"remote,omitempty"`
	RemoteState string `json:"remote_state,omitempty"`
	RemoteError string `json:"remote_error,omitempty"`
}

// Remote is the server side of the asset contract.
type Remote interface {
	Manifest(ctx context.Context) ([]ManifestItem, error)
	// Download streams the file starting at offset. total is the full file
	// size; sha is the X-Asset-Sha256 header (may be empty). The returned
	// body starts exactly at offset.
	Download(ctx context.Context, id string, offset int64) (body io.ReadCloser, total int64, sha string, err error)
	ReportStatus(ctx context.Context, id, state string, bytesDone int64, errText string) error
	// RequestAsset asks the server to fetch an on-demand asset. It returns the
	// server's state: "ready", "fetching" or "remote" (with the last error).
	RequestAsset(ctx context.Context, id string) (state, errText string, err error)
}

// HTTPRemote implements Remote against the Rails kiosk API.
type HTTPRemote struct {
	// Endpoint returns the base URL and device token, or ok=false when there
	// is no active account.
	Endpoint func() (baseURL, token string, ok bool)
	// Control is used for manifest and status calls (should have a timeout).
	Control *http.Client
	// Transfer is used for downloads (should have no overall timeout).
	Transfer *http.Client
}

func (h *HTTPRemote) control() *http.Client {
	if h.Control != nil {
		return h.Control
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (h *HTTPRemote) transfer() *http.Client {
	if h.Transfer != nil {
		return h.Transfer
	}
	return http.DefaultClient
}

func (h *HTTPRemote) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	base, token, ok := h.Endpoint()
	if !ok || base == "" || token == "" {
		return nil, ErrNoAccount
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

func (h *HTTPRemote) Manifest(ctx context.Context) ([]ManifestItem, error) {
	req, err := h.request(ctx, http.MethodGet, "/api/kiosk/assets", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.control().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpError("manifest", resp)
	}
	// Assets is a pointer so a body without the key, or with "assets": null,
	// is told apart from an explicit empty list. Only "assets": [] means the
	// account has no assets; anything else is a manifest error, and the
	// caller then deletes nothing.
	var payload struct {
		Assets *[]ManifestItem `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if payload.Assets == nil {
		return nil, errors.New("manifest: response has no assets list")
	}
	if *payload.Assets == nil {
		return []ManifestItem{}, nil
	}
	return *payload.Assets, nil
}

func (h *HTTPRemote) Download(ctx context.Context, id string, offset int64) (io.ReadCloser, int64, string, error) {
	req, err := h.request(ctx, http.MethodGet, "/api/kiosk/assets/"+url.PathEscape(id)+"/download", nil)
	if err != nil {
		return nil, 0, "", err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := h.transfer().Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	sha := resp.Header.Get("X-Asset-Sha256")
	switch resp.StatusCode {
	case http.StatusOK:
		// Server ignored the Range header: skip the bytes we already have.
		if offset > 0 {
			if _, err := io.CopyN(io.Discard, resp.Body, offset); err != nil {
				resp.Body.Close()
				return nil, 0, "", fmt.Errorf("skip to offset: %w", err)
			}
		}
		return resp.Body, resp.ContentLength, sha, nil
	case http.StatusPartialContent:
		total := int64(-1)
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndex(cr, "/"); i >= 0 {
				if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil {
					total = n
				}
			}
		}
		if total < 0 && resp.ContentLength >= 0 {
			total = offset + resp.ContentLength
		}
		return resp.Body, total, sha, nil
	default:
		defer resp.Body.Close()
		return nil, 0, "", httpError("download", resp)
	}
}

func (h *HTTPRemote) ReportStatus(ctx context.Context, id, state string, bytesDone int64, errText string) error {
	payload := map[string]any{"state": state, "bytes_done": bytesDone, "error": nil}
	if errText != "" {
		payload["error"] = errText
	}
	data, _ := json.Marshal(payload)
	req, err := h.request(ctx, http.MethodPatch, "/api/kiosk/assets/"+url.PathEscape(id)+"/status", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.control().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return httpError("status report", resp)
	}
	return nil
}

func (h *HTTPRemote) RequestAsset(ctx context.Context, id string) (string, string, error) {
	req, err := h.request(ctx, http.MethodPost, "/api/kiosk/assets/"+url.PathEscape(id)+"/request", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := h.control().Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", "", httpError("asset request", resp)
	}
	var body struct {
		State string  `json:"state"`
		Error *string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil {
		return "", "", fmt.Errorf("asset request: %w", err)
	}
	errText := ""
	if body.Error != nil {
		errText = *body.Error
	}
	return body.State, errText, nil
}

func httpError(what string, resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return fmt.Errorf("%s: HTTP %d %s", what, resp.StatusCode, strings.TrimSpace(string(snippet)))
}
