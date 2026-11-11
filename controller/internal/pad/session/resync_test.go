package session

import (
	"errors"
	"testing"
	"time"
)

// dropCable closes the fake server's side of the signaling socket.
func (f *fakeRails) dropCable() {
	f.mu.Lock()
	conn := f.conn
	f.ident = ""
	f.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReconnectResyncsRoster(t *testing.T) {
	f := newFake(t)
	stale := playerFixture("p-stale", 1, true, false)
	f.mu.Lock()
	f.sess.Players = []Player{{Player: stale}}
	f.mu.Unlock()
	mgr := f.manager(t)
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	if got := mgr.Snapshot().Players; len(got) != 1 || got[0].ID != "p-stale" {
		t.Fatalf("initial roster = %+v", got)
	}

	// While signaling is down, p-stale leaves and p-new joins. Neither
	// player_left nor player_joined reaches the kiosk.
	f.dropCable()
	joined := playerFixture("p-new", 2, true, true)
	f.mu.Lock()
	f.sess.Players = []Player{{Player: joined}}
	f.mu.Unlock()

	waitFor(t, "roster resync", func() bool {
		got := mgr.Snapshot().Players
		return len(got) == 1 && got[0].ID == "p-new"
	})
	offer := f.waitSignal(t, signalOffer)
	if offer.To != "p-new" {
		t.Fatalf("offer went to %q", offer.To)
	}
}

func TestReconnectToEndedSessionStops(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	cancel, done := start(t, f, mgr)
	defer cancel()

	f.mu.Lock()
	f.gone = true
	f.mu.Unlock()
	f.dropCable()

	select {
	case err := <-done:
		if !errors.Is(err, errSessionGone) {
			t.Fatalf("run = %v, want errSessionGone", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("run kept going after the server ended the session")
	}
	if mgr.Active() || mgr.Snapshot().Session != nil {
		t.Fatal("manager still reports a session")
	}
}

func TestClosePeerInvalidatesNegotiation(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	mgr.mu.Lock()
	mgr.negGen["p1"] = 3
	mgr.mu.Unlock()
	mgr.closePeer("p1")
	mgr.mu.Lock()
	gen := mgr.negGen["p1"]
	mgr.mu.Unlock()
	if gen != 4 {
		t.Fatalf("generation after close = %d, want 4", gen)
	}
}

func TestPeriodicResyncDropsPlayersRailsReleased(t *testing.T) {
	f := newFake(t)
	ghost := playerFixture("p-ghost", 1, true, true)
	f.mu.Lock()
	f.sess.Players = []Player{{Player: ghost}}
	f.mu.Unlock()
	mgr := f.manager(t)
	mgr.cfg.ResyncInterval = 20 * time.Millisecond
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	if got := mgr.Snapshot().Players; len(got) != 1 || !got[0].Connected {
		t.Fatalf("initial roster = %+v", got)
	}

	// Rails freed the ghost's slot and a new tester took P1; no event reached
	// the kiosk.
	joined := playerFixture("p-new", 1, true, true)
	f.mu.Lock()
	f.sess.Players = []Player{{Player: joined}}
	f.mu.Unlock()
	waitFor(t, "periodic resync", func() bool {
		got := mgr.Snapshot().Players
		return len(got) == 1 && got[0].ID == "p-new" && got[0].Slot != nil && *got[0].Slot == 1
	})
}
