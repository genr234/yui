package router

import (
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/session"
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

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func socialHarness(t *testing.T) (*harness, *fakeClock) {
	h := newHarness(t)
	clk := &fakeClock{t: time.Unix(1000, 0)}
	h.router.soc.now = clk.now
	for i, id := range []string{"1", "2", "3"} {
		h.sess.firePlayer(session.PlayerReady, player(id, i+1, i == 0))
	}
	h.sess.take()
	return h, clk
}

func pokedTo(msgs []sent, id string) []protocol.Poked {
	var out []protocol.Poked
	for _, m := range msgs {
		if p, ok := m.msg.(protocol.Poked); ok && m.to == id {
			out = append(out, p)
		}
	}
	return out
}

func TestReactionLimitAndSensitive(t *testing.T) {
	h, clk := socialHarness(t)
	react := protocol.React{T: "react", Emote: "heart"}
	h.sess.fireControl("1", react)
	h.sess.fireControl("1", react) // too soon
	h.sess.fireControl("2", protocol.React{T: "react", Emote: "star"})
	h.sess.fireControl("1", protocol.React{T: "react", Emote: "nope"})
	if got := h.eventsNamed(ReactionEvent); len(got) != 2 {
		t.Fatalf("reactions %v, want 2", got)
	}
	clk.add(701 * time.Millisecond)
	h.sess.fireControl("1", react)
	if got := h.eventsNamed(ReactionEvent); len(got) != 3 {
		t.Fatalf("reactions %v, want 3", got)
	}
	h.router.SetSensitive(true)
	clk.add(time.Second)
	h.sess.fireControl("1", react)
	if got := h.eventsNamed(ReactionEvent); len(got) != 3 {
		t.Fatalf("reaction while sensitive was emitted: %v", got)
	}
	if r := h.eventsNamed(ReactionEvent)[0].(Reaction); r.PlayerID != "1" || r.Emote != "heart" {
		t.Fatalf("payload %+v", r)
	}
}

func TestPokeLimitsAndOptOut(t *testing.T) {
	h, clk := socialHarness(t)
	poke := func(from, to string) { h.sess.fireControl(from, protocol.Poke{T: "poke", To: to}) }
	poke("1", "2")
	if p := pokedTo(h.sess.take(), "2"); len(p) != 1 || p[0].From != "1" {
		t.Fatalf("poked %v", p)
	}
	poke("1", "3") // sender limit
	poke("3", "2") // target limit
	poke("1", "1") // self
	poke("1", "99")
	if got := h.eventsNamed(PokeEvent); len(got) != 1 {
		t.Fatalf("pokes %v, want 1", got)
	}
	clk.add(3100 * time.Millisecond)
	poke("1", "3")
	if got := h.eventsNamed(PokeEvent); len(got) != 2 {
		t.Fatalf("pokes %v, want 2", got)
	}
	// Opt out.
	h.sess.fireControl("2", protocol.System{T: "system", Action: "pokes", Value: false})
	h.sess.take()
	clk.add(4 * time.Second)
	poke("1", "2")
	if got := h.eventsNamed(PokeEvent); len(got) != 2 {
		t.Fatalf("poke to opted-out player was emitted: %v", got)
	}
	h.sess.fireControl("2", protocol.System{T: "system", Action: "pokes", Value: true})
	poke("1", "2")
	if got := h.eventsNamed(PokeEvent); len(got) != 3 {
		t.Fatalf("pokes %v, want 3", got)
	}
}

func TestRosterExcludesSelfAndFlagsOptedOut(t *testing.T) {
	h, _ := socialHarness(t)
	h.sess.fireControl("3", protocol.System{T: "system", Action: "pokes", Value: false})
	var last protocol.Players
	for _, m := range h.sess.take() {
		if p, ok := m.msg.(protocol.Players); ok && m.to == "1" {
			last = p
		}
	}
	if len(last.Players) != 2 || last.Players[0].ID != "2" || !last.Players[0].Pokes || last.Players[1].ID != "3" || last.Players[1].Pokes {
		t.Fatalf("roster for 1: %+v", last)
	}
	h.sess.firePlayer(session.PlayerLeft, player("2", 2, false))
	for _, m := range h.sess.take() {
		if p, ok := m.msg.(protocol.Players); ok && m.to == "1" {
			last = p
		}
	}
	if len(last.Players) != 1 || last.Players[0].ID != "3" {
		t.Fatalf("roster after leave: %+v", last)
	}
}

func TestInputButtonsThrottledAndChangedOnly(t *testing.T) {
	h, _ := socialHarness(t)
	press := func(id string, b uint32) { h.sess.fireInput(id, protocol.InputState{Buttons: b}) }
	press("1", 0)
	time.Sleep(150 * time.Millisecond)
	if got := h.eventsNamed(InputEvent); len(got) != 0 {
		t.Fatalf("idle input emitted %v", got)
	}
	press("1", protocol.ButtonStart)
	press("2", 1)
	press("1", 1) // coalesced with the first
	time.Sleep(250 * time.Millisecond)
	got := h.eventsNamed(InputEvent)
	if len(got) < 1 {
		t.Fatal("no pad.input")
	}
	n := got[0].(InputNote)
	if len(n.Players) != 2 || n.Players[0] != (PlayerButtons{"1", 1}) || n.Players[1] != (PlayerButtons{"2", 1}) {
		t.Fatalf("note %+v", n)
	}
	press("1", 1) // unchanged
	press("2", 1)
	time.Sleep(200 * time.Millisecond)
	if len(h.eventsNamed(InputEvent)) != len(got) {
		t.Fatal("unchanged buttons were emitted again")
	}
	press("1", 0)
	time.Sleep(200 * time.Millisecond)
	all := h.eventsNamed(InputEvent)
	if len(all) != len(got)+1 {
		t.Fatalf("release not emitted: %v", all)
	}
	if n := all[len(all)-1].(InputNote); len(n.Players) != 1 || n.Players[0] != (PlayerButtons{"1", 0}) {
		t.Fatalf("release note %+v", n)
	}
}
