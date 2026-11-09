package cdp

import (
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestSwipeTargetBuildsAndRejects(t *testing.T) {
	target, err := SwipeTarget(map[string]string{"up": "ArrowUp", "left": "ArrowLeft", "right": "d"})
	if err != nil || target != "swipe:up=ArrowUp,left=ArrowLeft,right=d" {
		t.Fatalf("target %q %v", target, err)
	}
	if _, err := SwipeTarget(map[string]string{"up": "F13"}); err == nil {
		t.Fatal("unknown key accepted")
	}
	if _, err := SwipeTarget(map[string]string{"sideways": "a"}); err == nil {
		t.Fatal("unknown direction accepted")
	}
	if _, err := SwipeTarget(nil); err == nil {
		t.Fatal("empty swipe accepted")
	}
	if MouseTarget(nil) != "mouse" || MouseTarget(&Rect{X: 10, Y: 20.5, W: 300, H: 200}) != "mouse:10,20.5,300,200" {
		t.Fatal("mouse target")
	}
}

func TestSwipeGesturesTapArrowKeys(t *testing.T) {
	r := newRig(t)
	clock := &fakeClock{t: time.Unix(1000, 0)}
	r.adapter.now = clock.now
	target := "swipe:up=ArrowUp,down=ArrowDown,left=ArrowLeft,right=ArrowRight"
	change := func(dx, dy float64) {
		t.Helper()
		err := r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: target, Kind: "change", Value: map[string]any{"dx": dx, "dy": dy, "fingers": 1.0}}})
		if err != nil {
			t.Fatal(err)
		}
		clock.advance(16 * time.Millisecond)
	}
	expectTap := func(key string) {
		t.Helper()
		down, up := r.target.next(t), r.target.next(t)
		if down.Params["key"] != key || down.Params["type"] != "rawKeyDown" || up.Params["type"] != "keyUp" || up.Params["key"] != key {
			t.Fatalf("want %s tap, got %v / %v", key, down.Params, up.Params)
		}
	}

	// A short wiggle is not a swipe.
	change(10, 3)
	change(-8, 2)
	r.target.none(t)

	// A new gesture after a pause: mostly left.
	clock.advance(SwipeGap + time.Millisecond)
	change(-20, 4)
	change(-20, 4)
	expectTap("ArrowLeft")
	// The same gesture keeps moving: no second tap.
	change(-40, 0)
	r.target.none(t)

	// Next gesture: up (trackpad +dy is down, so negative dy is up).
	clock.advance(SwipeGap + time.Millisecond)
	change(3, -50)
	expectTap("ArrowUp")

	clock.advance(SwipeGap + time.Millisecond)
	change(0, 20)
	change(5, 30)
	expectTap("ArrowDown")

	// Players have their own gestures.
	clock.advance(SwipeGap + time.Millisecond)
	_ = r.adapter.Deliver("p2", []hub.Output{{Adapter: Name, Target: target, Kind: "change", Value: map[string]any{"dx": 50.0, "dy": 0.0}}})
	expectTap("ArrowRight")

	// A pad that recognises swipes itself.
	_ = r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: target, Kind: "swipe", Value: "down"}})
	expectTap("ArrowDown")
	_ = r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: target, Kind: "swipe", Value: map[string]any{"direction": "left"}}})
	expectTap("ArrowLeft")
	// Taps are not swipes.
	_ = r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: target, Kind: "tap", Value: map[string]any{"fingers": 1.0}}})
	r.target.none(t)

	if err := r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: "swipe:up=F13", Kind: "swipe", Value: "up"}}); err == nil {
		t.Fatal("bad target accepted")
	}
}

func TestAbsoluteTrackpadDragsTheMouse(t *testing.T) {
	r := newRig(t)
	target := MouseTarget(&Rect{X: 100, Y: 50, W: 400, H: 200})
	frame := func(px, py int16) {
		t.Helper()
		st := protocol.InputState{Seq: 1, TMs: 99, PX: px, PY: py}
		if err := r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: target, Kind: hub.KindState, Value: st}}); err != nil {
			t.Fatal(err)
		}
	}
	event := func(kind string) {
		t.Helper()
		if err := r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: target, Kind: kind}}); err != nil {
			t.Fatal(err)
		}
	}

	// Frames before a press (or from another control) do nothing.
	frame(0, 0)
	r.target.none(t)

	// Press arms; the next frame moves to the touch point and presses.
	event("press")
	r.target.none(t)
	frame(-32767, -32767) // top-left of the region
	move, press := r.target.next(t), r.target.next(t)
	if move.Params["type"] != "mouseMoved" || num(move.Params, "x") != 100 || num(move.Params, "y") != 50 {
		t.Fatalf("move %v", move.Params)
	}
	if press.Params["type"] != "mousePressed" || num(press.Params, "buttons") != 1 || num(press.Params, "x") != 100 {
		t.Fatalf("press %v", press.Params)
	}
	// Dragging to the centre of the region.
	frame(0, 0)
	drag := r.target.next(t)
	if drag.Params["type"] != "mouseMoved" || num(drag.Params, "buttons") != 1 || num(drag.Params, "x") != 300 || num(drag.Params, "y") != 150 {
		t.Fatalf("drag %v", drag.Params)
	}
	event("release")
	up := r.target.next(t)
	if up.Params["type"] != "mouseReleased" || num(up.Params, "x") != 300 || num(up.Params, "y") != 150 {
		t.Fatalf("release %v", up.Params)
	}

	// Without a region the pointer spans the viewport (1000×500).
	full := MouseTarget(nil)
	_ = r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: full, Kind: "press"}})
	_ = r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: full, Kind: hub.KindState, Value: protocol.InputState{TMs: 5, PX: 32767, PY: 32767}}})
	if m := r.target.next(t); num(m.Params, "x") != 999 || num(m.Params, "y") != 499 {
		t.Fatalf("viewport move %v", m.Params)
	}
	r.target.next(t) // press
	// The hub's neutral frame (focus change, player left) releases.
	_ = r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: full, Kind: hub.KindState, Value: protocol.InputState{}}})
	if m := r.target.next(t); m.Params["type"] != "mouseReleased" {
		t.Fatalf("neutral %v", m.Params)
	}
	r.target.none(t)

	// A press released before any frame is a click where the cursor is.
	event("press")
	event("release")
	if a, b := r.target.next(t), r.target.next(t); a.Params["type"] != "mousePressed" || b.Params["type"] != "mouseReleased" {
		t.Fatalf("tap %v / %v", a.Params, b.Params)
	}

	if err := r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: "mouse:1,2", Kind: "press"}}); err == nil {
		t.Fatal("bad mouse target accepted")
	}
}
