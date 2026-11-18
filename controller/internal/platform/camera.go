package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// Apps reach the kiosk's camera through the overlay's getUserMedia call. A
// kiosk has nobody to click Chrome's "Allow camera" bubble, so the call would
// hang. The controller grants video capture (and nothing else: no microphone)
// to the page's origin over CDP. The platform's own permission prompt and the
// "camera on" indicator are what the people at the kiosk actually see.

// pageOrigin is the origin of an http(s) page URL, or "" for anything else
// (about:blank, chrome://, file:).
func pageOrigin(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ensureCameraGrant grants video capture to the target's origin once per
// (target, origin); it runs on every injection tick, so a new tab, a
// navigation to another origin or a Chrome restart is picked up.
func (r *Runtime) ensureCameraGrant(ctx context.Context, target cdpTarget) {
	origin := pageOrigin(target.URL)
	if origin == "" {
		return
	}
	key := target.ID + " " + origin
	if r.cameraGrantKey == key {
		return
	}
	if err := grantVideoCapture(ctx, r.cfg.PlatformDebugPort, origin); err != nil {
		log.Printf("camera permission not granted for %s: %v", origin, err)
		return
	}
	r.cameraGrantKey = key
	log.Printf("camera (video capture) granted to %s", origin)
}

// grantVideoCapture calls Browser.grantPermissions on the browser endpoint.
func grantVideoCapture(ctx context.Context, port int, origin string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("query cdp version: %w", err)
	}
	defer resp.Body.Close()
	var info struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.WebSocketDebuggerURL == "" {
		return fmt.Errorf("browser has no debugger websocket")
	}

	conn, _, err := websocket.DefaultDialer.DialContext(reqCtx, info.WebSocketDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("connect cdp browser: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	return cdpCall(conn, 1, "Browser.grantPermissions", map[string]any{
		"origin":      origin,
		"permissions": []string{"videoCapture"},
	})
}
