package useractivity

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakePlatform struct {
	mu      sync.Mutex
	nudges  int
	awake   bool
	changes []bool
	err     error
}

func (f *fakePlatform) Nudge() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nudges++
	return f.err
}

func (f *fakePlatform) SetAwake(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.awake = on
	f.changes = append(f.changes, on)
	return nil
}

func (f *fakePlatform) state() (int, bool, []bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nudges, f.awake, append([]bool(nil), f.changes...)
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newKeeper(t *testing.T) (*Keeper, *fakePlatform, *clock) {
	t.Helper()
	p := &fakePlatform{}
	c := &clock{now: time.Unix(1_000_000, 0)}
	k := &Keeper{Interval: 30 * time.Second, HoldTTL: 60 * time.Second, Tick: 2 * time.Millisecond, Now: c.Now, Platform: p}
	t.Cleanup(k.Close)
	return k, p, c
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTouchNudgesAtMostOncePerInterval(t *testing.T) {
	k, p, c := newKeeper(t)
	k.Touch()
	k.Touch()
	c.Add(29 * time.Second)
	k.Touch()
	if n, _, _ := p.state(); n != 1 {
		t.Fatalf("nudges = %d within one interval, want 1", n)
	}
	c.Add(time.Second)
	k.Touch()
	if n, awake, _ := p.state(); n != 2 || awake {
		t.Fatalf("nudges = %d awake = %v, want 2 and false (touch never holds the OS awake)", n, awake)
	}
}

func TestTouchSurvivesNudgeErrorsAndNil(t *testing.T) {
	k, p, c := newKeeper(t)
	p.err = errors.New("no desktop")
	k.Touch()
	c.Add(time.Minute)
	k.Touch()
	if n, _, _ := p.state(); n != 2 {
		t.Fatalf("nudges = %d, want 2 (errors must not stop later nudges)", n)
	}
	var nilKeeper *Keeper
	nilKeeper.Touch()
}

func TestHoldKeepsAwakeAndReleaseGivesIdleBack(t *testing.T) {
	k, p, _ := newKeeper(t)
	var seen [][]Hold
	var seenMu sync.Mutex
	k.SetOnChange(func(h []Hold) { seenMu.Lock(); seen = append(seen, h); seenMu.Unlock() })

	h := k.Acquire("dev.genr.youtube", "video playing")
	n, awake, _ := p.state()
	if !awake || n != 1 {
		t.Fatalf("after acquire: awake=%v nudges=%d, want awake and an immediate nudge (closes a running screensaver)", awake, n)
	}
	if !k.Active() || len(k.Holds()) != 1 || k.Holds()[0].Owner != "dev.genr.youtube" {
		t.Fatalf("holds = %+v", k.Holds())
	}

	if !k.Release(h.ID) {
		t.Fatal("release of an active hold reported false")
	}
	if k.Release(h.ID) {
		t.Fatal("second release reported true")
	}
	_, awake, changes := p.state()
	if awake || len(changes) != 2 || changes[0] != true || changes[1] != false {
		t.Fatalf("awake=%v changes=%v, want on then off", awake, changes)
	}
	if k.Active() {
		t.Fatal("still active after the only hold was released")
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	if len(seen) != 2 || len(seen[0]) != 1 || len(seen[1]) != 0 {
		t.Fatalf("OnChange saw %v, want [1 hold] then [none]", seen)
	}
}

func TestOverlappingHoldsStayAwakeUntilTheLastEnds(t *testing.T) {
	k, p, _ := newKeeper(t)
	a := k.Acquire("app.a", "")
	b := k.Acquire("app.b", "")
	k.Release(a.ID)
	if _, awake, _ := p.state(); !awake {
		t.Fatal("went back to idle while app.b still holds")
	}
	k.Release(b.ID)
	if _, awake, changes := p.state(); awake || len(changes) != 2 {
		t.Fatalf("awake=%v changes=%v, want exactly one on and one off", awake, changes)
	}
}

func TestUnrenewedHoldExpiresAndRenewedHoldDoesNot(t *testing.T) {
	k, p, c := newKeeper(t)
	stale := k.Acquire("closed.app", "")
	live := k.Acquire("open.app", "")

	c.Add(40 * time.Second)
	if _, ok := k.Renew(live.ID); !ok {
		t.Fatal("renew of a live hold failed")
	}
	c.Add(25 * time.Second) // stale is now 65 s old, live was renewed 25 s ago
	eventually(t, "stale hold to expire", func() bool { return len(k.Holds()) == 1 })
	if got := k.Holds()[0].ID; got != live.ID {
		t.Fatalf("remaining hold = %s, want the renewed one", got)
	}
	if _, ok := k.Renew(stale.ID); ok {
		t.Fatal("renewing an expired hold must fail so the owner re-acquires")
	}
	if _, awake, _ := p.state(); !awake {
		t.Fatal("went idle while a renewed hold is active")
	}

	c.Add(61 * time.Second)
	eventually(t, "idle handed back after the last lease ran out", func() bool {
		_, awake, _ := p.state()
		return !awake && !k.Active()
	})
}

func TestActiveHoldKeepsNudging(t *testing.T) {
	k, p, c := newKeeper(t)
	h := k.Acquire("app", "")
	for i := 0; i < 3; i++ {
		c.Add(30 * time.Second)
		k.Renew(h.ID)
		want := i + 2
		eventually(t, "a nudge every interval", func() bool { n, _, _ := p.state(); return n >= want })
	}
}

func TestReleaseOwnerAndReacquire(t *testing.T) {
	k, p, _ := newKeeper(t)
	k.Acquire("app", "one")
	k.Acquire("app", "two")
	k.Acquire("other", "")
	if n := k.ReleaseOwner("app"); n != 2 {
		t.Fatalf("ReleaseOwner = %d, want 2", n)
	}
	if len(k.Holds()) != 1 {
		t.Fatalf("holds = %+v, want only other's", k.Holds())
	}
	k.ReleaseOwner("other")
	if _, awake, _ := p.state(); awake {
		t.Fatal("still awake with no holds")
	}
	// A new hold after going idle starts keeping the OS awake again.
	k.Acquire("app", "again")
	if _, awake, changes := p.state(); !awake || len(changes) != 3 {
		t.Fatalf("awake=%v changes=%v, want on/off/on", awake, changes)
	}
}

func TestPinnedHoldNeverExpiresButReleases(t *testing.T) {
	k, p, c := newKeeper(t)
	k.AcquirePinned("native", "game running")
	if !k.HasOwner("native") {
		t.Fatal("HasOwner(native) = false")
	}
	c.Add(time.Hour)
	time.Sleep(20 * time.Millisecond) // several ticks
	if !k.Active() {
		t.Fatal("a pinned hold expired")
	}
	k.ReleaseOwner("native")
	if _, awake, _ := p.state(); awake || k.Active() {
		t.Fatal("still awake after the pinned hold was released")
	}
}

func TestGraceKeepsAwakeAfterLastHold(t *testing.T) {
	k, p, _ := newKeeper(t)
	k.Grace = 50 * time.Millisecond
	h := k.AcquirePinned("app", "")
	k.Release(h.ID)
	if _, awake, _ := p.state(); !awake {
		t.Fatal("gave idle back at once; want it kept for the grace period")
	}
	eventually(t, "idle given back after grace", func() bool { _, awake, _ := p.state(); return !awake })
	if _, _, changes := p.state(); len(changes) != 2 {
		t.Fatalf("SetAwake calls = %v, want [true false]", changes)
	}
}

func TestHoldWithinGraceCarriesOn(t *testing.T) {
	k, p, _ := newKeeper(t)
	k.Grace = 40 * time.Millisecond
	h := k.AcquirePinned("app", "")
	k.Release(h.ID)
	k.AcquirePinned("app", "")
	time.Sleep(100 * time.Millisecond)
	if _, awake, changes := p.state(); !awake || len(changes) != 1 {
		t.Fatalf("awake=%v changes=%v, want still awake with one SetAwake(true)", awake, changes)
	}
}

func TestCloseSkipsGrace(t *testing.T) {
	k, p, _ := newKeeper(t)
	k.Grace = time.Hour
	k.AcquirePinned("app", "")
	k.Close()
	if _, awake, _ := p.state(); awake {
		t.Fatal("Close kept the OS awake; want idle given back at once")
	}
}
