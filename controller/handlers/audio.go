package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"

	"kiosk/controller/internal/pad/session"
)

// Speaker phone audio (plan 06): bridge commands and the long-lived
// session.Audio shared by every play session.
//
//	pad.speaker  { player_id }                → pad.players shape
//	pad.volume   { value: 0..1 }              → audio.status shape
//	audio.route  { source: auto|app|native }  → audio.status shape
//	audio.status                              → audio.status shape
//
// Native-session code redirects capture to its runtime through
// Registry.AudioSource (SetCapturePID on launch, ResetCapture on exit).

type padSpeakerCommand struct{}

func (padSpeakerCommand) Name() string { return "pad.speaker" }
func (padSpeakerCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	id, err := playerIDParam(params)
	if err != nil {
		return nil, err
	}
	mgr, err := r.activePad()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := mgr.SetSpeaker(ctx, id); err != nil {
		return nil, err
	}
	return mgr.Snapshot(), nil
}

type padVolumeCommand struct{}

func (padVolumeCommand) Name() string { return "pad.volume" }
func (padVolumeCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var body struct {
		Value *float64 `json:"value"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return nil, err
	}
	if body.Value == nil {
		return nil, errors.New("value is required (0 to 1)")
	}
	a := r.Audio()
	r.padMu.Lock()
	mgr := r.padMgr
	r.padMu.Unlock()
	if mgr != nil && mgr.Active() {
		// Also echoes the value to the speaker phone's slider.
		if _, err := mgr.SetVolume(*body.Value); err != nil {
			return nil, err
		}
	} else {
		a.SetVolume(*body.Value)
	}
	return a.Status(), nil
}

type audioRouteCommand struct{}

func (audioRouteCommand) Name() string { return "audio.route" }
func (audioRouteCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var body struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return nil, err
	}
	a := r.Audio()
	if err := a.SetRoute(body.Source); err != nil {
		return nil, err
	}
	return a.Status(), nil
}

type audioStatusCommand struct{}

func (audioStatusCommand) Name() string { return "audio.status" }
func (audioStatusCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return r.Audio().Status(), nil
}

// AudioSource is the hook native sessions use to send their runtime's
// sound to the speaker phone instead of the browser's.
func (r *Registry) AudioSource() session.AudioSource { return r.Audio() }

// Audio returns the speaker-phone audio, creating it on first use.
func (r *Registry) Audio() *session.Audio {
	r.padMu.Lock()
	defer r.padMu.Unlock()
	if r.padAudio == nil {
		r.padAudio = session.NewAudio(session.AudioConfig{BrowserPID: r.chromePID})
	}
	return r.padAudio
}

// chromePID finds Chrome's browser process, whose process tree (renderers
// and the audio service) is what process loopback captures for web apps.
// CDP knows the live browser even when the launcher the supervisor started
// has handed off and exited; status.json (written by the supervisor) is
// the fallback.
func (r *Registry) chromePID(ctx context.Context) (int, error) {
	cfg := r.snapshotConfig()
	pid, cdpErr := cdpBrowserPID(ctx, cfg.PlatformDebugPort)
	if cdpErr == nil {
		return pid, nil
	}
	if pid, err := statusChromePID(cfg.StatusPath); err == nil {
		return pid, nil
	}
	return 0, cdpErr
}

func cdpBrowserPID(ctx context.Context, port int) (int, error) {
	if port <= 0 {
		return 0, errors.New("chrome remote debugging is off")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return 0, err
	}
	if version.WebSocketDebuggerURL == "" {
		return 0, errors.New("chrome has no browser debugger url")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, version.WebSocketDebuggerURL, nil)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	if err := conn.WriteJSON(map[string]any{"id": 1, "method": "SystemInfo.getProcessInfo"}); err != nil {
		return 0, err
	}
	for {
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				ProcessInfo []struct {
					Type string `json:"type"`
					ID   int    `json:"id"`
				} `json:"processInfo"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return 0, err
		}
		if msg.ID != 1 {
			continue // an event
		}
		if msg.Error != nil {
			return 0, errors.New("cdp: " + msg.Error.Message)
		}
		for _, p := range msg.Result.ProcessInfo {
			if p.Type == "browser" && p.ID > 0 {
				return p.ID, nil
			}
		}
		return 0, errors.New("cdp: no browser process listed")
	}
}

func statusChromePID(path string) (int, error) {
	if path == "" {
		return 0, errors.New("no status path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var st struct {
		ChromePID int `json:"chrome_pid"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return 0, err
	}
	if st.ChromePID <= 0 {
		return 0, errors.New("status has no chrome pid")
	}
	return st.ChromePID, nil
}
