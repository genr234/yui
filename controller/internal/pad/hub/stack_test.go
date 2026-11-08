package hub

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

type recProvider struct {
	NopProvider
	name    string
	profile Profile

	mu      sync.Mutex
	inputs  []frame
	events  []protocol.Event
	texts   []protocol.Text
	systems []string
	layouts int
	onInput func(protocol.InputState)
}

type frame struct {
	player  string
	buttons uint32
	seq     uint16
}

func (r *recProvider) Layout(protocol.Player) protocol.Node {
	r.mu.Lock()
	r.layouts++
	r.mu.Unlock()
	return protocol.Node{Type: r.name}
}

func (r *recProvider) Profile() Profile { return r.profile }

func (r *recProvider) HandleInput(playerID string, state protocol.InputState) {
	if r.onInput != nil {
		r.onInput(state)
	}
	r.mu.Lock()
	r.inputs = append(r.inputs, frame{player: playerID, buttons: state.Buttons, seq: state.Seq})
	r.mu.Unlock()
}

func (r *recProvider) HandleEvent(playerID string, ev protocol.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *recProvider) HandleText(playerID string, msg protocol.Text) {
	r.mu.Lock()
	r.texts = append(r.texts, msg)
	r.mu.Unlock()
}

func (r *recProvider) HandleSystem(playerID string, msg protocol.System) {
	r.mu.Lock()
	r.systems = append(r.systems, msg.Action)
	r.mu.Unlock()
}

func (r *recProvider) inputCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.inputs)
}

func (r *recProvider) sawHome() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, in := range r.inputs {
		if in.buttons&protocol.ButtonHome != 0 {
			return true
		}
	}
	return false
}

type recAdapter struct {
	name string
	err  error

	mu      sync.Mutex
	batches [][]Output
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (a *recAdapter) Name() string { return a.name }

func (a *recAdapter) Deliver(playerID string, outputs []Output) error {
	if a.entered != nil {
		a.once.Do(func() { close(a.entered) })
		<-a.release
	}
	a.mu.Lock()
	cp := make([]Output, len(outputs))
	copy(cp, outputs)
	a.batches = append(a.batches, cp)
	a.mu.Unlock()
	return a.err
}

func (a *recAdapter) batchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.batches)
}

func player(id string) protocol.Player {
	return protocol.Player{ID: id, Name: id, Color: "#fff", Connected: true}
}

func TestNewRejectsNilShell(t *testing.T) {
	if _, err := New(nil, nil); !errors.Is(err, ErrNilProvider) {
		t.Fatalf("New: %v", err)
	}
}

func TestFocusPrecedence(t *testing.T) {
	shell := &recProvider{name: "shell"}
	app := &recProvider{name: "app"}
	native := &recProvider{name: "native"}
	other := &recProvider{name: "other-app"}
	stack, err := New(shell, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertTop := func(want Layer, provider *recProvider) {
		t.Helper()
		layer, got := stack.Top()
		if layer != want || got != Provider(provider) {
			t.Fatalf("top = %s %v, want %s %s", layer, got, want, provider.name)
		}
		node := stack.Layout(player("p1"))
		if node.Type != provider.name {
			t.Fatalf("layout %q, want %q", node.Type, provider.name)
		}
		state := protocol.InputState{Buttons: protocol.ButtonSouth, Seq: 1}
		if err := stack.Input("p1", state); err != nil {
			t.Fatal(err)
		}
	}

	assertTop(LayerShell, shell)

	if err := stack.Push(LayerApp, app); err != nil {
		t.Fatal(err)
	}
	assertTop(LayerApp, app)
	if stack.At(LayerShell) != shell {
		t.Fatal("shell left the bottom")
	}

	if err := stack.Push(LayerNative, native); err != nil {
		t.Fatal(err)
	}
	assertTop(LayerNative, native)

	if err := stack.Pop(LayerApp); err != nil {
		t.Fatal(err)
	}
	if stack.At(LayerApp) != nil {
		t.Fatal("app still installed")
	}
	assertTop(LayerNative, native)

	if err := stack.Push(LayerApp, other); err != nil {
		t.Fatal(err)
	}
	assertTop(LayerNative, native)

	if err := stack.Pop(LayerNative); err != nil {
		t.Fatal(err)
	}
	assertTop(LayerApp, other)

	if err := stack.Pop(LayerApp); err != nil {
		t.Fatal(err)
	}
	assertTop(LayerShell, shell)

	if err := stack.Pop(LayerShell); !errors.Is(err, ErrPopShell) {
		t.Fatalf("pop shell: %v", err)
	}
	if err := stack.Pop(LayerApp); !errors.Is(err, ErrLayerEmpty) {
		t.Fatalf("pop empty app: %v", err)
	}
	if err := stack.Pop(Layer(9)); !errors.Is(err, ErrBadLayer) {
		t.Fatalf("pop bad layer: %v", err)
	}
	if err := stack.Push(LayerApp, nil); !errors.Is(err, ErrNilProvider) {
		t.Fatalf("push nil: %v", err)
	}
	assertTop(LayerShell, shell)

	if shell.inputCount() == 0 || app.inputCount() == 0 || native.inputCount() == 0 || other.inputCount() == 0 {
		t.Fatalf("counts shell=%d app=%d native=%d other=%d", shell.inputCount(), app.inputCount(), native.inputCount(), other.inputCount())
	}
}

func TestProviderSwapLeavesInFlightOnOldProvider(t *testing.T) {
	old := &recProvider{name: "old"}
	next := &recProvider{name: "next"}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	old.onInput = func(state protocol.InputState) {
		once.Do(func() { close(entered) })
		<-release
		if state.Buttons&protocol.ButtonHome != 0 {
			t.Errorf("in-flight frame still has Home: %x", state.Buttons)
		}
	}
	stack, err := New(old, nil)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- stack.Input("p1", protocol.InputState{
			Seq:     1,
			Buttons: protocol.ButtonWest | protocol.ButtonHome,
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("input did not enter the old provider")
	}

	if err := stack.Push(LayerShell, next); err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p2", protocol.InputState{Seq: 2, Buttons: protocol.ButtonEast}); err != nil {
		t.Fatal(err)
	}
	if next.inputCount() != 1 {
		t.Fatalf("next inputs = %d, want 1 while old is still inside HandleInput", next.inputCount())
	}
	if old.inputCount() != 0 {
		t.Fatalf("old recorded input before release: %d", old.inputCount())
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if old.inputCount() != 1 || old.sawHome() {
		t.Fatalf("old inputs=%d home=%v", old.inputCount(), old.sawHome())
	}
	old.mu.Lock()
	if old.inputs[0].buttons != protocol.ButtonWest || old.inputs[0].player != "p1" {
		t.Fatalf("old frame = %+v", old.inputs[0])
	}
	old.mu.Unlock()
	next.mu.Lock()
	if next.inputs[0].player != "p2" || next.inputs[0].buttons != protocol.ButtonEast {
		t.Fatalf("next frame = %+v", next.inputs[0])
	}
	next.mu.Unlock()
}

func TestPopAndPushFromHandlerDoNotDeadlock(t *testing.T) {
	stack, err := New(&recProvider{name: "shell"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	app := &recProvider{name: "app"}
	app.onInput = func(protocol.InputState) {
		if err := stack.Pop(LayerApp); err != nil {
			t.Errorf("pop: %v", err)
		}
		replacement := &recProvider{name: "shell-2"}
		if err := stack.Push(LayerShell, replacement); err != nil {
			t.Errorf("push: %v", err)
		}
	}
	if err := stack.Push(LayerApp, app); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonNorth})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler deadlock")
	}
	layer, _ := stack.Top()
	if layer != LayerShell {
		t.Fatalf("top after pop-from-handler = %s", layer)
	}
}

func TestHomeButtonIsRisingEdgeAndStripped(t *testing.T) {
	provider := &recProvider{name: "shell"}
	var vias []HomeVia
	var mu sync.Mutex
	stack, err := New(provider, func(id string, via HomeVia) {
		if id != "p1" {
			t.Errorf("player %s", id)
		}
		mu.Lock()
		vias = append(vias, via)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	frames := []uint32{
		protocol.ButtonHome | protocol.ButtonSouth,
		protocol.ButtonHome | protocol.ButtonSouth,
		protocol.ButtonSouth,
		protocol.ButtonHome,
		0,
	}
	for i, buttons := range frames {
		if err := stack.Input("p1", protocol.InputState{Seq: uint16(i), Buttons: buttons}); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	if len(vias) != 2 || vias[0] != HomeViaButton || vias[1] != HomeViaButton {
		t.Fatalf("vias = %v", vias)
	}
	mu.Unlock()
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.inputs) != len(frames) {
		t.Fatalf("inputs = %d", len(provider.inputs))
	}
	want := []uint32{protocol.ButtonSouth, protocol.ButtonSouth, protocol.ButtonSouth, 0, 0}
	for i, frame := range provider.inputs {
		if frame.buttons != want[i] {
			t.Fatalf("frame %d buttons=%x want %x", i, frame.buttons, want[i])
		}
	}
}

func TestSystemHomeSkipsProvider(t *testing.T) {
	provider := &recProvider{name: "app"}
	var got atomic.Int32
	stack, err := New(&recProvider{name: "shell"}, func(string, HomeVia) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.Push(LayerApp, provider); err != nil {
		t.Fatal(err)
	}
	stack.home = func(id string, via HomeVia) {
		if id != "p9" || via != HomeViaSystem {
			t.Errorf("home %s %s", id, via)
		}
		got.Add(1)
	}
	if err := stack.System("p9", protocol.System{T: "system", Action: "home", Value: "sheet"}); err != nil {
		t.Fatal(err)
	}
	if err := stack.System("p9", protocol.System{T: "system", Action: "home"}); err != nil {
		t.Fatal(err)
	}
	if err := stack.System("p9", protocol.System{T: "system", Action: "quit"}); err != nil {
		t.Fatal(err)
	}
	if got.Load() != 2 {
		t.Fatalf("home calls = %d", got.Load())
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.systems) != 1 || provider.systems[0] != "quit" {
		t.Fatalf("systems = %v", provider.systems)
	}
}

func TestHomeFuncMayReenterInput(t *testing.T) {
	provider := &recProvider{name: "shell"}
	var stack *Stack
	var calls atomic.Int32
	var err error
	stack, err = New(provider, func(id string, via HomeVia) {
		if via != HomeViaButton {
			t.Errorf("via %s", via)
		}
		if calls.Add(1) != 1 {
			return
		}
		if err := stack.Input(id, protocol.InputState{Seq: 2, Buttons: protocol.ButtonSouth}); err != nil {
			t.Errorf("reenter: %v", err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p", protocol.InputState{Seq: 1, Buttons: protocol.ButtonHome}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("home calls = %d", calls.Load())
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.inputs) != 2 {
		t.Fatalf("inputs = %+v", provider.inputs)
	}
	if provider.inputs[0].seq != 2 || provider.inputs[0].buttons != protocol.ButtonSouth {
		t.Fatalf("inner frame = %+v", provider.inputs[0])
	}
	if provider.inputs[1].seq != 1 || provider.inputs[1].buttons != 0 {
		t.Fatalf("outer frame = %+v", provider.inputs[1])
	}
}

func TestHomeHandlerCanSwapProvider(t *testing.T) {
	first := &recProvider{name: "first"}
	second := &recProvider{name: "second"}
	var stack *Stack
	var err error
	stack, err = New(first, func(string, HomeVia) {
		if pushErr := stack.Push(LayerShell, second); pushErr != nil {
			t.Errorf("push: %v", pushErr)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonHome | protocol.ButtonL1}); err != nil {
		t.Fatal(err)
	}
	if first.inputCount() != 1 || first.sawHome() {
		t.Fatalf("first inputs=%d home=%v", first.inputCount(), first.sawHome())
	}
	if second.inputCount() != 0 {
		t.Fatal("swap during home leaked the in-flight frame")
	}
	if err := stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonR1}); err != nil {
		t.Fatal(err)
	}
	if second.inputCount() != 1 || first.inputCount() != 1 {
		t.Fatalf("first=%d second=%d", first.inputCount(), second.inputCount())
	}
}

func TestProfileRoutesAndReservedHome(t *testing.T) {
	pads := &recAdapter{name: "pads"}
	cdp := &recAdapter{name: "cdp"}
	apps := &recAdapter{name: "apps"}
	provider := &recProvider{
		name: "native",
		profile: Profile{Routes: []Route{
			{Button: protocol.ButtonSouth, Adapter: "pads", Target: "south"},
			{Button: protocol.ButtonHome, Adapter: "pads", Target: "home"},
			{Kind: KindState, Adapter: "pads", Target: "slot-0"},
			{Control: "trackpad", Adapter: "cdp", Target: "pointer"},
			{Kind: KindText, Adapter: "cdp", Target: "text"},
			{Control: "confirm", Adapter: "missing", Target: "ok"},
			{Kind: KindState, Adapter: "pads", Target: "nope", Button: protocol.ButtonEast},
		}},
	}
	if err := provider.profile.Validate(); !errors.Is(err, ErrReservedHome) || !errors.Is(err, ErrBadRoute) {
		t.Fatalf("validate: %v", err)
	}
	stack, err := New(&recProvider{name: "shell"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.Push(LayerNative, provider); err != nil {
		t.Fatal(err)
	}
	for _, adapter := range []Adapter{pads, cdp, apps} {
		if err := stack.RegisterAdapter(adapter); err != nil {
			t.Fatal(err)
		}
	}
	state := protocol.InputState{
		Seq:     4,
		Buttons: protocol.ButtonSouth | protocol.ButtonHome,
		LX:      100,
		Accel:   [3]float32{0, 1, 0},
	}
	if err := stack.Input("p1", state); err != nil {
		t.Fatal(err)
	}
	if pads.batchCount() != 1 {
		t.Fatalf("pad batches = %d", pads.batchCount())
	}
	pads.mu.Lock()
	batch := pads.batches[0]
	pads.mu.Unlock()
	if len(batch) != 2 {
		t.Fatalf("pad outputs = %+v", batch)
	}
	if batch[0].Target != "south" || batch[0].Kind != KindButton || batch[0].Value != true {
		t.Fatalf("south output = %+v", batch[0])
	}
	gotState, ok := batch[1].Value.(protocol.InputState)
	if !ok || batch[1].Kind != KindState || batch[1].Target != "slot-0" {
		t.Fatalf("state output = %+v", batch[1])
	}
	if gotState.Buttons&protocol.ButtonHome != 0 || gotState.Buttons != protocol.ButtonSouth || gotState.LX != 100 {
		t.Fatalf("forwarded state = %+v", gotState)
	}

	err = stack.Event("p1", protocol.Event{T: "event", Control: "confirm", Kind: "tap", Value: 1})
	if !errors.Is(err, ErrUnknownAdapter) {
		t.Fatalf("missing adapter: %v", err)
	}
	if err := stack.Event("p1", protocol.Event{T: "event", Control: "trackpad", Kind: "change", Value: []int{3, 4}}); err != nil {
		t.Fatal(err)
	}
	text := protocol.Text{T: "text", Op: "insert", Text: "ab"}
	if err := stack.Text("p1", text); err != nil {
		t.Fatal(err)
	}
	cdp.mu.Lock()
	defer cdp.mu.Unlock()
	if len(cdp.batches) != 2 {
		t.Fatalf("cdp batches = %+v", cdp.batches)
	}
	if cdp.batches[0][0].Kind != "change" || cdp.batches[0][0].Target != "pointer" {
		t.Fatalf("pointer = %+v", cdp.batches[0][0])
	}
	if cdp.batches[1][0].Value != text {
		t.Fatalf("text = %+v", cdp.batches[1][0])
	}
	if apps.batchCount() != 0 {
		t.Fatal("unrelated adapter received output")
	}
	if provider.inputCount() != 1 || len(provider.events) != 2 || len(provider.texts) != 1 {
		t.Fatalf("provider input=%d events=%d texts=%d", provider.inputCount(), len(provider.events), len(provider.texts))
	}
}

func TestAdapterSwapDuringDeliver(t *testing.T) {
	old := &recAdapter{name: "cdp", entered: make(chan struct{}), release: make(chan struct{})}
	next := &recAdapter{name: "cdp"}
	provider := &recProvider{
		name: "shell",
		profile: Profile{Routes: []Route{
			{Control: "key", Adapter: "cdp", Target: "key"},
		}},
	}
	stack, err := New(provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.RegisterAdapter(old); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- stack.Event("p1", protocol.Event{Control: "key", Kind: "press"})
	}()
	select {
	case <-old.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("deliver did not start")
	}
	if err := stack.RegisterAdapter(next); err != nil {
		t.Fatal(err)
	}
	if err := stack.Event("p2", protocol.Event{Control: "key", Kind: "tap"}); err != nil {
		t.Fatal(err)
	}
	if next.batchCount() != 1 || old.batchCount() != 0 {
		t.Fatalf("during swap next=%d old=%d", next.batchCount(), old.batchCount())
	}
	close(old.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if old.batchCount() != 1 || next.batchCount() != 1 {
		t.Fatalf("after swap next=%d old=%d", next.batchCount(), old.batchCount())
	}
}

func TestEmptyPlayerAndAdapterGuards(t *testing.T) {
	stack, err := New(&recProvider{name: "shell"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("", protocol.InputState{}); !errors.Is(err, ErrEmptyPlayer) {
		t.Fatalf("input: %v", err)
	}
	if err := stack.Event("", protocol.Event{}); !errors.Is(err, ErrEmptyPlayer) {
		t.Fatalf("event: %v", err)
	}
	if err := stack.Text("", protocol.Text{}); !errors.Is(err, ErrEmptyPlayer) {
		t.Fatalf("text: %v", err)
	}
	if err := stack.System("", protocol.System{}); !errors.Is(err, ErrEmptyPlayer) {
		t.Fatalf("system: %v", err)
	}
	if err := stack.RegisterAdapter(nil); !errors.Is(err, ErrNilAdapter) {
		t.Fatalf("nil adapter: %v", err)
	}
	if err := stack.RegisterAdapter(&recAdapter{}); !errors.Is(err, ErrBadAdapter) {
		t.Fatalf("empty name: %v", err)
	}
}

func TestConcurrentHomePressFiresOnce(t *testing.T) {
	provider := &recProvider{name: "shell"}
	var fires atomic.Int32
	stack, err := New(provider, func(string, HomeVia) { fires.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	const n = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := stack.Input("p", protocol.InputState{Buttons: protocol.ButtonHome | protocol.ButtonSouth}); err != nil {
				t.Errorf("input: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if fires.Load() != 1 {
		t.Fatalf("home fires = %d, want 1", fires.Load())
	}
	if provider.inputCount() != n || provider.sawHome() {
		t.Fatalf("inputs=%d home=%v", provider.inputCount(), provider.sawHome())
	}
}

func TestConcurrentInputPushPop(t *testing.T) {
	shell := &recProvider{name: "shell"}
	app := &recProvider{name: "app"}
	native := &recProvider{name: "native"}
	var homes atomic.Int32
	stack, err := New(shell, func(string, HomeVia) { homes.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.Push(LayerApp, app); err != nil {
		t.Fatal(err)
	}
	const (
		players = 4
		n       = 200
	)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if i%2 == 0 {
				if err := stack.Push(LayerNative, native); err != nil {
					t.Errorf("push: %v", err)
				}
			} else if err := stack.Pop(LayerNative); err != nil && !errors.Is(err, ErrLayerEmpty) {
				t.Errorf("pop: %v", err)
			}
		}
	}()
	for p := 0; p < players; p++ {
		wg.Add(1)
		id := fmt.Sprintf("p%d", p)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				buttons := uint32(protocol.ButtonSouth)
				if i%5 == 0 {
					buttons |= protocol.ButtonHome
				}
				err := stack.Input(id, protocol.InputState{Seq: uint16(i), Buttons: buttons, LX: int16(i)})
				if err != nil {
					t.Errorf("input: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	total := shell.inputCount() + app.inputCount() + native.inputCount()
	if total != players*n {
		t.Fatalf("delivered %d, want %d (shell=%d app=%d native=%d)", total, players*n, shell.inputCount(), app.inputCount(), native.inputCount())
	}
	if shell.sawHome() || app.sawHome() || native.sawHome() {
		t.Fatal("Home bit reached a provider")
	}
	// Each player's frames 0,5,10,... are rising edges because the frames
	// between them clear Home, and each player is driven by one goroutine.
	wantHomes := int32(players * ((n + 4) / 5))
	if homes.Load() != wantHomes {
		t.Fatalf("home fires = %d, want %d", homes.Load(), wantHomes)
	}
	layer, _ := stack.Top()
	if layer != LayerApp && layer != LayerNative {
		t.Fatalf("top = %s", layer)
	}
	if stack.At(LayerShell) != shell {
		t.Fatal("shell was removed")
	}
}

func TestButtonReleaseLevelAndNilHome(t *testing.T) {
	adapter := &recAdapter{name: "pads"}
	provider := &recProvider{
		name: "app",
		profile: Profile{Routes: []Route{
			{Button: protocol.ButtonEast, Adapter: "pads", Target: "east", Kind: KindButton},
		}},
	}
	stack, err := New(provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.RegisterAdapter(adapter); err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p", protocol.InputState{Buttons: protocol.ButtonEast}); err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p", protocol.InputState{}); err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p", protocol.InputState{Buttons: protocol.ButtonHome}); err != nil {
		t.Fatal(err)
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.batches) != 3 {
		t.Fatalf("batches = %d", len(adapter.batches))
	}
	if adapter.batches[0][0].Value != true || adapter.batches[1][0].Value != false || adapter.batches[2][0].Value != false {
		t.Fatalf("levels = %+v %+v %+v", adapter.batches[0][0], adapter.batches[1][0], adapter.batches[2][0])
	}
}

func TestLayerString(t *testing.T) {
	if LayerShell.String() != "shell" || LayerApp.String() != "app" || LayerNative.String() != "native" || Layer(9).String() != "unknown" {
		t.Fatal(LayerShell, LayerApp, LayerNative, Layer(9))
	}
}
