package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"kiosk/controller/internal/assets"
	"kiosk/controller/internal/config"
	"kiosk/controller/internal/pad/session"
)

func TestPadSessionCommands(t *testing.T) {
	fake := newPadRails(t)
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "store.db"),
		ServerURL:       fake.url,
		ActiveAccountID: "acct",
		Accounts: []config.AccountConfig{{
			ID:          "acct",
			Name:        "Lobby",
			KioskID:     "k1",
			DeviceToken: "device-token",
		}},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)

	registry := NewRegistry(cfg)
	registry.padDSUAddrs = [2]string{"127.0.0.1:0", "127.0.0.1:0"}
	var events []session.Snapshot
	var eventMu sync.Mutex
	registry.SetEmitter(func(event string, data any) {
		if event != "pad.players" {
			return
		}
		snap, ok := data.(session.Snapshot)
		if !ok {
			t.Errorf("pad.players data %T", data)
			return
		}
		eventMu.Lock()
		events = append(events, snap)
		eventMu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry.BindPadLifecycle(ctx)

	empty, err := registry.Dispatch("pad.players", nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap := empty.(session.Snapshot); snap.Session != nil || len(snap.Players) != 0 {
		t.Fatalf("empty %+v", snap)
	}

	if _, err := registry.Dispatch("pad.sensitive", json.RawMessage(`{"active":true}`)); err != nil {
		t.Fatal(err)
	}

	started, err := registry.Dispatch("pad.session.start", nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := started.(session.Snapshot)
	if snap.Session == nil || snap.Session.Code != "KQXM" || snap.Session.JoinURL == "" {
		t.Fatalf("started %+v", snap)
	}
	if !registry.padMgr.Sensitive() {
		t.Fatal("sensitive flag was not applied to the session")
	}
	if registry.padRouter == nil || !registry.padRouter.Sensitive() {
		t.Fatal("sensitive flag was not applied to the input hub")
	}
	if _, err := registry.Dispatch("pad.sensitive", json.RawMessage(`{"active":false}`)); err != nil {
		t.Fatal(err)
	}
	if registry.padRouter.Sensitive() {
		t.Fatal("hub kept the sensitive flag")
	}
	waitPause(t, registry.AssetSyncer(), true)

	again, err := registry.Dispatch("pad.session.start", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.(session.Snapshot).Session.Code != "KQXM" {
		t.Fatalf("reuse %+v", again)
	}
	if fake.creates() != 1 {
		t.Fatalf("creates %d", fake.creates())
	}

	if _, err := registry.Dispatch("pad.lobby", json.RawMessage(`{"open":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch("pad.kick", json.RawMessage(`{"player_id":"p1"}`)); err != nil {
		t.Fatal(err)
	}
	if !fake.saw("PATCH /api/kiosk/play_sessions/sess-1") || !fake.saw("POST /api/kiosk/play_sessions/sess-1/players/p1/kick") {
		t.Fatalf("calls %+v", fake.calls)
	}

	ended, err := registry.Dispatch("pad.session.end", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ended.(session.Snapshot).Session != nil {
		t.Fatalf("ended %+v", ended)
	}
	if !fake.saw("DELETE /api/kiosk/play_sessions/sess-1") {
		t.Fatalf("missing delete %+v", fake.calls)
	}
	waitPause(t, registry.AssetSyncer(), false)

	eventMu.Lock()
	n := len(events)
	eventMu.Unlock()
	if n == 0 {
		t.Fatal("expected pad.players events")
	}

}

func TestPadSessionStartIsSerialized(t *testing.T) {
	fake := newPadRails(t)
	fake.createDelay = 300 * time.Millisecond
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "store.db"),
		ServerURL:       fake.url,
		ActiveAccountID: "acct",
		Accounts: []config.AccountConfig{{
			ID: "acct", Name: "Lobby", KioskID: "k1", DeviceToken: "device-token",
		}},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)
	registry := NewRegistry(cfg)
	registry.padDSUAddrs = [2]string{"127.0.0.1:0", "127.0.0.1:0"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry.BindPadLifecycle(ctx)

	// Two taps while Create is still in flight: one from the Home action and
	// one from the Apps page.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = registry.Dispatch("pad.session.start", nil)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fake.creates() != 1 {
		t.Fatalf("creates = %d, want 1", fake.creates())
	}

	if _, err := registry.Dispatch("pad.session.end", nil); err != nil {
		t.Fatal(err)
	}
	waitPause(t, registry.AssetSyncer(), false)
	cancel()
	registry.WaitPadStopped(2 * time.Second)
}

func waitPause(t *testing.T, syncer *assets.Syncer, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := false
		for _, reason := range syncer.Status().PausedReasons {
			if reason == playPauseReason {
				got = true
			}
		}
		if got == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("pause %v, reasons %+v", want, syncer.Status().PausedReasons)
}

type padRails struct {
	url         string
	mu          sync.Mutex
	calls       []string
	n           int
	srv         *httptest.Server
	createDelay time.Duration
}

func newPadRails(t *testing.T) *padRails {
	t.Helper()
	f := &padRails{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/kiosk/play_sessions", f.create)
	mux.HandleFunc("/api/kiosk/play_sessions/", f.member)
	mux.HandleFunc("/cable", f.cable)
	f.srv = httptest.NewServer(mux)
	f.url = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

func (f *padRails) creates() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

func (f *padRails) saw(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if len(call) >= len(prefix) && call[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (f *padRails) note(r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer device-token" {
		return false
	}
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	return true
}

func (f *padRails) create(w http.ResponseWriter, r *http.Request) {
	if !f.note(r) {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	f.n++
	delay := f.createDelay
	f.mu.Unlock()
	time.Sleep(delay)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(session.SessionRecord{
		ID: "sess-1", Code: "KQXM", QRKey: "cXIta2V5LWJ5dGVzLWZvci1zaGFyZWQtdmVjdG9yISE=",
		JoinBaseURL: f.url + "/play/KQXM",
	})
}

func (f *padRails) member(w http.ResponseWriter, r *http.Request) {
	if !f.note(r) {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/ice"):
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ice_servers": []any{}, "ttl": 86400})
	case r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session.SessionRecord{
			ID: "sess-1", Code: "KQXM", QRKey: "cXIta2V5LWJ5dGVzLWZvci1zaGFyZWQtdmVjdG9yISE=",
			JoinBaseURL: f.url + "/play/KQXM",
		})
	case r.Method == http.MethodPatch:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session.SessionRecord{
			ID: "sess-1", Code: "KQXM", QRKey: "cXIta2V5LWJ5dGVzLWZvci1zaGFyZWQtdmVjdG9yISE=",
			JoinBaseURL: f.url + "/play/KQXM", LobbyOpen: true,
		})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *padRails) cable(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer device-token" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var cmd struct {
			Command    string `json:"command"`
			Identifier string `json:"identifier"`
		}
		if json.Unmarshal(data, &cmd) != nil || cmd.Command != "subscribe" {
			continue
		}
		_ = conn.WriteJSON(map[string]string{"type": "confirm_subscription", "identifier": cmd.Identifier})
	}
}
