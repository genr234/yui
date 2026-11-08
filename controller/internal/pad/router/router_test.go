package router

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/pad/cdp"
	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/session"
	"kiosk/controller/internal/pad/shell"
)

type sent struct {
	to  string
	msg any
}

type fakeSession struct {
	mu      sync.Mutex
	sent    []sent
	control func(string, any)
	input   func(string, protocol.InputState)
	player  func(session.PlayerEvent)
}

func (f *fakeSession) SetControlHandler(fn func(string, any)) {
	f.mu.Lock()
	f.control = fn
	f.mu.Unlock()
}

func (f *fakeSession) SetInputHandler(fn func(string, protocol.InputState)) {
	f.mu.Lock()
	f.input = fn
	f.mu.Unlock()
}

func (f *fakeSession) SetPlayerHandler(fn func(session.PlayerEvent)) {
	f.mu.Lock()
	f.player = fn
	f.mu.Unlock()
}

func (f *fakeSession) SendControl(to string, msg any) error {
	f.mu.Lock()
	f.sent = append(f.sent, sent{to, msg})
	f.mu.Unlock()
	return nil
}

func (f *fakeSession) firePlayer(kind session.PlayerEventKind, p protocol.Player) {
	f.mu.Lock()
	fn := f.player
	f.mu.Unlock()
	fn(session.PlayerEvent{Kind: kind, Player: p})
}

func (f *fakeSession) fireControl(id string, msg any) {
	f.mu.Lock()
	fn := f.control
	f.mu.Unlock()
	fn(id, msg)
}

func (f *fakeSession) fireInput(id string, s protocol.InputState) {
	f.mu.Lock()
	fn := f.input
	f.mu.Unlock()
	fn(id, s)
}

// take returns and clears what was sent.
func (f *fakeSession) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	return out
}

func layoutsTo(msgs []sent) map[string]protocol.Layout {
	out := map[string]protocol.Layout{}
	for _, m := range msgs {
		if l, ok := m.msg.(protocol.Layout); ok {
			out[m.to] = l
		}
	}
	return out
}

func systemsTo(msgs []sent, id string) []protocol.System {
	var out []protocol.System
	for _, m := range msgs {
		if s, ok := m.msg.(protocol.System); ok && m.to == id {
			out = append(out, s)
		}
	}
	return out
}

// recordingCaller is a CDP Caller that records commands.
type recordingCaller struct {
	mu    sync.Mutex
	calls []string
	texts []string
}

func (c *recordingCaller) Call(_ context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if method == "Page.getLayoutMetrics" {
		return json.RawMessage(`{"cssLayoutViewport":{"clientWidth":800,"clientHeight":600}}`), nil
	}
	c.mu.Lock()
	c.calls = append(c.calls, method+":"+typeOf(params))
	if t, ok := params["text"].(string); ok && method == "Input.insertText" {
		c.texts = append(c.texts, t)
	}
	c.mu.Unlock()
	return json.RawMessage(`{}`), nil
}

func typeOf(p map[string]any) string {
	if t, ok := p["type"].(string); ok {
		return t
	}
	return ""
}

func (c *recordingCaller) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

type fakePads struct {
	mu       sync.Mutex
	got      []hub.Output
	released []string
}

func (p *fakePads) Name() string { return "pads" }
func (p *fakePads) Deliver(_ string, outs []hub.Output) error {
	p.mu.Lock()
	p.got = append(p.got, outs...)
	p.mu.Unlock()
	return nil
}
func (p *fakePads) Release(id string) {
	p.mu.Lock()
	p.released = append(p.released, id)
	p.mu.Unlock()
}

type event struct {
	name string
	data any
}

type harness struct {
	t      *testing.T
	router *Router
	sess   *fakeSession
	caller *recordingCaller
	pads   *fakePads
	mu     sync.Mutex
	events []event
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, sess: &fakeSession{}, caller: &recordingCaller{}, pads: &fakePads{}}
	var r *Router
	pointer := cdp.New(cdp.Options{
		Caller:    h.caller,
		Sensitive: func() bool { return r.Sensitive() },
		Emit:      h.emit,
		Color:     func(id string) string { return r.Color(id) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go pointer.Run(ctx)
	var err error
	r, err = New(Config{Emit: h.emit, Pointer: pointer, Pads: h.pads, StartURL: "https://kiosk.example/"})
	if err != nil {
		t.Fatal(err)
	}
	h.router = r
	r.Attach(h.sess)
	return h
}

func (h *harness) emit(name string, data any) {
	h.mu.Lock()
	h.events = append(h.events, event{name, data})
	h.mu.Unlock()
}

func (h *harness) eventsNamed(name string) []any {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []any
	for _, e := range h.events {
		if e.name == name {
			out = append(out, e.data)
		}
	}
	return out
}

func (h *harness) waitCalls(n int) []string {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls := h.caller.snapshot(); len(calls) >= n {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("CDP calls %v, want %d", h.caller.snapshot(), n)
	return nil
}

func player(id string, slot int, host bool) protocol.Player {
	s := slot
	return protocol.Player{ID: id, Slot: &s, Name: "P" + id, Color: "teal", Host: host, Connected: true}
}

// appProvider is a stand-in for an app that owns the controllers.
type appProvider struct {
	hub.NopProvider
	mu     sync.Mutex
	inputs []protocol.InputState
	system []protocol.System
}

func (a *appProvider) Layout(p protocol.Player) protocol.Node {
	return protocol.Node{Type: "column", Children: []protocol.Node{
		hub.GamepadPreset(map[string]string{"South": "Jump"}),
	}}
}

func (a *appProvider) Profile() hub.Profile {
	return hub.Profile{Routes: []hub.Route{
		hub.PadRoute("pads", "player"),
		{Button: protocol.ButtonStart, Adapter: AppsAdapterName, Target: "pause"},
	}}
}

func (a *appProvider) HandleInput(_ string, s protocol.InputState) {
	a.mu.Lock()
	a.inputs = append(a.inputs, s)
	a.mu.Unlock()
}

func (a *appProvider) HandleSystem(_ string, m protocol.System) {
	a.mu.Lock()
	a.system = append(a.system, m)
	a.mu.Unlock()
}

func TestLayoutsOnJoinAndFocusChange(t *testing.T) {
	h := newHarness(t)
	h.sess.firePlayer(session.PlayerReady, player("p1", 1, true))
	got := layoutsTo(h.sess.take())
	host, ok := got["p1"]
	if !ok || host.Version != 1 || !hub.ContainsType(host.Tree, "trackpad") {
		t.Fatalf("host layout %+v", got)
	}
	// The layout parses as a control frame.
	raw, _ := json.Marshal(host)
	if _, err := protocol.ParseControl(raw); err != nil {
		t.Fatal(err)
	}

	h.sess.firePlayer(session.PlayerReady, player("p2", 2, false))
	got = layoutsTo(h.sess.take())
	guest, ok := got["p2"]
	if !ok || guest.Version != 2 || hub.ContainsType(guest.Tree, "trackpad") {
		t.Fatalf("guest layout %+v", got)
	}
	if _, resent := got["p1"]; resent {
		t.Fatal("unchanged host layout was re-sent")
	}

	// An app takes focus: every phone gets its layout, versions increase.
	app := &appProvider{}
	if err := h.router.Stack().Push(hub.LayerApp, app); err != nil {
		t.Fatal(err)
	}
	got = layoutsTo(h.sess.take())
	if len(got) != 2 || got["p1"].Version <= 2 || got["p2"].Version <= 2 || got["p1"].Version == got["p2"].Version {
		t.Fatalf("focus layouts %+v", got)
	}
	if !hub.ContainsType(got["p1"].Tree, "preset") {
		t.Fatalf("app layout %+v", got["p1"].Tree)
	}

	// App routes reach the pads and apps adapters, sticks and all.
	h.sess.fireInput("p2", protocol.InputState{Seq: 1, Buttons: protocol.ButtonStart | protocol.ButtonSouth})
	h.pads.mu.Lock()
	padOut := append([]hub.Output(nil), h.pads.got...)
	h.pads.mu.Unlock()
	if len(padOut) != 1 || padOut[0].Target != "player" || padOut[0].Value.(protocol.InputState).Buttons != protocol.ButtonStart|protocol.ButtonSouth {
		t.Fatalf("pads %+v", padOut)
	}
	apps := h.eventsNamed(AppInputEvent)
	if len(apps) != 1 || apps[0].(AppInput).Target != "pause" || apps[0].(AppInput).Value != true {
		t.Fatalf("apps %+v", apps)
	}

	// Popping the app brings the shell layouts back.
	if err := h.router.Stack().Pop(hub.LayerApp); err != nil {
		t.Fatal(err)
	}
	got = layoutsTo(h.sess.take())
	if !hub.ContainsType(got["p1"].Tree, "trackpad") || hub.ContainsType(got["p2"].Tree, "trackpad") {
		t.Fatalf("after pop %+v", got)
	}

	// Host transfer swaps the remote and the waiting layout.
	h.sess.firePlayer(session.PlayerUpdated, player("p1", 1, false))
	h.sess.firePlayer(session.PlayerUpdated, player("p2", 2, true))
	got = layoutsTo(h.sess.take())
	if hub.ContainsType(got["p1"].Tree, "trackpad") || !hub.ContainsType(got["p2"].Tree, "trackpad") {
		t.Fatalf("host transfer %+v", got)
	}
}

func TestHomeGoesToOverlayNotProvider(t *testing.T) {
	h := newHarness(t)
	app := &appProvider{}
	_ = h.router.Stack().Push(hub.LayerApp, app)
	h.sess.firePlayer(session.PlayerReady, player("p1", 1, true))

	h.sess.fireInput("p1", protocol.InputState{Seq: 1, Buttons: protocol.ButtonHome})
	h.sess.fireInput("p1", protocol.InputState{Seq: 2, Buttons: protocol.ButtonHome}) // held: no second edge
	h.sess.fireControl("p1", protocol.System{T: "system", Action: "home"})
	h.sess.fireControl("p1", protocol.System{T: "system", Action: "resume"})

	homes := h.eventsNamed(HomeEvent)
	if len(homes) != 2 || homes[0].(Home) != (Home{PlayerID: "p1", Via: "button"}) || homes[1].(Home).Via != "system" {
		t.Fatalf("home events %+v", homes)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	for _, in := range app.inputs {
		if in.Buttons&protocol.ButtonHome != 0 {
			t.Fatal("provider saw Home")
		}
	}
	if len(app.system) != 1 || app.system[0].Action != "resume" {
		t.Fatalf("provider system %+v", app.system)
	}
}

func TestLeaveForgetsPlayer(t *testing.T) {
	h := newHarness(t)
	app := &appProvider{}
	_ = h.router.Stack().Push(hub.LayerApp, app)
	h.sess.firePlayer(session.PlayerReady, player("p1", 1, true))
	h.sess.fireInput("p1", protocol.InputState{Seq: 1, Buttons: protocol.ButtonSouth})
	h.sess.take()

	h.sess.firePlayer(session.PlayerLeft, protocol.Player{ID: "p1"})
	h.pads.mu.Lock()
	last := h.pads.got[len(h.pads.got)-1]
	released := append([]string(nil), h.pads.released...)
	h.pads.mu.Unlock()
	if last.Value.(protocol.InputState).Buttons != 0 {
		t.Fatalf("held button not released on leave: %+v", last)
	}
	if len(released) != 1 || released[0] != "p1" {
		t.Fatalf("pads released %v", released)
	}
	if h.router.IsHost("p1") {
		t.Fatal("left player still known")
	}
	// No layouts go to a player who left.
	if got := layoutsTo(h.sess.take()); len(got) != 0 {
		t.Fatalf("layout after leave %+v", got)
	}
	// Rejoining sends the layout again.
	h.sess.firePlayer(session.PlayerReady, player("p1", 1, true))
	if got := layoutsTo(h.sess.take()); got["p1"].Tree.Type == "" {
		t.Fatal("no layout on rejoin")
	}

	// Detaching the session forgets everyone.
	h.router.Detach(h.sess)
	h.pads.mu.Lock()
	n := len(h.pads.released)
	h.pads.mu.Unlock()
	if n != 2 {
		t.Fatalf("detach released %d", n)
	}
	if err := h.router.Haptic("p1", []int{10}); err == nil {
		t.Fatal("haptic after detach should fail")
	}
}

func TestShellRemoteAndSensitiveDrop(t *testing.T) {
	h := newHarness(t)
	h.sess.firePlayer(session.PlayerReady, player("p1", 1, true))
	h.sess.firePlayer(session.PlayerReady, player("p2", 2, false))
	h.sess.take()

	move := protocol.Event{T: "event", Control: shell.ControlTrackpad, Kind: "change", Value: map[string]any{"dx": 5.0, "dy": 5.0, "fingers": 1.0}}
	// A guest's events and text are ignored.
	h.sess.fireControl("p2", move)
	h.sess.fireControl("p2", protocol.Text{T: "text", Op: "insert", Text: "guest"})
	// The host moves, taps and types.
	h.sess.fireControl("p1", move)
	h.sess.fireControl("p1", protocol.Event{T: "event", Control: shell.ControlTrackpad, Kind: "tap", Value: map[string]any{"fingers": 1.0}})
	h.sess.fireControl("p1", protocol.Text{T: "text", Op: "insert", Text: "hi"})
	calls := h.waitCalls(4)
	want := []string{"Input.dispatchMouseEvent:mouseMoved", "Input.dispatchMouseEvent:mousePressed", "Input.dispatchMouseEvent:mouseReleased", "Input.insertText:"}
	for i, w := range want {
		if calls[i] != w {
			t.Fatalf("calls %v", calls)
		}
	}
	h.caller.mu.Lock()
	texts := append([]string(nil), h.caller.texts...)
	h.caller.mu.Unlock()
	if len(texts) != 1 || texts[0] != "hi" {
		t.Fatalf("texts %v", texts)
	}
	cursors := h.eventsNamed(cdp.CursorEvent)
	if len(cursors) == 0 || cursors[0].(cdp.Cursor).Color != "teal" {
		t.Fatalf("cursor events %+v", cursors)
	}
	msgs := h.sess.take()
	haptic := false
	for _, m := range msgs {
		if hp, ok := m.msg.(protocol.Haptic); ok && m.to == "p1" && len(hp.Pattern) == 1 {
			haptic = true
		}
	}
	if !haptic {
		t.Fatalf("tap haptic missing: %+v", msgs)
	}

	// Remote buttons go to the overlay; Home also loads the start page.
	for _, id := range []string{shell.ControlBack, shell.ControlOpenYui, shell.ControlHome} {
		h.sess.fireControl("p1", protocol.Event{T: "event", Control: id, Kind: "tap"})
	}
	remote := h.eventsNamed(shell.RemoteEvent)
	if len(remote) != 3 || remote[2].(shell.RemoteAction).Action != "home" {
		t.Fatalf("remote %+v", remote)
	}
	calls = h.waitCalls(5)
	if calls[4] != "Page.navigate:" {
		t.Fatalf("home navigate %v", calls)
	}

	// D9: the overlay turns sensitive. Phones are told, input is dropped.
	h.router.SetSensitive(true)
	notices := h.sess.take()
	for _, id := range []string{"p1", "p2"} {
		sys := systemsTo(notices, id)
		if len(sys) != 1 || sys[0].Action != "sensitive" || sys[0].Value != true {
			t.Fatalf("sensitive to %s: %+v", id, sys)
		}
	}
	h.router.SetSensitive(true) // unchanged: no repeat
	if sys := systemsTo(h.sess.take(), "p1"); len(sys) != 0 {
		t.Fatalf("repeated notice %+v", sys)
	}
	before := len(h.caller.snapshot())
	h.sess.fireControl("p1", move)
	h.sess.fireControl("p1", protocol.Text{T: "text", Op: "insert", Text: "1234"})
	h.sess.fireControl("p1", protocol.Event{T: "event", Control: shell.ControlHome, Kind: "tap"})
	time.Sleep(100 * time.Millisecond)
	if after := h.caller.snapshot(); len(after) != before {
		t.Fatalf("input injected while sensitive: %v", after[before:])
	}
	last := h.eventsNamed(cdp.CursorEvent)
	if c := last[len(last)-1].(cdp.Cursor); c.Visible {
		t.Fatal("cursor still visible while sensitive")
	}

	// A phone that (re)joins while sensitive is told right away.
	h.sess.firePlayer(session.PlayerReady, player("p3", 3, false))
	if sys := systemsTo(h.sess.take(), "p3"); len(sys) != 1 || sys[0].Value != true {
		t.Fatalf("joiner while sensitive: %+v", sys)
	}
	h.router.SetSensitive(false)
	if sys := systemsTo(h.sess.take(), "p1"); len(sys) != 1 || sys[0].Value != false {
		t.Fatalf("sensitive off: %+v", sys)
	}
	h.sess.fireControl("p1", protocol.Text{T: "text", Op: "insert", Text: "ok"})
	h.waitCalls(before + 1)
}

func TestKeyboardToggleHidesOverlayKeyboard(t *testing.T) {
	h := newHarness(t)
	h.sess.firePlayer(session.PlayerReady, player("p1", 1, true))
	h.sess.take()
	toggle := protocol.Event{T: "event", Control: shell.ControlKeyboardToggle, Kind: "tap"}

	h.sess.fireControl("p1", toggle)
	got := layoutsTo(h.sess.take())
	if !hub.ContainsType(got["p1"].Tree, "keyboard") {
		t.Fatalf("keyboard missing %+v", got)
	}
	kb := h.eventsNamed(KeyboardEvent)
	if len(kb) != 1 || kb[0].(Keyboard).Active != true {
		t.Fatalf("pad.keyboard %+v", kb)
	}

	h.sess.fireControl("p1", toggle)
	got = layoutsTo(h.sess.take())
	if hub.ContainsType(got["p1"].Tree, "keyboard") {
		t.Fatal("keyboard still shown")
	}
	kb = h.eventsNamed(KeyboardEvent)
	if len(kb) != 2 || kb[1].(Keyboard).Active {
		t.Fatalf("pad.keyboard off %+v", kb)
	}

	// The keyboard also goes away when its phone leaves.
	h.sess.fireControl("p1", toggle)
	h.sess.firePlayer(session.PlayerLeft, protocol.Player{ID: "p1"})
	kb = h.eventsNamed(KeyboardEvent)
	if kb[len(kb)-1].(Keyboard).Active {
		t.Fatal("pad.keyboard still active after leave")
	}
}

func TestSlotOf(t *testing.T) {
	h := newHarness(t)
	h.sess.firePlayer(session.PlayerReady, player("p1", 3, false))
	spectator := protocol.Player{ID: "s1", Connected: true}
	h.sess.firePlayer(session.PlayerReady, spectator)
	if slot, ok := h.router.SlotOf("p1"); !ok || slot != 2 {
		t.Fatalf("slot %d %v", slot, ok)
	}
	if _, ok := h.router.SlotOf("s1"); ok {
		t.Fatal("spectator has a pad")
	}
}
