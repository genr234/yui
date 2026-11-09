package session

import (
	"sync"
	"testing"
	"time"
)

func TestIdleExpired(t *testing.T) {
	t0 := time.Unix(1000, 0)
	cases := []struct {
		name      string
		now       time.Time
		timeout   time.Duration
		connected int
		want      bool
	}{
		{"off", t0.Add(time.Hour), 0, 0, false},
		{"recent", t0.Add(4 * time.Minute), 5 * time.Minute, 0, false},
		{"at limit", t0.Add(5 * time.Minute), 5 * time.Minute, 0, true},
		{"someone connected", t0.Add(time.Hour), 5 * time.Minute, 1, false},
	}
	for _, c := range cases {
		if got := IdleExpired(c.now, t0, c.timeout, c.connected); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func eventManager(t *testing.T, f *fakeRails, clock *testClock, open bool, idle time.Duration) *Manager {
	t.Helper()
	mgr, err := Open(Config{
		ServerURL:    f.srv.URL,
		DeviceToken:  f.token,
		KioskName:    "Lobby",
		PingInterval: time.Hour,
		Now:          clock.now,
		OpenLobby:    open,
		IdleTimeout:  idle,
		IdleTick:     10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestOpenLobbyOnStart(t *testing.T) {
	f := newFake(t)
	clock := &testClock{t: time.Unix(1000, 0)}
	mgr := eventManager(t, f, clock, true, 0)
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)
	f.mu.Lock()
	open := f.sess.LobbyOpen
	f.mu.Unlock()
	if !open || !mgr.Snapshot().Session.LobbyOpen {
		t.Fatal("lobby was not opened automatically")
	}

	g := newFake(t)
	m2 := eventManager(t, g, clock, false, 0)
	c2, d2 := start(t, g, m2)
	defer stop(t, c2, d2)
	if m2.Snapshot().Session.LobbyOpen {
		t.Fatal("lobby opened without event mode")
	}
}

func TestIdleSessionEndsItself(t *testing.T) {
	f := newFake(t)
	clock := &testClock{t: time.Unix(1000, 0)}
	mgr := eventManager(t, f, clock, true, 5*time.Minute)
	cancel, done := start(t, f, mgr)
	defer cancel()

	// Someone connected: stays up however long the clock runs.
	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 1, true, true)})
	f.waitSignal(t, signalOffer)
	clock.add(time.Hour)
	time.Sleep(60 * time.Millisecond)
	if !mgr.Active() {
		t.Fatal("ended while a player was connected")
	}

	// They drop; the idle clock restarts from that moment.
	f.push(t, map[string]any{"type": "player_updated", "player": playerFixture("p1", 1, true, false)})
	deadline := time.Now().Add(2 * time.Second)
	for len(connectedOf(mgr)) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("player update not applied")
		}
		time.Sleep(5 * time.Millisecond)
	}
	clock.add(4 * time.Minute)
	time.Sleep(60 * time.Millisecond)
	if !mgr.Active() {
		t.Fatal("ended before the idle timeout")
	}
	clock.add(2 * time.Minute)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("idle session did not end")
	}
	if !f.hasCall("DELETE /api/kiosk/play_sessions/sess-1") {
		t.Fatalf("missing delete: %+v", f.calls)
	}
	if mgr.Active() {
		t.Fatal("still active")
	}
}

func connectedOf(m *Manager) []string {
	var out []string
	for _, p := range m.Snapshot().Players {
		if p.Connected {
			out = append(out, p.ID)
		}
	}
	return out
}
