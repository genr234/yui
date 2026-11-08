package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

// ErrUnauthorized means the device token was rejected.
var ErrUnauthorized = errors.New("pad session: unauthorized")

// SessionRecord is the kiosk play-session JSON (as_kiosk_json).
type SessionRecord struct {
	ID          string            `json:"id"`
	Code        string            `json:"code"`
	QRKey       string            `json:"qr_key"`
	JoinBaseURL string            `json:"join_base_url"`
	LobbyOpen   bool              `json:"lobby_open"`
	Players     []protocol.Player `json:"players"`
}

// ICEConfig is GET /api/kiosk/play_sessions/:id/ice.
type ICEConfig struct {
	IceServers []ICEServer `json:"ice_servers"`
	TTL        int         `json:"ttl"`
}

// ICEServer is one entry. Rails sends urls as an array.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type apiClient struct {
	base   string
	token  string
	client *http.Client
}

func newAPI(base, token string, client *http.Client) *apiClient {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &apiClient{base: strings.TrimRight(base, "/"), token: token, client: client}
}

// Create returns the active session. created is true on 201 and false when
// Rails reused the kiosk's existing session (200).
func (c *apiClient) Create(ctx context.Context) (SessionRecord, bool, error) {
	resp, err := c.do(ctx, http.MethodPost, "/api/kiosk/play_sessions", nil)
	if err != nil {
		return SessionRecord{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return SessionRecord{}, false, httpStatusError(resp)
	}
	var rec SessionRecord
	if err := decodeJSON(resp.Body, &rec); err != nil {
		return SessionRecord{}, false, err
	}
	if rec.ID == "" || rec.Code == "" || rec.QRKey == "" {
		return SessionRecord{}, false, errors.New("pad session: create response is missing id, code, or qr_key")
	}
	return rec, resp.StatusCode == http.StatusCreated, nil
}

// errSessionGone means Rails no longer has this kiosk's session as active.
var errSessionGone = errors.New("pad session: session ended on the server")

// Get fetches the session and its roster. An ended session is errSessionGone.
func (c *apiClient) Get(ctx context.Context, id string) (SessionRecord, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/kiosk/play_sessions/"+url.PathEscape(id), nil)
	if err != nil {
		return SessionRecord{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return SessionRecord{}, errSessionGone
	}
	if resp.StatusCode != http.StatusOK {
		return SessionRecord{}, httpStatusError(resp)
	}
	var rec SessionRecord
	if err := decodeJSON(resp.Body, &rec); err != nil {
		return SessionRecord{}, err
	}
	return rec, nil
}

func (c *apiClient) SetLobbyOpen(ctx context.Context, id string, open bool) (SessionRecord, error) {
	body, _ := json.Marshal(map[string]bool{"lobby_open": open})
	resp, err := c.do(ctx, http.MethodPatch, "/api/kiosk/play_sessions/"+url.PathEscape(id), body)
	if err != nil {
		return SessionRecord{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SessionRecord{}, httpStatusError(resp)
	}
	var rec SessionRecord
	if err := decodeJSON(resp.Body, &rec); err != nil {
		return SessionRecord{}, err
	}
	return rec, nil
}

func (c *apiClient) Destroy(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/api/kiosk/play_sessions/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return httpStatusError(resp)
}

func (c *apiClient) Kick(ctx context.Context, sessionID, playerID string) error {
	return c.postEmpty(ctx, playerActionPath(sessionID, playerID, "kick"))
}

func (c *apiClient) TransferHost(ctx context.Context, sessionID, playerID string) error {
	return c.postEmpty(ctx, playerActionPath(sessionID, playerID, "host"))
}

func (c *apiClient) ICE(ctx context.Context, id string) (ICEConfig, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/kiosk/play_sessions/"+url.PathEscape(id)+"/ice", nil)
	if err != nil {
		return ICEConfig{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ICEConfig{}, httpStatusError(resp)
	}
	var cfg ICEConfig
	if err := decodeJSON(resp.Body, &cfg); err != nil {
		return ICEConfig{}, err
	}
	return cfg, nil
}

func playerActionPath(sessionID, playerID, action string) string {
	return "/api/kiosk/play_sessions/" + url.PathEscape(sessionID) + "/players/" + url.PathEscape(playerID) + "/" + action
}

func (c *apiClient) postEmpty(ctx context.Context, path string) error {
	resp, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return httpStatusError(resp)
	}
	return nil
}

func (c *apiClient) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pad session: %s %s: %w", method, path, err)
	}
	return resp, nil
}

func decodeJSON(r io.Reader, dest any) error {
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("pad session: response: %w", err)
	}
	return nil
}

func httpStatusError(resp *http.Response) error {
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(text))
	err := fmt.Errorf("pad session: %s: %s", resp.Status, msg)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	}
	return err
}
