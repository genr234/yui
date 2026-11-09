package assets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Save-file backups (GET/PUT /api/kiosk/saves/:app_id/:runtime). The server
// keeps one `other` asset per account and key; PUT replaces it.

var (
	// ErrSaveNewer means the server holds a newer backup than the upload (HTTP 409).
	ErrSaveNewer = errors.New("assets: the account has a newer save backup")
	// ErrSaveTooLarge means the server refused the upload as too large (HTTP 413).
	ErrSaveTooLarge = errors.New("assets: save backup is too large")
)

const (
	saveInfoTimeout     = 20 * time.Second
	saveTransferTimeout = 5 * time.Minute
)

// SaveInfo is the metadata of the backup the account holds.
type SaveInfo struct {
	ID        string
	SHA256    string
	Size      int64
	SavedAt   time.Time
	KioskName string
}

func savePath(appID, runtime string) string {
	return "/api/kiosk/saves/" + url.PathEscape(appID) + "/" + url.PathEscape(runtime)
}

// SaveInfo returns the account's backup for the key, or ok=false when none exists.
func (h *HTTPRemote) SaveInfo(ctx context.Context, appID, runtime string) (SaveInfo, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, saveInfoTimeout)
	defer cancel()
	req, err := h.request(ctx, http.MethodGet, savePath(appID, runtime), nil)
	if err != nil {
		return SaveInfo{}, false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.control().Do(req)
	if err != nil {
		return SaveInfo{}, false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return SaveInfo{}, false, nil
	default:
		return SaveInfo{}, false, httpError("save info", resp)
	}
	var payload struct {
		ID       string `json:"id"`
		SHA256   string `json:"sha256"`
		ByteSize int64  `json:"byte_size"`
		SavedAt  string `json:"saved_at"`
		Metadata struct {
			KioskName string `json:"kiosk_name"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return SaveInfo{}, false, fmt.Errorf("decode save info: %w", err)
	}
	savedAt, err := time.Parse(time.RFC3339, payload.SavedAt)
	if err != nil {
		return SaveInfo{}, false, fmt.Errorf("save info: bad saved_at %q", payload.SavedAt)
	}
	return SaveInfo{
		ID: payload.ID, SHA256: payload.SHA256, Size: payload.ByteSize,
		SavedAt: savedAt.UTC(), KioskName: payload.Metadata.KioskName,
	}, true, nil
}

// SaveDownload streams the backup. The caller closes the body and cancels
// nothing: the transfer is bounded by an internal timeout released on Close.
func (h *HTTPRemote) SaveDownload(ctx context.Context, appID, runtime string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, saveTransferTimeout)
	req, err := h.request(ctx, http.MethodGet, savePath(appID, runtime)+"/download", nil)
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := h.transfer().Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer cancel()
		defer resp.Body.Close()
		return nil, httpError("save download", resp)
	}
	return &cancelBody{ReadCloser: resp.Body, cancel: cancel}, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelBody) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// SaveUpload replaces the account's backup for the key with body (size bytes,
// sha256 hex). savedAt orders backups; the server refuses an older one.
func (h *HTTPRemote) SaveUpload(ctx context.Context, appID, runtime string, body io.Reader, size int64, sha256 string, savedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, saveTransferTimeout)
	defer cancel()
	path := savePath(appID, runtime) + "?saved_at=" + url.QueryEscape(savedAt.UTC().Format(time.RFC3339))
	req, err := h.request(ctx, http.MethodPut, path, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	if sha256 != "" {
		req.Header.Set("X-Save-Sha256", sha256)
	}
	resp, err := h.transfer().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	case http.StatusConflict:
		return ErrSaveNewer
	case http.StatusRequestEntityTooLarge:
		return ErrSaveTooLarge
	default:
		return httpError("save upload", resp)
	}
}
