package hub

import (
	"testing"

	"kiosk/controller/internal/pad/protocol"
)

func TestTouchRoutesThroughTopProfile(t *testing.T) {
	pads := &recAdapter{name: "pads"}
	other := &recAdapter{name: "other"}
	app := &recProvider{name: "app", profile: Profile{Routes: []Route{
		{Kind: KindTouch, Adapter: "other", Target: "never"},
	}}}
	native := &recProvider{name: "native", profile: Profile{Routes: []Route{
		{Kind: KindState, Adapter: "pads", Target: "slot:0"},
		{Kind: KindTouch, Adapter: "pads", Target: "pad"},
		{Kind: KindTouch, Adapter: "pads", Target: "pad2"},
	}}}
	if err := native.profile.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	stack, err := New(&recProvider{name: "shell"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = stack.RegisterAdapter(pads)
	_ = stack.RegisterAdapter(other)

	ts := protocol.TouchState{Seq: 3, Points: [2]protocol.TouchPoint{{Down: true, X: 10, Y: 20}}}

	// No provider route: dropped without error.
	if err := stack.Touch("p1", ts); err != nil {
		t.Fatal(err)
	}
	if pads.batchCount() != 0 {
		t.Fatal("touch delivered without a route")
	}

	_ = stack.Push(LayerApp, app)
	_ = stack.Push(LayerNative, native)
	if err := stack.Touch("p1", ts); err != nil {
		t.Fatal(err)
	}
	if other.batchCount() != 0 {
		t.Fatal("lower layer received touch")
	}
	if pads.batchCount() != 1 || len(pads.batches[0]) != 2 {
		t.Fatalf("batches %+v", pads.batches)
	}
	out := pads.batches[0][0]
	got, ok := out.Value.(protocol.TouchState)
	if out.Kind != KindTouch || out.Target != "pad" || !ok || got != ts {
		t.Fatalf("output %+v", out)
	}
	// Touch frames never reach the provider's input handler.
	if native.inputCount() != 0 {
		t.Fatal("touch reached HandleInput")
	}
	if err := stack.Touch("", ts); err != ErrEmptyPlayer {
		t.Fatalf("empty player: %v", err)
	}
}

func TestTouchRouteValidation(t *testing.T) {
	bad := Profile{Routes: []Route{{Kind: KindTouch, Control: "x", Adapter: "a", Target: "t"}}}
	if bad.Validate() == nil {
		t.Fatal("touch route with a control id must be invalid")
	}
	// Event routes ignore touch routes and the reverse.
	p := Profile{Routes: []Route{{Kind: KindTouch, Adapter: "a", Target: "t"}}}
	if len(p.outputsForEvent(protocol.Event{Control: "x", Kind: "press"})) != 0 {
		t.Fatal("event matched a touch route")
	}
	if len(p.outputsForInput(protocol.InputState{})) != 0 {
		t.Fatal("input matched a touch route")
	}
}
