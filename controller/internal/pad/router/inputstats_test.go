package router

import (
	"testing"

	"kiosk/controller/internal/pad/protocol"
)

func TestInputStatsReachTheHook(t *testing.T) {
	var seen []protocol.InputStats
	r, err := New(Config{InputStats: func(_ string, s protocol.InputStats) { seen = append(seen, s) }})
	if err != nil {
		t.Fatal(err)
	}
	sess := &fakeSession{}
	r.Attach(sess)
	sess.fireControl("p1", protocol.InputStats{T: "input_stats", WindowMs: 30000, Layout: "gamepad:portrait", Assist: true, Counts: map[string]float64{"dead": 2}})
	if len(seen) != 1 || seen[0].Layout != "gamepad:portrait" || seen[0].Counts["dead"] != 2 {
		t.Fatalf("seen %+v", seen)
	}
	// Without a hook it is dropped quietly.
	r2, _ := New(Config{})
	s2 := &fakeSession{}
	r2.Attach(s2)
	s2.fireControl("p1", protocol.InputStats{T: "input_stats", Layout: "x", Counts: map[string]float64{}})
}
