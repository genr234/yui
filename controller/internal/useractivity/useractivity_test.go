package useractivity

import (
	"errors"
	"testing"
	"time"
)

func TestTouchNudgesAtMostOncePerInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	k := &Keeper{Interval: 30 * time.Second, Now: func() time.Time { return now }, Nudge: func() error { calls++; return nil }}

	k.Touch()
	k.Touch()
	now = now.Add(29 * time.Second)
	k.Touch()
	if calls != 1 {
		t.Fatalf("calls = %d within one interval, want 1", calls)
	}
	now = now.Add(time.Second)
	k.Touch()
	if calls != 2 {
		t.Fatalf("calls = %d after the interval, want 2", calls)
	}
}

func TestTouchSurvivesNudgeErrorsAndNil(t *testing.T) {
	now := time.Unix(0, 0)
	calls := 0
	k := &Keeper{Interval: time.Second, Now: func() time.Time { return now }, Nudge: func() error { calls++; return errors.New("no desktop") }}
	k.Touch()
	now = now.Add(2 * time.Second)
	k.Touch()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (errors must not stop later nudges)", calls)
	}
	var nilKeeper *Keeper
	nilKeeper.Touch()
}
