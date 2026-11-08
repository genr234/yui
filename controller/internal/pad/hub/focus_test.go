package hub

import (
	"errors"
	"sync"
	"testing"

	"kiosk/controller/internal/pad/protocol"
)

func southProvider(name string) *recProvider {
	return &recProvider{name: name, profile: Profile{Routes: []Route{
		{Button: protocol.ButtonSouth, Adapter: "pads", Target: name + "-south"},
	}}}
}

func lastOutputs(a *recAdapter) []Output {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.batches) == 0 {
		return nil
	}
	return a.batches[len(a.batches)-1]
}

func TestFocusChangeReleasesHeldButtons(t *testing.T) {
	pads := &recAdapter{name: "pads"}
	app := southProvider("app")
	stack, err := New(&recProvider{name: "shell"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.RegisterAdapter(pads); err != nil {
		t.Fatal(err)
	}
	if err := stack.Push(LayerApp, app); err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonSouth}); err != nil {
		t.Fatal(err)
	}
	if out := lastOutputs(pads); len(out) != 1 || out[0].Value != true {
		t.Fatalf("press = %+v", out)
	}

	// A native session takes focus while South is still held.
	if err := stack.Push(LayerNative, &recProvider{name: "native"}); err != nil {
		t.Fatal(err)
	}
	out := lastOutputs(pads)
	if len(out) != 1 || out[0].Target != "app-south" || out[0].Value != false {
		t.Fatalf("release on push = %+v", out)
	}

	// Pushing below the top changes nothing for the player.
	before := pads.batchCount()
	if err := stack.Push(LayerApp, southProvider("app2")); err != nil {
		t.Fatal(err)
	}
	if pads.batchCount() != before {
		t.Fatal("push below the top sent outputs")
	}

	// Popping the top releases on the popped provider's routes.
	native := southProvider("native2")
	if err := stack.Push(LayerNative, native); err != nil {
		t.Fatal(err)
	}
	if err := stack.Input("p1", protocol.InputState{Buttons: protocol.ButtonSouth}); err != nil {
		t.Fatal(err)
	}
	if err := stack.Pop(LayerNative); err != nil {
		t.Fatal(err)
	}
	out = lastOutputs(pads)
	if len(out) != 1 || out[0].Target != "native2-south" || out[0].Value != false {
		t.Fatalf("release on pop = %+v", out)
	}
}

func TestFocusFuncReportsNewTop(t *testing.T) {
	stack, err := New(&recProvider{name: "shell"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []string
	stack.SetFocusFunc(func(layer Layer, provider Provider) {
		mu.Lock()
		got = append(got, layer.String()+":"+provider.(*recProvider).name)
		mu.Unlock()
	})
	app := &recProvider{name: "app"}
	native := &recProvider{name: "native"}
	steps := []func() error{
		func() error { return stack.Push(LayerApp, app) },
		func() error { return stack.Push(LayerNative, native) },
		func() error { return stack.Push(LayerApp, &recProvider{name: "hidden"}) },
		func() error { return stack.Pop(LayerNative) },
		func() error { return stack.Pop(LayerApp) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"app:app", "native:native", "app:hidden", "shell:shell"}
	if len(got) != len(want) {
		t.Fatalf("focus = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("focus = %v, want %v", got, want)
		}
	}
}

func TestForgetReleasesAndResetsHome(t *testing.T) {
	pads := &recAdapter{name: "pads"}
	homes := 0
	stack, err := New(southProvider("shell"), func(string, HomeVia) { homes++ })
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.RegisterAdapter(pads); err != nil {
		t.Fatal(err)
	}
	held := protocol.InputState{Buttons: protocol.ButtonSouth | protocol.ButtonHome}
	if err := stack.Input("p1", held); err != nil {
		t.Fatal(err)
	}
	if err := stack.Forget("p1"); err != nil {
		t.Fatal(err)
	}
	if out := lastOutputs(pads); len(out) != 1 || out[0].Value != false {
		t.Fatalf("release on forget = %+v", out)
	}
	if err := stack.Input("p1", held); err != nil {
		t.Fatal(err)
	}
	if homes != 2 {
		t.Fatalf("home edges = %d, want 2", homes)
	}
	before := pads.batchCount()
	if err := stack.Forget("nobody"); err != nil || pads.batchCount() != before {
		t.Fatalf("forget unknown: err=%v batches=%d", err, pads.batchCount())
	}
	if err := stack.Forget(""); !errors.Is(err, ErrEmptyPlayer) {
		t.Fatalf("forget empty: %v", err)
	}
}

func TestEventRouteKindFilters(t *testing.T) {
	keys := &recAdapter{name: "keys"}
	provider := &recProvider{name: "app", profile: Profile{Routes: []Route{
		{Control: "fire", Kind: "press", Adapter: "keys", Target: "space-down"},
		{Control: "fire", Kind: "release", Adapter: "keys", Target: "space-up"},
		{Control: "fire", Adapter: "keys", Target: "log"},
	}}}
	stack, err := New(provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := stack.RegisterAdapter(keys); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"press", "release"} {
		if err := stack.Event("p1", protocol.Event{Control: "fire", Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	keys.mu.Lock()
	defer keys.mu.Unlock()
	if len(keys.batches) != 2 {
		t.Fatalf("batches = %+v", keys.batches)
	}
	press, release := keys.batches[0], keys.batches[1]
	if len(press) != 2 || press[0].Target != "space-down" || press[0].Kind != "press" || press[1].Target != "log" {
		t.Fatalf("press = %+v", press)
	}
	if len(release) != 2 || release[0].Target != "space-up" || release[0].Kind != "release" || release[1].Target != "log" {
		t.Fatalf("release = %+v", release)
	}
}

func TestTextRouteRejectsControl(t *testing.T) {
	p := Profile{Routes: []Route{{Kind: KindText, Control: "name", Adapter: "cdp", Target: "text"}}}
	if err := p.Validate(); !errors.Is(err, ErrBadRoute) {
		t.Fatalf("validate = %v", err)
	}
	if out := p.outputsForText(protocol.Text{Op: "insert", Text: "a"}); len(out) != 0 {
		t.Fatalf("invalid text route delivered %+v", out)
	}
}
