package session

import (
	"sync"
	"testing"
)

func TestNewWiFiQR(t *testing.T) {
	w := NewWiFi("Yui KKTC", `a;b:c\d`)
	if w.QR != `WIFI:T:WPA;S:Yui KKTC;P:a\;b\:c\\d;;` {
		t.Fatalf("qr = %q", w.QR)
	}
}

// liveManager is a bare manager with an active session and no loops.
func liveManager() *Manager {
	return &Manager{
		record:  &SessionRecord{Code: "KKTC"},
		active:  true,
		joinURL: "https://yui.example/play/KKTC?t=AAAAAAAA",
		players: map[string]*playerState{},
	}
}

func TestWiFiPendingUntilSet(t *testing.T) {
	m := liveManager()
	m.SetWiFiPending(true)
	if s := m.Snapshot().Session; s == nil || !s.WiFiPending || s.WiFi != nil {
		t.Fatalf("pending: %+v", s)
	}
	m.SetWiFi(NewWiFi("Yui KKTC", "pw"))
	if s := m.Snapshot().Session; s.WiFiPending || s.WiFi == nil {
		t.Fatalf("set: %+v", s)
	}
	m.SetWiFi(nil)
	m.SetWiFiPending(true)
	m.SetWiFiPending(false)
	if s := m.Snapshot().Session; s.WiFiPending || s.WiFi != nil {
		t.Fatalf("failed start: %+v", s)
	}
}

// Watchers see snapshots in the order they were taken, even when emits
// race, so the overlay never ends on an older view than the newest.
func TestEmitDeliversInOrder(t *testing.T) {
	m := liveManager()
	var mu sync.Mutex
	var seen []uint64
	m.Watch(func(s Snapshot) {
		mu.Lock()
		seen = append(seen, s.Seq)
		mu.Unlock()
	})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.emit() }()
	}
	wg.Wait()
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("delivery out of order at %d: %v", i, seen)
		}
	}
	if next := EmptySnapshot().Seq; next <= seen[len(seen)-1] {
		t.Fatalf("empty snapshot seq %d not after %d", next, seen[len(seen)-1])
	}
}
