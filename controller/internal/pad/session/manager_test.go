package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"kiosk/controller/internal/pad/protocol"
)

type savedSignal struct {
	To     string
	Type   string
	Action string
	Data   json.RawMessage
}

type fakeRails struct {
	srv     *httptest.Server
	token   string
	mu      sync.Mutex
	sess    SessionRecord
	calls   []string
	bodies  []string
	ident   string
	conn    *websocket.Conn
	writeMu sync.Mutex
	sigs    []savedSignal
	reject  bool
	status  int
}

func newFake(t *testing.T) *fakeRails {
	t.Helper()
	f := &fakeRails{
		token: "device-token",
		sess: SessionRecord{
			ID: "sess-1", Code: "KQXM", QRKey: sharedQRB64,
			LobbyOpen: false,
		},
		status: http.StatusCreated,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/kiosk/play_sessions", f.create)
	mux.HandleFunc("/api/kiosk/play_sessions/", f.member)
	mux.HandleFunc("/cable", f.cable)
	f.srv = httptest.NewServer(mux)
	f.sess.JoinBaseURL = f.srv.URL + "/play/KQXM"
	t.Cleanup(func() {
		f.mu.Lock()
		conn := f.conn
		f.conn = nil
		f.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		f.srv.Close()
	})
	return f
}

func (f *fakeRails) manager(t *testing.T) *Manager {
	t.Helper()
	mgr, err := Open(Config{
		ServerURL:    f.srv.URL,
		DeviceToken:  f.token,
		KioskName:    "Lobby",
		PingInterval: time.Hour,
		Now:          func() time.Time { return time.Unix(sharedQRAt, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func (f *fakeRails) auth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (f *fakeRails) note(r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.bodies = append(f.bodies, string(body))
	f.mu.Unlock()
}

func (f *fakeRails) create(w http.ResponseWriter, r *http.Request) {
	if !f.auth(w, r) {
		return
	}
	f.note(r)
	f.mu.Lock()
	status := f.status
	sess := f.sess
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(sess)
}

func (f *fakeRails) member(w http.ResponseWriter, r *http.Request) {
	if !f.auth(w, r) {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.bodies = append(f.bodies, string(body))
	f.mu.Unlock()
	switch {
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && len(r.URL.Path) > 4 && r.URL.Path[len(r.URL.Path)-4:] == "/ice":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ICEConfig{IceServers: []ICEServer{}, TTL: 86400})
	case r.Method == http.MethodPatch:
		var payload struct {
			LobbyOpen bool `json:"lobby_open"`
		}
		_ = json.Unmarshal(body, &payload)
		f.mu.Lock()
		f.sess.LobbyOpen = payload.LobbyOpen
		sess := f.sess
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sess)
	case r.Method == http.MethodPost:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeRails) cable(w http.ResponseWriter, r *http.Request) {
	if !f.auth(w, r) {
		return
	}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	f.mu.Lock()
	if f.conn != nil {
		_ = f.conn.Close()
	}
	f.conn = conn
	reject := f.reject
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		if f.conn == conn {
			f.conn = nil
		}
		f.mu.Unlock()
		_ = conn.Close()
	}()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var cmd cableCommand
		if json.Unmarshal(data, &cmd) != nil {
			continue
		}
		switch cmd.Command {
		case "subscribe":
			f.mu.Lock()
			f.ident = cmd.Identifier
			f.mu.Unlock()
			kind := "confirm_subscription"
			if reject {
				kind = "reject_subscription"
			}
			_ = f.writeConn(conn, map[string]string{"type": kind, "identifier": cmd.Identifier})
		case "message":
			var payload struct {
				Action string          `json:"action"`
				To     string          `json:"to"`
				Type   string          `json:"type"`
				Data   json.RawMessage `json:"data"`
			}
			if json.Unmarshal([]byte(cmd.Data), &payload) != nil || payload.Action != "signal" {
				continue
			}
			f.mu.Lock()
			f.sigs = append(f.sigs, savedSignal{To: payload.To, Type: payload.Type, Action: payload.Action, Data: payload.Data})
			f.mu.Unlock()
		}
	}
}

func (f *fakeRails) push(t *testing.T, msg any) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var conn *websocket.Conn
	var ident string
	for {
		f.mu.Lock()
		conn, ident = f.conn, f.ident
		f.mu.Unlock()
		if conn != nil && ident != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cable not subscribed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	env := map[string]any{"identifier": ident, "message": msg}
	if err := f.writeConn(conn, env); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeRails) writeConn(conn *websocket.Conn, v any) error {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	return conn.WriteJSON(v)
}

func (f *fakeRails) waitSignal(t *testing.T, typ string) savedSignal {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		for i, sig := range f.sigs {
			if sig.Type == typ {
				f.sigs = append(f.sigs[:i], f.sigs[i+1:]...)
				f.mu.Unlock()
				return sig
			}
		}
		queued := len(f.sigs)
		f.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for signal %s (%d queued)", typ, queued)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fakeRails) signalCount(typ string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, sig := range f.sigs {
		if sig.Type == typ {
			n++
		}
	}
	return n
}

func (f *fakeRails) hasCall(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if len(call) >= len(prefix) && call[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func playerFixture(id string, slot int, host, connected bool) protocol.Player {
	s := slot
	return protocol.Player{ID: id, Slot: &s, Name: "Ada", Color: "blue", Host: host, Connected: connected}
}

func start(t *testing.T, f *fakeRails, mgr *Manager) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mgr.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if mgr.Active() && f.hasCall("GET /api/kiosk/play_sessions/sess-1/ice") {
			f.mu.Lock()
			ready := f.ident != ""
			f.mu.Unlock()
			if ready {
				return cancel, done
			}
		}
		select {
		case err := <-done:
			t.Fatalf("run ended early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("session did not subscribe")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stop(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
}

func TestSignalingRosterAndQR(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	snap := mgr.Snapshot()
	if snap.Session == nil || snap.Session.Code != "KQXM" {
		t.Fatalf("snapshot %+v", snap)
	}
	if snap.Session.JoinURL != f.srv.URL+"/play/KQXM?t="+sharedQRTok {
		t.Fatalf("join url %s", snap.Session.JoinURL)
	}

	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 2, true, true)})
	offer := f.waitSignal(t, signalOffer)
	if offer.To != "p1" || offer.Action != "signal" {
		t.Fatalf("offer %+v", offer)
	}
	var sdp struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}
	if json.Unmarshal(offer.Data, &sdp) != nil || sdp.Type != "offer" || sdp.SDP == "" {
		t.Fatalf("sdp %s", offer.Data)
	}

	mgr.mu.Lock()
	peer := mgr.peers["p1"]
	mgr.mu.Unlock()
	if peer == nil || !peer.control.Ordered() || peer.control.MaxRetransmits() != nil {
		t.Fatal("control channel should be ordered and reliable")
	}
	retries := peer.input.MaxRetransmits()
	if peer.input.Ordered() || retries == nil || *retries != 0 || peer.input.Label() != "input" || peer.control.Label() != "control" {
		t.Fatal("input channel should be unordered with maxRetransmits 0")
	}

	f.push(t, map[string]any{"type": "hello", "from": "p1", "data": map[string]any{}})
	time.Sleep(150 * time.Millisecond)
	if got := f.signalCount(signalOffer); got != 0 {
		t.Fatalf("hello while connecting sent %d extra offers", got)
	}

	slot := 2
	updated := playerFixture("p1", 2, false, true)
	updated.Color = "teal"
	f.push(t, map[string]any{"type": "player_updated", "player": updated})
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap = mgr.Snapshot()
		if len(snap.Players) == 1 && snap.Players[0].Color == "teal" && snap.Players[0].Host == false && snap.Players[0].Slot != nil && *snap.Players[0].Slot == slot {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("updated snapshot %+v", snap.Players)
		}
		time.Sleep(10 * time.Millisecond)
	}

	left := playerFixture("p1", 2, false, false)
	f.push(t, map[string]any{"type": "player_left", "player": left})
	deadline = time.Now().Add(2 * time.Second)
	for {
		snap = mgr.Snapshot()
		if len(snap.Players) == 1 && !snap.Players[0].Connected && snap.Players[0].Slot != nil && *snap.Players[0].Slot == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("left snapshot %+v", snap.Players)
		}
		time.Sleep(10 * time.Millisecond)
	}

	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 2, false, true)})
	again := f.waitSignal(t, signalOffer)
	if again.To != "p1" {
		t.Fatalf("rejoin offer %+v", again)
	}
	snap = mgr.Snapshot()
	if len(snap.Players) != 1 || snap.Players[0].Slot == nil || *snap.Players[0].Slot != 2 {
		t.Fatalf("rejoin kept slot: %+v", snap.Players)
	}
}

func TestICEFallbackControl(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 1, true, true)})
	f.waitSignal(t, signalOffer)
	mgr.mu.Lock()
	gen := mgr.peers["p1"].gen
	mgr.mu.Unlock()
	mgr.noteICEFailed("p1", gen)

	hello := protocol.Hello{
		T: "hello", V: 1,
		Caps:   protocol.Caps{Motion: true, Haptics: false, WakeLock: true},
		Screen: protocol.Screen{W: 390, H: 844, Orientation: "portrait"},
	}
	f.push(t, map[string]any{"from": "p1", "type": "control", "data": hello})
	sig := f.waitSignal(t, signalControl)
	var welcome protocol.Welcome
	if err := json.Unmarshal(sig.Data, &welcome); err != nil {
		t.Fatal(err)
	}
	if welcome.T != "welcome" || welcome.V != 1 || welcome.KioskName != "Lobby" || welcome.Player.ID != "p1" || welcome.Player.Slot == nil || *welcome.Player.Slot != 1 {
		t.Fatalf("welcome %+v", welcome)
	}
	if _, err := protocol.ParseControl(sig.Data); err != nil {
		t.Fatal(err)
	}
}

func TestLobbyKickHostAndStopDeletes(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	cancel, done := start(t, f, mgr)

	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 1, true, true)})
	f.waitSignal(t, signalOffer)

	ctx := context.Background()
	if err := mgr.SetLobbyOpen(ctx, true); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	open := f.sess.LobbyOpen
	f.mu.Unlock()
	if !open {
		t.Fatal("lobby not open")
	}
	if err := mgr.TransferHost(ctx, "p2"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Kick(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if !f.hasCall("POST /api/kiosk/play_sessions/sess-1/players/p2/host") || !f.hasCall("POST /api/kiosk/play_sessions/sess-1/players/p1/kick") {
		t.Fatalf("calls %+v", f.calls)
	}
	bye := f.waitSignal(t, signalBye)
	if bye.To != "p1" {
		t.Fatalf("bye %+v", bye)
	}
	if players := mgr.Snapshot().Players; len(players) != 0 {
		t.Fatalf("players after kick %+v", players)
	}

	mgr.SetSensitive(true)
	if !mgr.Sensitive() {
		t.Fatal("sensitive flag")
	}

	stop(t, cancel, done)
	if !f.hasCall("DELETE /api/kiosk/play_sessions/sess-1") {
		t.Fatalf("missing delete: %+v", f.calls)
	}
	if mgr.Active() || mgr.Snapshot().Session != nil {
		t.Fatal("session still active")
	}
}

func TestSubscriptionRejectedDoesNotDelete(t *testing.T) {
	f := newFake(t)
	f.reject = true
	mgr := f.manager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := mgr.Run(ctx)
	if !errors.Is(err, errSubscriptionRejected) {
		t.Fatalf("err %v", err)
	}
	if f.hasCall("DELETE /api/kiosk/play_sessions/sess-1") {
		t.Fatal("rejected subscribe deleted the session")
	}
}

func TestUnauthorized(t *testing.T) {
	f := newFake(t)
	mgr, err := Open(Config{ServerURL: f.srv.URL, DeviceToken: "nope", KioskName: "Lobby"})
	if err != nil {
		t.Fatal(err)
	}
	err = mgr.Run(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err %v", err)
	}
}

func TestStaleInputAndSnapshotJSON(t *testing.T) {
	mgr, err := Open(Config{ServerURL: "http://kiosk.example", DeviceToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	mgr.players["p1"] = &playerState{Player: playerFixture("p1", 1, true, true)}
	first := protocol.EncodeInput(protocol.InputState{Seq: 1, Buttons: protocol.ButtonSouth})
	stale := protocol.EncodeInput(protocol.InputState{Seq: 0, Buttons: protocol.ButtonEast})
	next := protocol.EncodeInput(protocol.InputState{Seq: 2, Buttons: protocol.ButtonWest})
	wrapped := protocol.EncodeInput(protocol.InputState{Seq: 0, Buttons: protocol.ButtonNorth})
	mgr.applyInput("p1", first)
	mgr.applyInput("p1", stale)
	got, ok := mgr.LatestInput("p1")
	if !ok || got.Seq != 1 || got.Buttons != protocol.ButtonSouth {
		t.Fatalf("after stale %+v %v", got, ok)
	}
	mgr.applyInput("p1", next)
	got, _ = mgr.LatestInput("p1")
	if got.Seq != 2 || got.Buttons != protocol.ButtonWest {
		t.Fatalf("next %+v", got)
	}
	mgr.mu.Lock()
	mgr.players["p1"].input.Seq = 65535
	mgr.mu.Unlock()
	mgr.applyInput("p1", wrapped)
	got, _ = mgr.LatestInput("p1")
	if got.Seq != 0 || got.Buttons != protocol.ButtonNorth {
		t.Fatalf("wrap %+v", got)
	}

	raw, err := json.Marshal(mgr.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Session any               `json:"session"`
		Players []protocol.Player `json:"players"`
	}
	if json.Unmarshal(raw, &decoded) != nil || decoded.Session != nil || len(decoded.Players) != 1 {
		t.Fatalf("json %s", raw)
	}
}

func TestQRPublishUsesClock(t *testing.T) {
	mgr, err := Open(Config{
		ServerURL:   "http://kiosk.example",
		DeviceToken: "t",
		Now:         func() time.Time { return time.Unix(sharedQRAt, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.active = true
	mgr.record = &SessionRecord{ID: "sess-1", Code: "KQXM", QRKey: sharedQRB64, JoinBaseURL: "https://yui.example/play/KQXM"}
	if err := mgr.publishQR(); err != nil {
		t.Fatal(err)
	}
	if mgr.Snapshot().Session.JoinURL != "https://yui.example/play/KQXM?t="+sharedQRTok {
		t.Fatalf("url %s", mgr.Snapshot().Session.JoinURL)
	}
	mgr.cfg.Now = func() time.Time { return time.Unix(sharedQRAt+30, 0) }
	if err := mgr.publishQR(); err != nil {
		t.Fatal(err)
	}
	if mgr.token == sharedQRTok {
		t.Fatal("token did not rotate")
	}
}
