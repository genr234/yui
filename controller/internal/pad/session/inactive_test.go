package session

import (
	"testing"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

// inactiveManager is a bare manager with one connected phone, driven by
// inactiveStep directly (no Rails, no loops).
func inactiveManager(clock *testClock) *Manager {
	m := &Manager{
		cfg: Config{
			Now:             clock.now,
			InactiveTimeout: 10 * time.Minute,
			InactiveWarning: time.Minute,
		},
		players: map[string]*playerState{"1": {Player: protocol.Player{ID: "1", Connected: true}}},
	}
	m.lastAction = clock.now()
	return m
}

func TestInactiveWarnsThenEnds(t *testing.T) {
	clock := &testClock{t: time.Unix(1000, 0)}
	m := inactiveManager(clock)

	clock.add(8 * time.Minute)
	if end, warn := m.inactiveStep(); end || warn != nil {
		t.Fatalf("8 min: end=%v warn=%v", end, warn)
	}
	clock.add(90 * time.Second)
	end, warn := m.inactiveStep()
	if end || warn == nil || warn.EndsIn != 30 {
		t.Fatalf("9.5 min: end=%v warn=%+v", end, warn)
	}
	if _, again := m.inactiveStep(); again != nil {
		t.Fatal("warned twice")
	}

	// A button press clears the warning.
	m.noteAction()
	if end, warn := m.inactiveStep(); end || warn == nil || warn.EndsIn != 0 {
		t.Fatalf("after action: end=%v warn=%+v", end, warn)
	}

	clock.add(10 * time.Minute)
	m.inactiveStep() // warning
	if end, _ := m.inactiveStep(); !end {
		t.Fatal("did not end after 10 idle minutes")
	}
	m.markAbandoned()
	if !m.Abandoned() {
		t.Fatal("not marked abandoned")
	}
}

// A game left running holds a wake lock all the time; that must not keep an
// abandoned session open (it used to, for hours). Only an empty session is
// left to the empty timeout.
func TestInactiveEmptyLeftToEmptyTimeout(t *testing.T) {
	clock := &testClock{t: time.Unix(1000, 0)}
	m := inactiveManager(clock)
	m.players["1"].Connected = false
	clock.add(time.Hour)
	if end, _ := m.inactiveStep(); end {
		t.Fatal("an empty session is the empty timeout's job")
	}
}

func TestActedIgnoresMotion(t *testing.T) {
	a := protocol.InputState{Seq: 1, Gyro: [3]float32{1, 2, 3}}
	b := a
	b.Seq, b.Gyro, b.Accel = 2, [3]float32{4, 5, 6}, [3]float32{1, 1, 1}
	if protocol.Acted(a, b) {
		t.Fatal("motion counted as an action")
	}
	b.Buttons = 1
	if !protocol.Acted(a, b) {
		t.Fatal("a button press did not count")
	}
}
