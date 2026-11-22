package inputstats

import (
	"testing"

	"kiosk/controller/internal/pad/protocol"
)

func TestAddSplitsByLayoutAndAssist(t *testing.T) {
	tot := New()
	tot.Add(protocol.InputStats{WindowMs: 30000, Layout: "gamepad:portrait", Assist: true, Counts: map[string]float64{"touches": 100, "dead": 2, "press": 50, "chord": 5}})
	tot.Add(protocol.InputStats{WindowMs: 10000, Layout: "gamepad:portrait", Assist: true, Counts: map[string]float64{"touches": 100, "dead": 2}, DeadAt: [][2]float64{{0.1, 0.9}}})
	tot.Add(protocol.InputStats{WindowMs: 30000, Layout: "gamepad:portrait", Assist: false, Counts: map[string]float64{"touches": 50, "gap_miss": 5}})

	groups := tot.Snapshot()["groups"].([]Group)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	plain, assist := groups[0], groups[1]
	if plain.Assist || !assist.Assist {
		t.Fatalf("order: plain first, got %+v", groups)
	}
	if assist.Windows != 2 || assist.PlayedMs != 40000 || assist.Counts["dead"] != 4 || assist.Counts["touches"] != 200 {
		t.Errorf("assist totals = %+v", assist)
	}
	if assist.Rates["dead"] != 2 || assist.Rates["chord"] != 10 {
		t.Errorf("assist rates = %+v", assist.Rates)
	}
	if plain.Rates["gap_miss"] != 10 {
		t.Errorf("plain rates = %+v", plain.Rates)
	}
	if len(assist.DeadAt) != 1 || assist.DeadAt[0] != [2]float64{0.1, 0.9} {
		t.Errorf("dead_at = %v", assist.DeadAt)
	}
}

func TestBounds(t *testing.T) {
	tot := New()
	for i := 0; i < MaxGroups+5; i++ {
		tot.Add(protocol.InputStats{Layout: string(rune('A'+i%26)) + string(rune('a'+i/26)), Counts: map[string]float64{"touches": 1}})
	}
	if n := len(tot.Snapshot()["groups"].([]Group)); n != MaxGroups {
		t.Errorf("groups = %d, want %d", n, MaxGroups)
	}
	pts := make([][2]float64, 60)
	for i := 0; i < 10; i++ {
		tot.Add(protocol.InputStats{Layout: "Aa", Counts: map[string]float64{}, DeadAt: pts})
	}
	for _, g := range tot.Snapshot()["groups"].([]Group) {
		if g.Layout == "Aa" && len(g.DeadAt) != MaxDeadAt {
			t.Errorf("dead_at kept %d, want %d", len(g.DeadAt), MaxDeadAt)
		}
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	tot := New()
	tot.Add(protocol.InputStats{Layout: "x", Counts: map[string]float64{"dead": 1}})
	g := tot.Snapshot()["groups"].([]Group)[0]
	g.Counts["dead"] = 99
	if tot.Snapshot()["groups"].([]Group)[0].Counts["dead"] != 1 {
		t.Error("snapshot shares the counts map")
	}
}
