package session

import (
	"sync"
	"testing"
)

func TestPlayerHandlerUpdatesAndLeaves(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	var mu sync.Mutex
	var seen []string
	mgr.SetPlayerHandler(func(ev PlayerEvent) {
		mu.Lock()
		seen = append(seen, string(ev.Kind)+":"+ev.Player.ID)
		mu.Unlock()
	})
	cancel, done := start(t, f, mgr)

	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 1, true, true)})
	f.push(t, map[string]any{"type": "player_updated", "player": playerFixture("p1", 1, false, true)})
	f.push(t, map[string]any{"type": "player_left", "player": playerFixture("p1", 1, false, false)})
	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p2", 2, false, true)})
	waitFor(t, "p2 in roster", func() bool { return len(mgr.Snapshot().Players) == 2 })
	stop(t, cancel, done)

	mu.Lock()
	defer mu.Unlock()
	want := []string{"updated:p1", "left:p1"}
	for i, w := range want {
		if i >= len(seen) || seen[i] != w {
			t.Fatalf("events %v, want prefix %v", seen, want)
		}
	}
	// Ending the session reports every remaining player as left.
	if len(seen) < 4 {
		t.Fatalf("end events %v", seen)
	}
	tail := seen[len(seen)-2:]
	if tail[0] != "left:p1" || tail[1] != "left:p2" {
		t.Fatalf("end events %v", seen)
	}
}
