package pads

import (
	"context"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/pad/dsu"
	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

type fakeSink struct {
	mu           sync.Mutex
	set          map[int]dsu.PadState
	disconnected []int
}

func newSink() *fakeSink { return &fakeSink{set: map[int]dsu.PadState{}} }

func (f *fakeSink) SetSlot(slot int, s dsu.PadState) {
	f.mu.Lock()
	f.set[slot] = s
	f.mu.Unlock()
}

func (f *fakeSink) DisconnectSlot(slot int) {
	f.mu.Lock()
	delete(f.set, slot)
	f.disconnected = append(f.disconnected, slot)
	f.mu.Unlock()
}

func (f *fakeSink) get(slot int) (dsu.PadState, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.set[slot]
	return s, ok
}

func slots(m map[string]int) SlotFunc {
	return func(id string) (int, bool) {
		s, ok := m[id]
		return s, ok
	}
}

func stateOut(target string, s protocol.InputState) hub.Output {
	return hub.Output{Adapter: Name, Target: target, Kind: hub.KindState, Value: s}
}

func TestPlayerRouteUsesSlotAndStamps(t *testing.T) {
	players, system := newSink(), newSink()
	a := New(players, system, slots(map[string]int{"p1": 0, "p2": 2}))
	now := time.Unix(100, 0)
	a.start = now
	a.now = func() time.Time { return now }

	frame := protocol.InputState{TMs: 5000, Buttons: protocol.ButtonSouth, Accel: [3]float32{0, 0, 1}, Gyro: [3]float32{1, 2, 3}}
	if err := a.Deliver("p2", []hub.Output{stateOut("player", frame)}); err != nil {
		t.Fatal(err)
	}
	got, ok := players.get(2)
	if !ok || !got.Cross || got.Accel != [3]float32{0, 1, 0} || got.Gyro != [3]float32{1, 2, 3} {
		t.Fatalf("slot 2 %+v %v", got, ok)
	}
	first := got.MotionTimestamp
	if first == 0 {
		t.Fatal("timestamp not stamped")
	}

	// 16 ms later on the pad clock: +16000 µs, even if the kiosk saw it late.
	now = now.Add(40 * time.Millisecond)
	frame.TMs += 16
	_ = a.Deliver("p2", []hub.Output{stateOut("player", frame)})
	got, _ = players.get(2)
	if got.MotionTimestamp-first != 16000 {
		t.Fatalf("delta %d µs", got.MotionTimestamp-first)
	}

	// Spectators have no pad.
	if err := a.Deliver("p9", []hub.Output{stateOut("player", frame)}); err != nil {
		t.Fatal(err)
	}

	a.Release("p2")
	if _, ok := players.get(2); ok || len(players.disconnected) != 1 || players.disconnected[0] != 2 {
		t.Fatalf("release %+v", players.disconnected)
	}
}

func TestRecenterRezerosPointer(t *testing.T) {
	players := newSink()
	a := New(players, nil, slots(map[string]int{"p1": 0}))
	frame := protocol.InputState{PX: 1000, PY: -2000}
	_ = a.Deliver("p1", []hub.Output{stateOut("player/pointer-stick", frame)})
	got, _ := players.get(0)
	if got.RX <= 0 {
		t.Fatalf("pointer before recenter %v", got.RX)
	}
	frame.Buttons = protocol.ButtonRecenter
	_ = a.Deliver("p1", []hub.Output{stateOut("player/pointer-stick", frame)})
	got, _ = players.get(0)
	if got.RX != 0 || got.RY != 0 || !got.Touch {
		t.Fatalf("after recenter %+v", got)
	}
	frame.Buttons = 0
	frame.PX = 1500
	_ = a.Deliver("p1", []hub.Output{stateOut("player/pointer-stick", frame)})
	got, _ = players.get(0)
	if want := float32(500) / 32767; got.RX != want {
		t.Fatalf("offset pointer %v want %v", got.RX, want)
	}
}

func TestSlotAndSystemTargets(t *testing.T) {
	players, system := newSink(), newSink()
	a := New(players, system, nil)
	_ = a.Deliver("p1", []hub.Output{stateOut("slot:3", protocol.InputState{Buttons: protocol.ButtonEast})})
	if got, ok := players.get(3); !ok || !got.Circle {
		t.Fatalf("slot:3 %+v", got)
	}
	err := a.Deliver("p1", []hub.Output{
		{Adapter: Name, Target: "system:Start", Kind: hub.KindButton, Value: true},
		{Adapter: Name, Target: "system:West", Kind: "press"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := system.get(0)
	if !got.Options || !got.Square {
		t.Fatalf("system %+v", got)
	}
	_ = a.Deliver("p1", []hub.Output{{Adapter: Name, Target: "system:Start", Kind: hub.KindButton, Value: false}})
	got, _ = system.get(0)
	if got.Options || !got.Square {
		t.Fatalf("system release %+v", got)
	}
	if err := a.Deliver("p1", []hub.Output{{Adapter: Name, Target: "system:Home", Kind: "tap"}}); err == nil {
		t.Fatal("Home must not be bindable on the system pad")
	}
	if err := a.Deliver("p1", []hub.Output{stateOut("slot:9", protocol.InputState{})}); err != nil {
		t.Fatal(err)
	}
}

func TestMotionClock(t *testing.T) {
	var c motionClock
	a := c.stamp(1_000_000, 100)
	b := c.stamp(1_020_000, 116)
	if b-a != 16_000 {
		t.Fatalf("delta %d", b-a)
	}
	// Same pad millisecond: still strictly increasing.
	if d := c.stamp(1_020_000, 116); d <= b {
		t.Fatal("not monotonic")
	}
	// Pad reload: clock jumps back, re-anchor on the kiosk clock.
	e := c.stamp(5_000_000, 3)
	if e != 5_000_000 {
		t.Fatalf("re-anchor %d", e)
	}
	// u32 wrap counts as a small step.
	c = motionClock{}
	c.stamp(1_000_000, 0xFFFFFFF0)
	if got := c.stamp(1_000_000, 0x10); got != 1_000_000+0x20*1000 {
		t.Fatalf("wrap %d", got)
	}
	// Drift beyond the bound re-anchors.
	c = motionClock{}
	c.stamp(1_000_000, 0)
	for i := uint32(1); i <= 100; i++ {
		c.stamp(1_000_000, i*10) // kiosk clock stuck, pad keeps going
	}
	if c.lastUS > 1_000_000+maxDrift+1000 {
		t.Fatalf("drifted to %d", c.lastUS)
	}
}

// TestListenRealServers checks the adapter drives real DSU servers.
func TestListenRealServers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := Listen(ctx, "127.0.0.1:0", "127.0.0.1:0", slots(map[string]int{"p1": 1}))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Deliver("p1", []hub.Output{stateOut("player", protocol.InputState{Buttons: protocol.ButtonSouth})}); err != nil {
		t.Fatal(err)
	}
	a.Release("p1")
}

func touchOut(target string, ts protocol.TouchState) hub.Output {
	return hub.Output{Adapter: Name, Target: target, Kind: hub.KindTouch, Value: ts}
}

func TestSurfaceTouchScalesAndKeepsAcrossStateFrames(t *testing.T) {
	players := newSink()
	a := New(players, nil, nil)
	// A state frame first, so the slot has buttons.
	_ = a.Deliver("p1", []hub.Output{stateOut("slot:0", protocol.InputState{Buttons: protocol.ButtonSouth})})
	if got, _ := players.get(0); got.Touches[0].Active {
		t.Fatal("touch active without a touch frame")
	}

	down := protocol.TouchState{Points: [2]protocol.TouchPoint{
		{Down: true, X: 0, Y: 0}, {Down: true, X: 65535, Y: 65535},
	}}
	_ = a.Deliver("p1", []hub.Output{touchOut("slot:0", down)})
	got, _ := players.get(0)
	if !got.Cross {
		t.Fatal("touch frame lost the buttons")
	}
	if got.Touches[0] != (dsu.TouchPoint{Active: true, ID: 1, X: 0, Y: 0}) ||
		got.Touches[1] != (dsu.TouchPoint{Active: true, ID: 2, X: 1919, Y: 941}) {
		t.Fatalf("touches %+v", got.Touches)
	}

	// Centre.
	mid := protocol.TouchState{Points: [2]protocol.TouchPoint{{Down: true, X: 32768, Y: 32768}}}
	_ = a.Deliver("p1", []hub.Output{touchOut("slot:0", mid)})
	got, _ = players.get(0)
	if got.Touches[0].X != 959 || got.Touches[0].Y != 470 || got.Touches[0].ID != 1 || got.Touches[1].Active {
		t.Fatalf("mid %+v", got.Touches)
	}

	// A later state frame keeps the touch.
	_ = a.Deliver("p1", []hub.Output{stateOut("slot:0", protocol.InputState{})})
	got, _ = players.get(0)
	if !got.Touches[0].Active || got.Cross {
		t.Fatalf("state frame dropped touch or kept button: %+v", got)
	}

	// Lift, then touch again: a new contact id.
	_ = a.Deliver("p1", []hub.Output{touchOut("slot:0", protocol.TouchState{})})
	got, _ = players.get(0)
	if got.Touches[0].Active {
		t.Fatal("release ignored")
	}
	_ = a.Deliver("p1", []hub.Output{touchOut("slot:0", mid)})
	got, _ = players.get(0)
	if got.Touches[0].ID == 1 {
		t.Fatalf("contact id not incremented: %+v", got.Touches[0])
	}

	// Release clears touches with the pad.
	a.Release("p1")
	_ = a.Deliver("p1", []hub.Output{stateOut("slot:0", protocol.InputState{})})
	if got, _ := players.get(0); got.Touches[0].Active {
		t.Fatal("touch survived Release")
	}
}

func TestTouchPointerFromPointerAxes(t *testing.T) {
	players := newSink()
	a := New(players, nil, nil)
	cases := []struct {
		px, py int16
		x, y   uint16
	}{
		{-32767, -32767, 0, 0},
		{32767, 32767, 1919, 941},
		{0, 0, 959, 470},
	}
	for _, c := range cases {
		_ = a.Deliver("p1", []hub.Output{stateOut("slot:1/pointer-stick/touch-pointer", protocol.InputState{PX: c.px, PY: c.py})})
		got, _ := players.get(1)
		tp := got.Touches[0]
		if !tp.Active || tp.X != c.x || tp.Y != c.y || got.Touches[1].Active {
			t.Fatalf("pointer %d,%d -> %+v", c.px, c.py, got.Touches)
		}
		if got.RX == 0 && c.px != 0 {
			t.Fatal("suffixes did not combine: pointer-stick lost")
		}
	}
	got, _ := players.get(1)
	if got.Touches[0].ID != 1 {
		t.Fatalf("pointer contact id changed: %d", got.Touches[0].ID)
	}
	// Dropping the suffix drops the pointer touch.
	_ = a.Deliver("p1", []hub.Output{stateOut("slot:1", protocol.InputState{})})
	if got, _ := players.get(1); got.Touches[0].Active {
		t.Fatal("pointer touch stayed without the suffix")
	}
}
