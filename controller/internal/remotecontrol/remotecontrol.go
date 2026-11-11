// Package remotecontrol lets the control room see and drive the kiosk's
// browser page: screenshots and taps, scrolls, keys and text, through
// Chrome's local debugging port. It only covers what Chrome shows (a native
// app window such as an emulator is not in the picture).
package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Screen is one screenshot of the kiosk page.
type Screen struct {
	Image  string `json:"image"` // data:image/jpeg;base64,...
	Width  int    `json:"width"` // page size in CSS pixels
	Height int    `json:"height"`
	Title  string `json:"title,omitempty"`
	At     string `json:"at"`
}

// Input is one remote action. X and Y are 0..1 of the page size.
type Input struct {
	Action string  `json:"action"` // tap, scroll, key, text
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	DeltaY float64 `json:"dy"`
	Key    string  `json:"key"`
	Text   string  `json:"text"`
}

type target struct {
	Type                 string `json:"type"`
	Title                string `json:"title"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type session struct {
	conn  *websocket.Conn
	next  int
	title string
}

func open(ctx context.Context, port int) (*session, error) {
	if port <= 0 {
		return nil, fmt.Errorf("chrome debugging is turned off on this kiosk")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json", port), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chrome is not reachable: %w", err)
	}
	defer resp.Body.Close()
	var targets []target
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode chrome targets: %w", err)
	}
	for _, t := range targets {
		if t.Type != "page" || t.WebSocketDebuggerURL == "" {
			continue
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, t.WebSocketDebuggerURL, nil)
		if err != nil {
			return nil, fmt.Errorf("attach to chrome page: %w", err)
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetReadDeadline(deadline)
			_ = conn.SetWriteDeadline(deadline)
		}
		return &session{conn: conn, title: t.Title}, nil
	}
	return nil, fmt.Errorf("chrome has no page open")
}

func (s *session) close() { _ = s.conn.Close() }

func (s *session) call(method string, params map[string]any, result any) error {
	s.next++
	id := s.next
	if err := s.conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := s.conn.ReadJSON(&msg); err != nil {
			return err
		}
		if msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		if result != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, result)
		}
		return nil
	}
}

func (s *session) viewport() (float64, float64, error) {
	var metrics struct {
		CSSLayoutViewport struct {
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssLayoutViewport"`
	}
	if err := s.call("Page.getLayoutMetrics", map[string]any{}, &metrics); err != nil {
		return 0, 0, err
	}
	w, h := metrics.CSSLayoutViewport.ClientWidth, metrics.CSSLayoutViewport.ClientHeight
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("chrome reported an empty page")
	}
	return w, h, nil
}

// Capture takes a JPEG of the page, scaled so its long side is at most maxSide.
func Capture(ctx context.Context, port int, maxSide int) (Screen, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := open(ctx, port)
	if err != nil {
		return Screen{}, err
	}
	defer s.close()
	w, h, err := s.viewport()
	if err != nil {
		return Screen{}, err
	}
	scale := 1.0
	if long := math.Max(w, h); maxSide > 0 && long > float64(maxSide) {
		scale = float64(maxSide) / long
	}
	var shot struct {
		Data string `json:"data"`
	}
	err = s.call("Page.captureScreenshot", map[string]any{
		"format": "jpeg", "quality": 60, "fromSurface": true,
		"clip": map[string]any{"x": 0, "y": 0, "width": w, "height": h, "scale": scale},
	}, &shot)
	if err != nil {
		return Screen{}, err
	}
	return Screen{Image: "data:image/jpeg;base64," + shot.Data, Width: int(w), Height: int(h), Title: s.title, At: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

// GameRectExpr reads the rect the overlay publishes on window.__yuiGameRect
// (viewport CSS px: {x,y,w,h}, or null when no game leaves room around it).
// See platform/src/sdk/game-rect.ts.
const GameRectExpr = "JSON.stringify(window.__yuiGameRect||null)"

// Clip is a page region in CSS pixels.
type Clip struct{ X, Y, W, H float64 }

// ClampClip fits a published game rect to the viewport. It returns the whole
// viewport when the rect is missing, malformed or entirely outside.
func ClampClip(raw string, vw, vh float64) Clip {
	whole := Clip{0, 0, vw, vh}
	var c *Clip
	var in struct{ X, Y, W, H *float64 }
	if json.Unmarshal([]byte(raw), &in) != nil || in.X == nil || in.Y == nil || in.W == nil || in.H == nil {
		return whole
	}
	c = &Clip{*in.X, *in.Y, *in.W, *in.H}
	for _, f := range []float64{c.X, c.Y, c.W, c.H} {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return whole
		}
	}
	x0, y0 := math.Max(c.X, 0), math.Max(c.Y, 0)
	x1, y1 := math.Min(c.X+c.W, vw), math.Min(c.Y+c.H, vh)
	if x1-x0 < 8 || y1-y0 < 8 {
		return whole
	}
	return Clip{x0, y0, x1 - x0, y1 - y0}
}

// CaptureGameArea takes a JPEG of the part of the Chrome page that holds the
// game: the overlay's published game rect when there is one, else the whole
// page. The long side is at most maxSide pixels.
func CaptureGameArea(ctx context.Context, port, maxSide, quality int) (Shot, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := open(ctx, port)
	if err != nil {
		return Shot{}, err
	}
	defer s.close()
	w, h, err := s.viewport()
	if err != nil {
		return Shot{}, err
	}
	clip := Clip{0, 0, w, h}
	var ev struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := s.call("Runtime.evaluate", map[string]any{"expression": GameRectExpr, "returnByValue": true}, &ev); err == nil {
		clip = ClampClip(ev.Result.Value, w, h)
	}
	scale := 1.0
	if long := math.Max(clip.W, clip.H); maxSide > 0 && long > float64(maxSide) {
		scale = float64(maxSide) / long
	}
	var shot struct {
		Data string `json:"data"`
	}
	err = s.call("Page.captureScreenshot", map[string]any{
		"format": "jpeg", "quality": quality, "fromSurface": true,
		"clip": map[string]any{"x": clip.X, "y": clip.Y, "width": clip.W, "height": clip.H, "scale": scale},
	}, &shot)
	if err != nil {
		return Shot{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return Shot{}, err
	}
	return Shot{JPEG: raw, W: int(clip.W*scale + 0.5), H: int(clip.H*scale + 0.5)}, nil
}

var keys = map[string]struct {
	code    string
	keyCode int
}{
	"Enter": {"Enter", 13}, "Escape": {"Escape", 27}, "Backspace": {"Backspace", 8}, "Tab": {"Tab", 9},
	"ArrowUp": {"ArrowUp", 38}, "ArrowDown": {"ArrowDown", 40}, "ArrowLeft": {"ArrowLeft", 37}, "ArrowRight": {"ArrowRight", 39},
	" ": {"Space", 32},
}

// Send performs one remote action on the page.
func Send(ctx context.Context, port int, in Input) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, err := open(ctx, port)
	if err != nil {
		return err
	}
	defer s.close()
	switch in.Action {
	case "tap", "scroll":
		if in.X < 0 || in.X > 1 || in.Y < 0 || in.Y > 1 {
			return fmt.Errorf("x and y must be between 0 and 1")
		}
		w, h, err := s.viewport()
		if err != nil {
			return err
		}
		x, y := in.X*w, in.Y*h
		if in.Action == "scroll" {
			return s.call("Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": x, "y": y, "deltaX": 0, "deltaY": in.DeltaY}, nil)
		}
		for _, step := range []map[string]any{
			{"type": "mouseMoved", "x": x, "y": y},
			{"type": "mousePressed", "x": x, "y": y, "button": "left", "clickCount": 1},
			{"type": "mouseReleased", "x": x, "y": y, "button": "left", "clickCount": 1},
		} {
			if err := s.call("Input.dispatchMouseEvent", step, nil); err != nil {
				return err
			}
		}
		return nil
	case "key":
		k, ok := keys[in.Key]
		if !ok {
			return fmt.Errorf("unsupported key %q", in.Key)
		}
		for _, kind := range []string{"rawKeyDown", "keyUp"} {
			if err := s.call("Input.dispatchKeyEvent", map[string]any{"type": kind, "key": in.Key, "code": k.code, "windowsVirtualKeyCode": k.keyCode}, nil); err != nil {
				return err
			}
		}
		return nil
	case "text":
		if in.Text == "" || len(in.Text) > 500 {
			return fmt.Errorf("text must be 1 to 500 characters")
		}
		return s.call("Input.insertText", map[string]any{"text": in.Text}, nil)
	default:
		return fmt.Errorf("unknown action %q", in.Action)
	}
}
