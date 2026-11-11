package native

import (
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPlaceRect(t *testing.T) {
	cases := []struct {
		name   string
		screen Screen
		aspect float64
		want   Rect
	}{
		{"16:9 on a portrait kiosk", Screen{1080, 1920}, 16.0 / 9, Rect{0, 656, 1080, 608}},
		{"4:3 on a portrait kiosk", Screen{1080, 1920}, 4.0 / 3, Rect{0, 555, 1080, 810}},
		{"16:9 on a 16:9 screen", Screen{1920, 1080}, 16.0 / 9, Rect{0, 0, 1920, 1080}},
		{"4:3 limited by height", Screen{1920, 1080}, 4.0 / 3, Rect{240, 0, 1440, 1080}},
		{"square", Screen{1080, 1920}, 1, Rect{0, 420, 1080, 1080}},
	}
	for _, c := range cases {
		if got := PlaceRect(c.screen, c.aspect); got != c.want {
			t.Errorf("%s: PlaceRect = %+v, want %+v", c.name, got, c.want)
		}
	}
	for _, bad := range []struct {
		s Screen
		a float64
	}{{Screen{}, 1.5}, {Screen{1080, 1920}, 0}, {Screen{1080, 1920}, math.NaN()}} {
		if got := PlaceRect(bad.s, bad.a); got != (Rect{}) {
			t.Errorf("PlaceRect(%+v, %v) = %+v, want zero", bad.s, bad.a, got)
		}
	}
}

func TestWindowSpecJSON(t *testing.T) {
	var s WindowSpec
	if err := json.Unmarshal([]byte(`{"aspect":"16:9","align":"center"}`), &s); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(s.Aspect)-16.0/9) > 1e-9 {
		t.Fatalf("aspect = %v", s.Aspect)
	}
	if err := json.Unmarshal([]byte(`{"aspect":1.5}`), &s); err != nil || s.Aspect != 1.5 {
		t.Fatalf("number aspect: %v %v", s.Aspect, err)
	}
	for _, bad := range []string{`{"aspect":"wide"}`, `{"aspect":"16:0"}`, `{"aspect":true}`} {
		if err := json.Unmarshal([]byte(bad), &s); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestNormalizeWindow(t *testing.T) {
	w := &WindowSpec{Aspect: 16.0 / 9}
	if err := normalizeWindow(w); err != nil || w.Align != AlignCenter {
		t.Fatalf("default: %+v %v", w, err)
	}
	for _, bad := range []*WindowSpec{{Aspect: 0}, {Aspect: 9}, {Aspect: 1.5, Align: "left"}} {
		if err := normalizeWindow(bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if err := normalizeWindow(nil); err != nil {
		t.Fatal(err)
	}
}

type fakePlacer struct{ got chan WindowSpec }

func (f fakePlacer) Place(pid int, spec WindowSpec, stop <-chan struct{}, report func(Rect, Screen)) {
	f.got <- spec
	report(Rect{0, 656, 1080, 608}, Screen{1080, 1920})
	<-stop
}

func TestSessionPlacesWindowAndClearsOnEnd(t *testing.T) {
	rig := newRig(t, "wait")
	var mu sync.Mutex
	var events []WindowEvent
	rig.mgr.cfg.Placer = fakePlacer{got: make(chan WindowSpec, 1)}
	rig.mgr.cfg.OnWindow = func(ev WindowEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}
	spec := baseSpec()
	spec.Window = &WindowSpec{Aspect: 16.0 / 9}
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-rig.mgr.cfg.Placer.(fakePlacer).got:
		if got.Align != AlignCenter {
			t.Fatalf("placer spec = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("placer not called")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := rig.mgr.Stop("dev.example.arcade", false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Rect == nil || *events[0].Rect != (Rect{0, 656, 1080, 608}) || events[0].Screen != (Screen{1080, 1920}) {
		t.Fatalf("first = %+v", events[0])
	}
	if events[1].Rect != nil || events[1].AppID != "dev.example.arcade" {
		t.Fatalf("last = %+v", events[1])
	}
	raw, _ := json.Marshal(events[1])
	if !strings.Contains(string(raw), `"rect":null`) {
		t.Fatalf("null rect not serialised: %s", raw)
	}
}

func TestSessionWithoutWindowOnlyReportsEnd(t *testing.T) {
	rig := newRig(t, "wait")
	var mu sync.Mutex
	var events []WindowEvent
	rig.mgr.cfg.OnWindow = func(ev WindowEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}
	if _, err := rig.mgr.Launch(baseSpec()); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.mgr.Stop("dev.example.arcade", false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0].Rect != nil {
		t.Fatalf("events = %+v", events)
	}
}
