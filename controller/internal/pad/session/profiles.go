package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Profile errors (plan 10), mapped from the kiosk profile endpoints.
var (
	// ErrNotLinked: the player is not playing as a Yui account (409).
	ErrNotLinked = errors.New("not_linked")
	// ErrNotGranted: the player's account has not allowed this app (403).
	ErrNotGranted = errors.New("not_granted")
	// ErrTooLarge: the profile is over the size limit (413).
	ErrTooLarge = errors.New("too_large")
)

// VersionConflictError is a profile write against a stale version (409).
type VersionConflictError struct{ Version int }

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version_conflict: the profile is at version %d", e.Version)
}

// Profile is GET …/players/:player_id/profiles/:app_id.
type Profile struct {
	ProfileID string          `json:"profile_id"`
	Data      json.RawMessage `json:"data"`
	Version   int             `json:"version"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

// PhoneLinkCode is POST /api/kiosk/phone_link_codes.
type PhoneLinkCode struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}

func profilePath(sessionID, playerID, appID string) string {
	return "/api/kiosk/play_sessions/" + url.PathEscape(sessionID) + "/players/" + url.PathEscape(playerID) +
		"/profiles/" + url.PathEscape(appID)
}

func (c *apiClient) GetProfile(ctx context.Context, sessionID, playerID, appID string) (Profile, error) {
	resp, err := c.do(ctx, http.MethodGet, profilePath(sessionID, playerID, appID), nil)
	if err != nil {
		return Profile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Profile{}, profileError(resp)
	}
	var p Profile
	if err := decodeJSON(resp.Body, &p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// PutProfile writes data at version and returns the new version.
func (c *apiClient) PutProfile(ctx context.Context, sessionID, playerID, appID string, data json.RawMessage, version int) (int, error) {
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	body, err := json.Marshal(struct {
		Data    json.RawMessage `json:"data"`
		Version int             `json:"version"`
	}{data, version})
	if err != nil {
		return 0, err
	}
	resp, err := c.do(ctx, http.MethodPut, profilePath(sessionID, playerID, appID), body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, profileError(resp)
	}
	var out struct {
		Version int `json:"version"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		return 0, err
	}
	return out.Version, nil
}

// profileError maps the documented profile statuses to typed errors.
func profileError(resp *http.Response) error {
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var body struct {
		Error   string `json:"error"`
		Version int    `json:"version"`
	}
	_ = json.Unmarshal(text, &body)
	switch {
	case resp.StatusCode == http.StatusConflict && body.Error == "version_conflict":
		return &VersionConflictError{Version: body.Version}
	case resp.StatusCode == http.StatusConflict && body.Error == "not_linked":
		return ErrNotLinked
	case resp.StatusCode == http.StatusForbidden && body.Error == "not_granted":
		return ErrNotGranted
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	}
	msg := strings.TrimSpace(string(text))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	}
	return fmt.Errorf("pad session: %s: %s", resp.Status, msg)
}

func (c *apiClient) CreatePhoneLinkCode(ctx context.Context) (PhoneLinkCode, error) {
	resp, err := c.do(ctx, http.MethodPost, "/api/kiosk/phone_link_codes", nil)
	if err != nil {
		return PhoneLinkCode{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return PhoneLinkCode{}, httpStatusError(resp)
	}
	var out PhoneLinkCode
	if err := decodeJSON(resp.Body, &out); err != nil {
		return PhoneLinkCode{}, err
	}
	if out.Code == "" {
		return PhoneLinkCode{}, errors.New("pad session: link code response is missing code")
	}
	return out, nil
}

// CreatePhoneLinkCode asks Rails for a one-time code that links a phone to
// the account behind deviceToken. It needs no play session.
func CreatePhoneLinkCode(ctx context.Context, serverURL, deviceToken string) (PhoneLinkCode, error) {
	return newAPI(serverURL, deviceToken, nil).CreatePhoneLinkCode(ctx)
}

// Profile reads a player's profile for an app. The player must be playing as
// an account that allowed the app, or the error is ErrNotLinked or
// ErrNotGranted.
func (m *Manager) Profile(ctx context.Context, playerID, appID string) (Profile, error) {
	id, err := m.sessionID()
	if err != nil {
		return Profile{}, err
	}
	return m.api.GetProfile(ctx, id, playerID, appID)
}

// SetProfile writes data at version. A stale version is a
// *VersionConflictError carrying the current one.
func (m *Manager) SetProfile(ctx context.Context, playerID, appID string, data json.RawMessage, version int) (int, error) {
	id, err := m.sessionID()
	if err != nil {
		return 0, err
	}
	return m.api.PutProfile(ctx, id, playerID, appID, data, version)
}

// PhoneLinkCode asks Rails for a one-time code that links a phone to the
// kiosk's account. It needs no session, only the device token.
func (m *Manager) PhoneLinkCode(ctx context.Context) (PhoneLinkCode, error) {
	return m.api.CreatePhoneLinkCode(ctx)
}
