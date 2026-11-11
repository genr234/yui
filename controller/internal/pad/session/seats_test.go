package session

import (
	"encoding/json"
	"testing"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

// seatRig runs a session with a seated giver (p1, slot 1, connected) and a
// connected spectator (p2). Control messages go over the cable fallback, so
// the fake Rails sees what each phone would receive.
type seatRig struct {
	t    *testing.T
	f    *fakeRails
	mgr  *Manager
	stop func()
}

func newSeatRig(t *testing.T, ttl time.Duration) *seatRig {
	t.Helper()
	f := newFake(t)
	mgr := f.manager(t)
	mgr.cfg.SeatOfferTTL = ttl
	cancel, done := start(t, f, mgr)
	r := &seatRig{t: t, f: f, mgr: mgr, stop: func() { stop(t, cancel, done) }}
	r.join(playerFixture("p1", 1, true, true))
	spec := playerFixture("p2", 1, false, true)
	spec.Slot = nil
	spec.Name = "Dee"
	r.join(spec)
	return r
}

func (r *seatRig) join(p protocol.Player) {
	r.t.Helper()
	r.f.push(r.t, map[string]any{"type": "player_joined", "player": p})
	r.f.waitSignal(r.t, signalOffer)
	r.mgr.mu.Lock()
	gen := r.mgr.peers[p.ID].gen
	r.mgr.mu.Unlock()
	r.mgr.noteICEFailed(p.ID, gen)
}

func (r *seatRig) phone(from string, msg any) {
	r.f.push(r.t, map[string]any{"from": from, "type": "control", "data": msg})
}

// next waits for the next control message to the player with the given t.
func (r *seatRig) next(to, typ string) map[string]any {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.f.mu.Lock()
		for i, sig := range r.f.sigs {
			if sig.Type != signalControl || sig.To != to {
				continue
			}
			var m map[string]any
			_ = json.Unmarshal(sig.Data, &m)
			if m["t"] == typ {
				r.f.sigs = append(r.f.sigs[:i], r.f.sigs[i+1:]...)
				r.f.mu.Unlock()
				if _, err := protocol.ParseControl(sig.Data); err != nil {
					r.t.Fatalf("%s to %s does not parse: %v", typ, to, err)
				}
				return m
			}
		}
		r.f.mu.Unlock()
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s to %s", typ, to)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *seatRig) offer() {
	r.t.Helper()
	r.phone("p1", map[string]any{"t": "seat_offer", "to": "p2"})
	if m := r.next("p1", "seat_result"); m["result"] != "pending" || m["role"] != "giver" {
		r.t.Fatalf("giver result %v", m)
	}
	m := r.next("p2", "seat_offered")
	from := m["from"].(map[string]any)
	if from["id"] != "p1" || m["slot"] != float64(1) || m["expires_in"] == nil {
		r.t.Fatalf("offered %v", m)
	}
}

func TestSeatOfferAcceptedCallsRails(t *testing.T) {
	r := newSeatRig(t, 0)
	defer r.stop()
	r.offer()

	r.phone("p2", map[string]any{"t": "seat_answer", "from": "p1", "accept": true})
	for _, who := range []struct{ id, role string }{{"p1", "giver"}, {"p2", "recipient"}} {
		m := r.next(who.id, "seat_result")
		if m["result"] != "accepted" || m["role"] != who.role || m["slot"] != float64(1) {
			t.Fatalf("%s result %v", who.id, m)
		}
	}
	if !r.f.hasCall("POST /api/kiosk/play_sessions/sess-1/players/p1/pass_seat") {
		t.Fatalf("calls %v", r.f.calls)
	}
	r.f.mu.Lock()
	body := r.f.bodies[len(r.f.bodies)-1]
	r.f.mu.Unlock()
	if body != `{"slot":1,"to":"p2"}` {
		t.Fatalf("body %s", body)
	}
	// The offer is spent: answering again just closes the card.
	r.phone("p2", map[string]any{"t": "seat_answer", "from": "p1", "accept": true})
	if m := r.next("p2", "seat_result"); m["result"] != "expired" {
		t.Fatalf("second answer %v", m)
	}
}

func TestSeatOfferExpires(t *testing.T) {
	r := newSeatRig(t, 80*time.Millisecond)
	defer r.stop()
	r.offer()
	for _, id := range []string{"p1", "p2"} {
		if m := r.next(id, "seat_result"); m["result"] != "expired" {
			t.Fatalf("%s result %v", id, m)
		}
	}
	if r.f.hasCall("POST /api/kiosk/play_sessions/sess-1/players/p1/pass_seat") {
		t.Fatal("expired offer must not reach Rails")
	}
	// Accepting after expiry does nothing.
	r.phone("p2", map[string]any{"t": "seat_answer", "from": "p1", "accept": true})
	if m := r.next("p2", "seat_result"); m["result"] != "expired" {
		t.Fatalf("late answer %v", m)
	}
	if r.f.hasCall("POST /api/kiosk/play_sessions/sess-1/players/p1/pass_seat") {
		t.Fatal("late accept must not reach Rails")
	}
}

func TestSeatDeclineCancelAndOnePerSeat(t *testing.T) {
	r := newSeatRig(t, 0)
	defer r.stop()
	r.offer()

	r.phone("p1", map[string]any{"t": "seat_offer", "to": "p2"})
	if m := r.next("p1", "seat_result"); m["result"] != "failed" || m["reason"] != "busy" {
		t.Fatalf("second offer %v", m)
	}

	r.phone("p2", map[string]any{"t": "seat_answer", "from": "p1", "accept": false})
	for _, id := range []string{"p1", "p2"} {
		if m := r.next(id, "seat_result"); m["result"] != "declined" {
			t.Fatalf("%s %v", id, m)
		}
	}

	r.offer()
	r.phone("p1", map[string]any{"t": "seat_cancel"})
	for _, id := range []string{"p1", "p2"} {
		if m := r.next(id, "seat_result"); m["result"] != "cancelled" {
			t.Fatalf("%s %v", id, m)
		}
	}

	// Offers to a seated player or a stranger are refused.
	r.phone("p2", map[string]any{"t": "seat_offer", "to": "p1"})
	if m := r.next("p2", "seat_result"); m["result"] != "failed" || m["reason"] != "not_seated" {
		t.Fatalf("spectator offer %v", m)
	}
	r.phone("p1", map[string]any{"t": "seat_offer", "to": "nobody"})
	if m := r.next("p1", "seat_result"); m["result"] != "failed" || m["reason"] != "recipient_unavailable" {
		t.Fatalf("stranger offer %v", m)
	}
}

func TestSeatOfferCancelledWhenAPartyChanges(t *testing.T) {
	r := newSeatRig(t, 0)
	defer r.stop()
	r.offer()
	// The giver loses the seat (stale release) while the offer waits.
	lost := playerFixture("p1", 1, true, false)
	lost.Slot = nil
	r.f.push(t, map[string]any{"type": "player_updated", "player": lost})
	for _, id := range []string{"p1", "p2"} {
		if m := r.next(id, "seat_result"); m["result"] != "cancelled" {
			t.Fatalf("%s %v", id, m)
		}
	}

	// Recipient leaves.
	r.f.push(t, map[string]any{"type": "player_updated", "player": playerFixture("p1", 1, true, true)})
	waitFor(t, "p1 seated again", func() bool { return r.mgr.Snapshot().Players[0].Slot != nil })
	r.offer()
	gone := playerFixture("p2", 1, false, false)
	gone.Slot = nil
	r.f.push(t, map[string]any{"type": "player_left", "player": gone})
	if m := r.next("p1", "seat_result"); m["result"] != "cancelled" {
		t.Fatalf("giver %v", m)
	}
}

func TestSeatAcceptRefusedByRails(t *testing.T) {
	r := newSeatRig(t, 0)
	defer r.stop()
	r.f.mu.Lock()
	r.f.passStatus, r.f.passBody = 409, `{"error":"giver_not_seated"}`
	r.f.mu.Unlock()
	r.offer()
	r.phone("p2", map[string]any{"t": "seat_answer", "from": "p1", "accept": true})
	for _, id := range []string{"p1", "p2"} {
		if m := r.next(id, "seat_result"); m["result"] != "failed" || m["reason"] != "giver_not_seated" {
			t.Fatalf("%s %v", id, m)
		}
	}
}
