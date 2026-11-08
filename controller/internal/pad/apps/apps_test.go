package apps

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/cdp"
	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

type recAdapter struct {
	name string
	mu   sync.Mutex
	outs []delivered
	sys  []string
}

type delivered struct {
	player string
	out    hub.Output
}

func (a *recAdapter) Name() string { return a.name }

func (a *recAdapter) Deliver(playerID string, outputs []hub.Output) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, o := range outputs {
		a.outs = append(a.outs, delivered{playerID, o})
	}
	return nil
}

func (a *recAdapter) SetSystemButton(bit uint32, down bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := "up"
	if down {
		state = "down"
	}
	for name := range map[string]bool{"South": true, "Start": true, "L1": true} {
		if b, _ := hub.ButtonBit(name); b == bit {
			a.sys = append(a.sys, name+":"+state)
		}
	}
}

func (a *recAdapter) take() []delivered {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.outs
	a.outs = nil
	return out
}

func (a *recAdapter) system() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.sys...)
}

type shellProvider struct{ hub.NopProvider }

func (shellProvider) Layout(protocol.Player) protocol.Node {
	return protocol.Node{Type: "text", Props: map[string]any{"text": "shell"}}
}

type rig struct {
	stack    *hub.Stack
	ctl      *Controller
	cdp      *recAdapter
	pads     *recAdapter
	mu       sync.Mutex
	events   []Input
	refresh  atomic.Int32
	host     atomic.Value // string
	hasHost  atomic.Bool
	stopped  chan string
	ticks    chan time.Time
	clock    atomic.Int64
	resolver DSUResolver
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{cdp: &recAdapter{name: "cdp"}, pads: &recAdapter{name: "pads"}, stopped: make(chan string, 4), ticks: make(chan time.Time)}
	r.host.Store("p1")
	r.clock.Store(time.Unix(1000, 0).UnixNano())
	stack, err := hub.New(shellProvider{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	stack.SetFocusFunc(func(hub.Layer, hub.Provider) { r.refresh.Add(1) })
	_ = stack.RegisterAdapter(r.cdp)
	_ = stack.RegisterAdapter(r.pads)
	r.stack = stack
	r.resolver = DSUResolver{Players: "127.0.0.1:26760", System: "127.0.0.1:26761"}
	r.ctl, err = New(Config{
		Stack:   stack,
		Refresh: func() { r.refresh.Add(1) },
		Emit: func(event string, data any) {
			if event != AppInputEvent {
				t.Errorf("event %s", event)
				return
			}
			r.mu.Lock()
			r.events = append(r.events, data.(Input))
			r.mu.Unlock()
		},
		IsHost:        func(id string) bool { return id == r.host.Load().(string) },
		HostName:      func() string { return "Riccardo" },
		CDP:           r.cdp,
		Pads:          r.pads,
		Resolver:      r.resolver,
		HasHost:       r.hasHost.Load,
		StopNative:    func(appID string) error { r.stopped <- appID; return nil },
		NoHostTimeout: 3 * time.Minute,
		Now:           func() time.Time { return time.Unix(0, r.clock.Load()) },
		ticks:         func() (<-chan time.Time, func()) { return r.ticks, func() {} },
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) takeEvents() []Input {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

func player(id string, host bool) protocol.Player {
	return protocol.Player{ID: id, Host: host, Connected: true}
}

func layoutType(t *testing.T, r *rig, p protocol.Player) string {
	t.Helper()
	tree := r.stack.Layout(p)
	if tree.Type == "text" {
		return "text:" + tree.Props["text"].(string)
	}
	raw, _ := json.Marshal(tree)
	return string(raw)
}

const quiz = "dev.example.quiz"

func TestAppFocusPushesAndPops(t *testing.T) {
	r := newRig(t)
	if err := r.ctl.SetFocus(Focus{AppID: quiz, Name: "Quiz", Active: true}); err != nil {
		t.Fatal(err)
	}
	if layer, top := r.stack.Top(); layer != hub.LayerApp || top == nil {
		t.Fatalf("top = %v", layer)
	}
	if !r.ctl.Owns(quiz) || r.ctl.Focused() != quiz {
		t.Fatal("app does not own the controllers")
	}
	// No layouts yet: a waiting screen naming the app.
	if got := layoutType(t, r, player("p1", true)); !contains(got, "Quiz") {
		t.Fatalf("waiting layout = %s", got)
	}

	// Per-player layouts, with "*" for everyone else.
	choice := protocol.Node{Type: "choice", Props: map[string]any{"id": "answer", "options": []any{map[string]any{"id": "a", "label": "A"}}}}
	next := protocol.Node{Type: "column", Children: []protocol.Node{choice, {Type: "button", Props: map[string]any{"id": "next", "label": "Next"}}}}
	other := protocol.Node{Type: "text", Props: map[string]any{"text": "watch"}}
	before := r.refresh.Load()
	if err := r.ctl.SetLayouts(quiz, map[string]*protocol.Node{"p1": &next, "*": &other, "p9": nil}); err != nil {
		t.Fatal(err)
	}
	if r.refresh.Load() == before {
		t.Fatal("layouts did not refresh the phones")
	}
	if got := layoutType(t, r, player("p1", true)); !contains(got, `"next"`) {
		t.Fatalf("p1 layout = %s", got)
	}
	if got := layoutType(t, r, player("p2", false)); got != "text:watch" {
		t.Fatalf("p2 layout = %s", got)
	}

	// Events on the app's controls reach the overlay, tagged with the app.
	if err := r.stack.Event("p1", protocol.Event{Control: "answer", Kind: "tap", Value: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.stack.Event("p2", protocol.Event{Control: "next", Kind: "tap"}); err != nil {
		t.Fatal(err)
	}
	if err := r.stack.Text("p1", protocol.Text{Op: "insert", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	_ = r.stack.System("p1", protocol.System{Action: "quit"})
	_ = r.stack.System("p1", protocol.System{Action: "home"}) // never reaches the app
	evs := r.takeEvents()
	if len(evs) != 4 || evs[0].AppID != quiz || evs[0].Target != "answer" || evs[0].Kind != "tap" || evs[0].Value != "a" ||
		evs[1].PlayerID != "p2" || evs[1].Target != "next" || evs[2].Kind != hub.KindText || evs[3].Kind != "system" || evs[3].Target != "quit" {
		t.Fatalf("events %+v", evs)
	}
	// Input frames are not forwarded unless the app asked for them.
	_ = r.stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonSouth, TMs: 1})
	if evs := r.takeEvents(); len(evs) != 0 {
		t.Fatalf("frames without frames: %+v", evs)
	}
	if err := r.ctl.SetFocus(Focus{AppID: quiz, Name: "Quiz", Active: true, Frames: true}); err != nil {
		t.Fatal(err)
	}
	_ = r.stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonSouth | protocol.ButtonHome, TMs: 2})
	evs = r.takeEvents()
	if len(evs) != 1 || evs[0].Kind != hub.KindState || evs[0].Value.(protocol.InputState).Buttons != protocol.ButtonSouth {
		t.Fatalf("frames %+v", evs)
	}

	// Another app cannot blur this one; this app can.
	if err := r.ctl.SetFocus(Focus{AppID: "dev.example.other", Active: false}); err != nil {
		t.Fatal(err)
	}
	if r.ctl.Focused() != quiz {
		t.Fatal("blurred by another app")
	}
	if err := r.ctl.SetFocus(Focus{AppID: quiz, Active: false}); err != nil {
		t.Fatal(err)
	}
	if layer, _ := r.stack.Top(); layer != hub.LayerShell {
		t.Fatalf("top after blur = %v", layer)
	}
	if r.ctl.Owns(quiz) {
		t.Fatal("still owns after blur")
	}
	// The app's adapter and layouts are gone.
	if got := layoutType(t, r, player("p1", true)); got != "text:shell" {
		t.Fatalf("shell layout = %s", got)
	}
	r.ctl.mu.Lock()
	_, kept := r.ctl.layouts[quiz]
	r.ctl.mu.Unlock()
	if kept {
		t.Fatal("layouts kept after blur")
	}

	// Blur with no app id clears whatever app is focused (overlay reload).
	_ = r.ctl.SetFocus(Focus{AppID: quiz, Active: true})
	_ = r.ctl.SetFocus(Focus{Active: false})
	if r.ctl.Focused() != "" {
		t.Fatal("reset did not blur")
	}
	if err := r.ctl.SetFocus(Focus{AppID: "../x", Active: true}); !errors.Is(err, ErrBadAppID) {
		t.Fatalf("bad id: %v", err)
	}
}

func TestSwitchingAppsReleasesThePrevious(t *testing.T) {
	r := newRig(t)
	_ = r.ctl.SetFocus(Focus{AppID: quiz, Active: true})
	tree := protocol.Node{Type: "button", Props: map[string]any{"id": "go"}}
	_ = r.ctl.SetLayouts(quiz, map[string]*protocol.Node{"*": &tree})
	_ = r.ctl.SetFocus(Focus{AppID: "dev.example.other", Active: true})
	if r.ctl.Owns(quiz) || r.ctl.Focused() != "dev.example.other" {
		t.Fatal("switch")
	}
	// The old app's control no longer routes anywhere.
	_ = r.stack.Event("p1", protocol.Event{Control: "go", Kind: "tap"})
	if evs := r.takeEvents(); len(evs) != 0 {
		t.Fatalf("old app got %+v", evs)
	}
}

func TestLayoutValidation(t *testing.T) {
	r := newRig(t)
	deep := protocol.Node{Type: "column"}
	for i := 0; i < maxLayoutDepth+2; i++ {
		deep = protocol.Node{Type: "column", Children: []protocol.Node{deep}}
	}
	big := protocol.Node{Type: "text", Props: map[string]any{"text": string(make([]byte, maxLayoutBytes+1))}}
	untyped := protocol.Node{Type: "column", Children: []protocol.Node{{}}}
	for name, tree := range map[string]protocol.Node{"deep": deep, "big": big, "untyped": untyped} {
		if err := r.ctl.SetLayouts(quiz, map[string]*protocol.Node{"*": &tree}); !errors.Is(err, ErrBadLayout) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := r.ctl.SetLayouts("Bad App", nil); !errors.Is(err, ErrBadAppID) {
		t.Fatalf("bad app: %v", err)
	}
}

func TestKeymapSubwaySwipesAndDpadReachCDP(t *testing.T) {
	r := newRig(t)
	arrows := map[string]string{"up": "ArrowUp", "down": "ArrowDown", "left": "ArrowLeft", "right": "ArrowRight"}
	km := &Keymap{Controls: []KeymapControl{
		{Type: "swipe", Keys: arrows, Tap: "Space"},
		{Type: "dpad", Keys: arrows},
	}}
	if err := r.ctl.SetFocus(Focus{AppID: "dev.genr.subway-surfers", Name: "Subway Surfers", Active: true, Keymap: km}); err != nil {
		t.Fatal(err)
	}
	host, guest := player("p1", true), player("p2", false)
	if got := layoutType(t, r, host); !contains(got, `"trackpad"`) || !contains(got, `"dpad"`) {
		t.Fatalf("host layout %s", got)
	}
	if got := layoutType(t, r, guest); !contains(got, "Waiting for Riccardo") {
		t.Fatalf("guest layout %s", got)
	}

	swipeTarget := "swipe:up=ArrowUp,down=ArrowDown,left=ArrowLeft,right=ArrowRight"
	_ = r.stack.Event("p1", protocol.Event{Control: "swipe", Kind: "change", Value: map[string]any{"dx": -50.0, "dy": 0.0}})
	_ = r.stack.Event("p1", protocol.Event{Control: "swipe", Kind: "tap", Value: map[string]any{"fingers": 1.0}})
	_ = r.stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonUp, TMs: 1})
	_ = r.stack.Input("p1", protocol.InputState{TMs: 2})
	got := r.cdp.take()
	want := []struct{ target, kind string }{
		{swipeTarget, "change"},
		{"key:Space", "tap"},
	}
	if len(got) < 2 {
		t.Fatalf("cdp got %+v", got)
	}
	for i, w := range want {
		if got[i].out.Target != w.target || got[i].out.Kind != w.kind || got[i].out.Adapter != "cdp" {
			t.Fatalf("output %d = %+v, want %v", i, got[i].out, w)
		}
	}
	// The d-pad's Up bit holds ArrowUp, then releases it.
	var up []any
	for _, d := range got[2:] {
		if d.out.Target == "key:ArrowUp" {
			up = append(up, d.out.Value)
		}
	}
	if len(up) != 2 || up[0] != true || up[1] != false {
		t.Fatalf("ArrowUp levels %v (all %+v)", up, got)
	}

	// Guests' presses are dropped in host mode, releases are not.
	_ = r.stack.Event("p2", protocol.Event{Control: "swipe", Kind: "change", Value: map[string]any{"dx": 50.0}})
	_ = r.stack.Input("p2", protocol.InputState{Buttons: protocol.ButtonLeft, TMs: 1})
	for _, d := range r.cdp.take() {
		if d.out.Kind == "change" || d.out.Value == true {
			t.Fatalf("guest press reached CDP: %+v", d)
		}
	}
	// Host moves to p2: now p2 plays.
	r.host.Store("p2")
	_ = r.stack.Event("p2", protocol.Event{Control: "swipe", Kind: "swipe", Value: "up"})
	if got := r.cdp.take(); len(got) != 1 || got[0].player != "p2" {
		t.Fatalf("new host %+v", got)
	}

	// Popping releases a key held through the switch.
	_ = r.stack.Input("p2", protocol.InputState{Buttons: protocol.ButtonDown, TMs: 3})
	r.cdp.take()
	_ = r.ctl.SetFocus(Focus{AppID: "dev.genr.subway-surfers", Active: false})
	var released bool
	for _, d := range r.cdp.take() {
		if d.out.Target == "key:ArrowDown" && d.out.Value == false {
			released = true
		}
	}
	if !released {
		t.Fatal("held key not released on blur")
	}
}

func TestKeymapFruitNinjaDragsInsideTheRegion(t *testing.T) {
	r := newRig(t)
	km := &Keymap{Players: PlayersAll, Controls: []KeymapControl{{Type: "trackpad", Mode: "absolute", Mouse: "drag"}}}
	region := &cdp.Rect{X: 40, Y: 120, W: 800, H: 600}
	if err := r.ctl.SetFocus(Focus{AppID: "dev.genr.fruit-ninja", Active: true, Keymap: km, Region: region}); err != nil {
		t.Fatal(err)
	}
	// "all": guests play too.
	_ = r.stack.Event("p2", protocol.Event{Control: "drag", Kind: "press"})
	_ = r.stack.Input("p2", protocol.InputState{PX: 100, PY: -100, TMs: 1})
	_ = r.stack.Event("p2", protocol.Event{Control: "drag", Kind: "release"})
	got := r.cdp.take()
	if len(got) != 3 {
		t.Fatalf("outputs %+v", got)
	}
	for _, d := range got {
		if d.out.Target != "mouse:40,120,800,600" {
			t.Fatalf("target %+v", d.out)
		}
	}
	if got[0].out.Kind != "press" || got[1].out.Kind != hub.KindState || got[2].out.Kind != "release" {
		t.Fatalf("kinds %+v", got)
	}

	// The window resized: the overlay sends a new region, routes follow.
	_ = r.ctl.SetFocus(Focus{AppID: "dev.genr.fruit-ninja", Active: true, Keymap: km, Region: &cdp.Rect{X: 0, Y: 0, W: 1920, H: 1080}})
	_ = r.stack.Event("p1", protocol.Event{Control: "drag", Kind: "press"})
	if got := r.cdp.take(); len(got) != 1 || got[0].out.Target != "mouse:0,0,1920,1080" {
		t.Fatalf("after resize %+v", got)
	}
}

func TestKeymapValidation(t *testing.T) {
	arrows := map[string]string{"up": "ArrowUp"}
	bad := map[string]Keymap{
		"no controls":     {},
		"players":         {Players: "some", Controls: []KeymapControl{{Type: "dpad", Keys: arrows}}},
		"unknown type":    {Controls: []KeymapControl{{Type: "joystick"}}},
		"unknown key":     {Controls: []KeymapControl{{Type: "dpad", Keys: map[string]string{"up": "F13"}}}},
		"bad direction":   {Controls: []KeymapControl{{Type: "swipe", Keys: map[string]string{"north": "ArrowUp"}}}},
		"two swipes":      {Controls: []KeymapControl{{Type: "swipe", Keys: arrows}, {Type: "swipe", Keys: arrows}}},
		"button no label": {Controls: []KeymapControl{{Type: "button", Key: "Space"}}},
		"button both":     {Controls: []KeymapControl{{Type: "button", Label: "x", Key: "Space", Mouse: "left"}}},
		"abs no drag":     {Controls: []KeymapControl{{Type: "trackpad", Mode: "absolute"}}},
		"trackpad mode":   {Controls: []KeymapControl{{Type: "trackpad", Mode: "sideways"}}},
		"swipe tap key":   {Controls: []KeymapControl{{Type: "swipe", Keys: arrows, Tap: "Hyper"}}},
		"long label":      {Controls: []KeymapControl{{Type: "button", Label: "this label is far too long to fit on a phone button", Key: "a"}}},
	}
	for name, km := range bad {
		if err := ValidateKeymap(km); !errors.Is(err, ErrBadKeymap) {
			t.Errorf("%s: %v", name, err)
		}
	}
	good := Keymap{Controls: []KeymapControl{
		{Type: "button", Label: "Jump", Key: "Space"},
		{Type: "button", Label: "Fire", Mouse: "left"},
		{Type: "trackpad", Mode: "relative"},
	}}
	compiled, err := compileKeymap(good, nil, KeymapAdapterName)
	if err != nil {
		t.Fatal(err)
	}
	// Buttons get South, East… and share a row.
	raw, _ := json.Marshal(compiled.tree)
	if !contains(string(raw), `"bit":0`) || !contains(string(raw), `"bit":1`) || !contains(string(raw), `"type":"row"`) {
		t.Fatalf("tree %s", raw)
	}
	if err := (hub.Profile{Routes: compiled.routes}).Validate(); err != nil {
		t.Fatalf("routes invalid: %v", err)
	}
}

func TestVirtualPadResolver(t *testing.T) {
	r := DSUResolver{Players: "127.0.0.1:26760", System: "127.0.0.1:26761"}
	if got, err := r.VirtualPad("dev.example.app", 0); err != nil || got != "127.0.0.1:26760/0" {
		t.Fatalf("pad 0 = %q %v", got, err)
	}
	if got, err := r.VirtualPad("dev.example.app", 3); err != nil || got != "127.0.0.1:26760/3" {
		t.Fatalf("pad 3 = %q %v", got, err)
	}
	if _, err := r.VirtualPad("dev.example.app", 4); err == nil {
		t.Fatal("pad 4 resolved")
	}
	if got, err := r.SystemPad("dev.example.app"); err != nil || got != "127.0.0.1:26761/0" {
		t.Fatalf("system = %q %v", got, err)
	}
	// It plugs into native templates.
	var _ native.VirtualPadResolver = r
	var _ native.SystemPadResolver = r
	env := native.TemplateEnv{
		VirtualPad: func(n int) (string, error) { return r.VirtualPad("a", n) },
		SystemPad:  func() (string, error) { return r.SystemPad("a") },
	}
	got, err := native.Render("pads={{virtualPad:1}} hotkeys={{systemPad}}", env)
	if err != nil || got != "pads=127.0.0.1:26760/1 hotkeys=127.0.0.1:26761/0" {
		t.Fatalf("render %q %v", got, err)
	}
}

const arcade = "dev.example.arcade"

func TestNativeSessionPushesPopsAndDrivesPads(t *testing.T) {
	r := newRig(t)
	r.hasHost.Store(true)
	_ = r.ctl.SetFocus(Focus{AppID: arcade, Active: true})
	remote := hub.MotionRemotePreset(map[string]string{"South": "A"})
	_ = r.ctl.SetLayouts(arcade, map[string]*protocol.Node{"*": &remote})

	info := native.Info{ID: "s1", AppID: arcade, VirtualPads: []int{0, 1}}
	r.ctl.SessionStarted(info)
	if layer, _ := r.stack.Top(); layer != hub.LayerNative {
		t.Fatalf("top = %v", layer)
	}
	if r.ctl.NativeApp() != arcade {
		t.Fatal("native app")
	}
	// The native layer shows the app's layout.
	if got := layoutType(t, r, player("p1", true)); !contains(got, "motion-remote") {
		t.Fatalf("native layout %s", got)
	}

	// No attachments: each player drives the pad of their own slot.
	_ = r.stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonSouth, TMs: 1})
	got := r.pads.take()
	if len(got) != 1 || got[0].out.Target != "player" || got[0].out.Kind != hub.KindState {
		t.Fatalf("unattached %+v", got)
	}

	// Attachments pick the pad; others become spectators.
	a, err := r.ctl.Attach(arcade, Attachment{PlayerID: "p2", Slot: 0, PointerStick: true})
	if err != nil || a.Address != "127.0.0.1:26760/0" || a.Backend != "dsu" {
		t.Fatalf("attach %+v %v", a, err)
	}
	_ = r.stack.Input("p2", protocol.InputState{Buttons: protocol.ButtonSouth, TMs: 1})
	_ = r.stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonSouth, TMs: 2})
	got = r.pads.take()
	if len(got) != 1 || got[0].player != "p2" || got[0].out.Target != "slot:0/pointer-stick" {
		t.Fatalf("attached %+v", got)
	}
	// Moving slot 0 to p1 releases p2's pad.
	if _, err := r.ctl.Attach(arcade, Attachment{PlayerID: "p1", Slot: 0}); err != nil {
		t.Fatal(err)
	}
	got = r.pads.take()
	if len(got) != 1 || got[0].player != "p2" || got[0].out.Target != "slot:0" || got[0].out.Value != (protocol.InputState{}) {
		t.Fatalf("moved %+v", got)
	}
	if list := r.ctl.Attachments(arcade); len(list) != 1 || list[0].PlayerID != "p1" {
		t.Fatalf("list %+v", list)
	}
	if _, err := r.ctl.Attach(arcade, Attachment{PlayerID: "p3", Slot: 4}); !errors.Is(err, ErrBadSlot) {
		t.Fatalf("slot 4: %v", err)
	}
	if _, err := r.ctl.Attach(arcade, Attachment{PlayerID: "p3", Slot: 1, Backend: "vigem"}); !errors.Is(err, ErrBadBackend) {
		t.Fatalf("backend: %v", err)
	}

	// Events on the layout and Home-sheet actions still reach the app.
	_ = r.stack.System("p1", protocol.System{Action: "save_state"})
	if evs := r.takeEvents(); len(evs) != 1 || evs[0].AppID != arcade || evs[0].Target != "save_state" {
		t.Fatalf("system %+v", evs)
	}

	// System pad hotkeys need ownership.
	if err := r.ctl.SystemButton("dev.example.other", "Start", "tap"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("other app: %v", err)
	}
	if err := r.ctl.SystemButton(arcade, "Home", "tap"); !errors.Is(err, ErrBadButton) {
		t.Fatalf("home: %v", err)
	}
	if err := r.ctl.SystemButton(arcade, "L1", "press"); err != nil {
		t.Fatal(err)
	}
	if err := r.ctl.SystemButton(arcade, "L1", "release"); err != nil {
		t.Fatal(err)
	}
	if sys := r.pads.system(); len(sys) != 2 || sys[0] != "L1:down" || sys[1] != "L1:up" {
		t.Fatalf("system pad %v", sys)
	}

	// The app closes its view: the session keeps the app's layouts and
	// attachments.
	_ = r.ctl.SetFocus(Focus{AppID: arcade, Active: false})
	if !r.ctl.Owns(arcade) || len(r.ctl.Attachments(arcade)) != 1 {
		t.Fatal("native owner lost its state on blur")
	}

	// Session ends: native layer popped, neutral frame sent, state released.
	_ = r.stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonSouth, TMs: 3})
	r.pads.take()
	r.ctl.SessionEnded(info, native.Status{State: native.StateExited})
	if layer, _ := r.stack.Top(); layer != hub.LayerShell {
		t.Fatalf("top after end = %v", layer)
	}
	got = r.pads.take()
	if len(got) != 1 || got[0].out.Value != (protocol.InputState{}) {
		t.Fatalf("neutral on end %+v", got)
	}
	if r.ctl.Owns(arcade) || len(r.ctl.Attachments(arcade)) != 0 {
		t.Fatal("state kept after the session")
	}
	// A stale end for another session changes nothing.
	r.ctl.SessionStarted(native.Info{ID: "s2", AppID: arcade})
	r.ctl.SessionEnded(native.Info{ID: "s1", AppID: arcade}, native.Status{})
	if layer, _ := r.stack.Top(); layer != hub.LayerNative {
		t.Fatal("stale end popped the new session")
	}
	r.ctl.SessionEnded(native.Info{ID: "s2", AppID: arcade}, native.Status{})
}

func TestNativeDefaultLayoutIsAGamepad(t *testing.T) {
	r := newRig(t)
	r.hasHost.Store(true)
	r.ctl.SessionStarted(native.Info{ID: "s1", AppID: arcade})
	defer r.ctl.SessionEnded(native.Info{ID: "s1", AppID: arcade}, native.Status{})
	if got := layoutType(t, r, player("p1", true)); !contains(got, `"gamepad"`) {
		t.Fatalf("default %s", got)
	}
}

// tick runs one watchdog check and waits until it has finished (the next
// send only completes once the loop is back at its select). It reports
// false when the watchdog is no longer running.
func (r *rig) tick() bool {
	for i := 0; i < 2; i++ {
		select {
		case r.ticks <- time.Now():
		case <-time.After(200 * time.Millisecond):
			return false
		}
	}
	return true
}

func (r *rig) noStop(t *testing.T, when string) {
	t.Helper()
	select {
	case id := <-r.stopped:
		t.Fatalf("stopped %s %s", id, when)
	default:
	}
}

func TestWatchdogStopsWithoutAHost(t *testing.T) {
	r := newRig(t)
	r.hasHost.Store(true)
	r.ctl.SessionStarted(native.Info{ID: "s1", AppID: arcade})

	// A host is connected: nothing happens however long it runs.
	r.clock.Add(int64(10 * time.Minute))
	r.tick()
	r.noStop(t, "with a host")

	// The host leaves: 2 minutes is not enough…
	r.hasHost.Store(false)
	r.clock.Add(int64(2 * time.Minute))
	r.tick()
	r.noStop(t, "too early")
	// …a host coming back resets the clock…
	r.hasHost.Store(true)
	r.tick()
	r.hasHost.Store(false)
	r.clock.Add(int64(2 * time.Minute))
	r.tick()
	r.noStop(t, "after the host returned")
	// …and 3 minutes without one stops the session.
	r.clock.Add(int64(90 * time.Second))
	r.tick()
	select {
	case id := <-r.stopped:
		if id != arcade {
			t.Fatalf("stopped %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not stop the session")
	}
	if r.tick() {
		t.Fatal("watchdog still running after stopping the session")
	}
	r.ctl.SessionEnded(native.Info{ID: "s1", AppID: arcade}, native.Status{})
}

func TestWatchdogEndsWithTheSession(t *testing.T) {
	r := newRig(t)
	r.ctl.SessionStarted(native.Info{ID: "s1", AppID: arcade})
	r.ctl.SessionEnded(native.Info{ID: "s1", AppID: arcade}, native.Status{})
	r.clock.Add(int64(time.Hour))
	if r.tick() {
		t.Fatal("watchdog still running after the session ended")
	}
	r.noStop(t, "after the session ended")
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (stringIndex(s, sub) >= 0))
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
