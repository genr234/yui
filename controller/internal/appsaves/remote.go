// Package appsaves keeps app save blobs (contracts §6 "App saves"): small
// opaque files an app stores for the kiosk's account or for a player's
// account, with a local copy and an upload queue for when Rails is out of
// reach.
package appsaves

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// MaxBlobBytes is the size limit of one blob.
	MaxBlobBytes = 8 << 20
	// TimeFormat is how updated_at is written (UTC, milliseconds).
	TimeFormat = "2006-01-02T15:04:05.000Z"
)

// Error is a bridge error whose message starts with a code, so a caller can
// switch on the prefix (like the profile errors).
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

// Is matches on the code, so a copy with another message still matches.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

var (
	ErrNotLinked  = &Error{"not_linked", "this player is not playing as a Yui account"}
	ErrNotGranted = &Error{"not_granted", "this player has not allowed this app to keep saves"}
	ErrTooLarge   = &Error{"too_large", "a save can hold at most 8 MiB"}
	ErrTooMany    = &Error{"too_many", "an app can keep at most 64 saves per account"}
	ErrInvalidKey = &Error{"invalid_key", "keys use a-z, 0-9, '.', '_' and '-' (1-80 characters, starting with a letter or digit)"}
	ErrOffline    = &Error{"offline", "this kiosk has no Yui account connected, or it cannot be reached"}

	// ErrNotFound is the remote's 404 for a missing blob (or, in the player
	// scope, a player who is not in the kiosk's active session).
	ErrNotFound = errors.New("appsaves: not found")
	// ErrNewer means Rails holds a newer blob than the one uploaded (409).
	ErrNewer = errors.New("appsaves: the account has a newer save")
	// ErrNoAccount means the remote has no account to talk to.
	ErrNoAccount = errors.New("appsaves: no active account")
)

// Meta describes one stored blob.
type Meta struct {
	Key       string `json:"key"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// Remote is the Rails side. playerID "" is the kiosk scope. Errors other than
// the typed ones mean "could not reach Rails" and make the caller go offline.
type Remote interface {
	List(ctx context.Context, appID, playerID string) ([]Meta, error)
	// Get returns found=false for a missing blob.
	Get(ctx context.Context, appID, key, playerID string) (data []byte, meta Meta, found bool, err error)
	// Put uploads data. A zero updatedAt lets the server stamp it.
	Put(ctx context.Context, appID, key, playerID string, data []byte, updatedAt time.Time) (Meta, error)
	Delete(ctx context.Context, appID, key, playerID string) error
}

// HTTPRemote talks to /api/kiosk/app_saves with the kiosk's bearer token.
type HTTPRemote struct {
	// Endpoint returns the server URL and device token, or ok=false.
	Endpoint func() (baseURL, token string, ok bool)
	Client   *http.Client
}

func (h *HTTPRemote) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func (h *HTTPRemote) request(ctx context.Context, method, appID, key, playerID string, body io.Reader) (*http.Request, error) {
	base, tok, ok := h.Endpoint()
	if !ok || base == "" || tok == "" {
		return nil, ErrNoAccount
	}
	p := "/api/kiosk/app_saves/" + url.PathEscape(appID)
	if key != "" {
		p += "/" + url.PathEscape(key)
	}
	if playerID != "" {
		p += "?player_id=" + url.QueryEscape(playerID)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+p, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return req, nil
}

// statusError maps a refusal to its typed error; other statuses are plain
// errors (the caller treats them as "unreachable").
func statusError(what string, resp *http.Response, playerID string) error {
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(text, &body)
	switch {
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case resp.StatusCode == http.StatusConflict && body.Error == "too_many":
		return ErrTooMany
	case resp.StatusCode == http.StatusConflict && body.Error == "newer_save_exists":
		return ErrNewer
	case resp.StatusCode == http.StatusConflict && body.Error == "not_linked":
		return ErrNotLinked
	case resp.StatusCode == http.StatusForbidden && body.Error == "not_granted":
		return ErrNotGranted
	case resp.StatusCode == http.StatusUnprocessableEntity && body.Error == "invalid_key":
		return ErrInvalidKey
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	}
	return fmt.Errorf("%s: HTTP %d %s", what, resp.StatusCode, strings.TrimSpace(string(text)))
}

func (h *HTTPRemote) List(ctx context.Context, appID, playerID string) ([]Meta, error) {
	req, err := h.request(ctx, http.MethodGet, appID, "", playerID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError("list saves", resp, playerID)
	}
	var payload struct {
		Saves []Meta `json:"saves"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode saves list: %w", err)
	}
	return payload.Saves, nil
}

func (h *HTTPRemote) Get(ctx context.Context, appID, key, playerID string) ([]byte, Meta, bool, error) {
	req, err := h.request(ctx, http.MethodGet, appID, key, playerID, nil)
	if err != nil {
		return nil, Meta{}, false, err
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return nil, Meta{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := statusError("get save", resp, playerID)
		if errors.Is(err, ErrNotFound) {
			return nil, Meta{}, false, nil
		}
		return nil, Meta{}, false, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBlobBytes+1))
	if err != nil {
		return nil, Meta{}, false, err
	}
	if len(data) > MaxBlobBytes {
		return nil, Meta{}, false, ErrTooLarge
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if want := resp.Header.Get("X-Save-Sha256"); want != "" && !strings.EqualFold(want, hash) {
		return nil, Meta{}, false, errors.New("get save: checksum mismatch")
	}
	return data, Meta{Key: key, Size: int64(len(data)), SHA256: hash, UpdatedAt: resp.Header.Get("X-Updated-At")}, true, nil
}

func (h *HTTPRemote) Put(ctx context.Context, appID, key, playerID string, data []byte, updatedAt time.Time) (Meta, error) {
	req, err := h.request(ctx, http.MethodPut, appID, key, playerID, bytes.NewReader(data))
	if err != nil {
		return Meta{}, err
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	if !updatedAt.IsZero() {
		req.Header.Set("X-Updated-At", updatedAt.UTC().Format(TimeFormat))
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return Meta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return Meta{}, statusError("put save", resp, playerID)
	}
	var meta Meta
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&meta); err != nil {
		return Meta{}, fmt.Errorf("decode save: %w", err)
	}
	meta.Key = key
	return meta, nil
}

func (h *HTTPRemote) Delete(ctx context.Context, appID, key, playerID string) error {
	req, err := h.request(ctx, http.MethodDelete, appID, key, playerID, nil)
	if err != nil {
		return err
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	}
	err = statusError("delete save", resp, playerID)
	if errors.Is(err, ErrNotFound) {
		return nil // nothing to delete
	}
	return err
}
