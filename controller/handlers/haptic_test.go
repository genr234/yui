package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestPadAppHapticValidationAndRateLimit(t *testing.T) {
	r := newControllersRegistry(t)
	if _, err := dispatch(t, r, "pad.app.focus", `{"app_id":"a.b","name":"A","active":true}`); err != nil {
		t.Fatal(err)
	}
	reject := func(params string) {
		t.Helper()
		_, err := dispatch(t, r, "pad.app.haptic", params)
		if err == nil || strings.Contains(err.Error(), "no play session") {
			t.Fatalf("%s: want a validation error, got %v", params, err)
		}
	}
	reject(`{"app_id":"a.b","player_id":"p1"}`)
	reject(`{"app_id":"a.b","player_id":"p1","name":"zap"}`)
	reject(`{"app_id":"a.b","player_id":"p1","name":"tap","pattern":[10]}`)
	reject(`{"app_id":"a.b","player_id":"p1","pattern":[1001]}`)
	reject(`{"app_id":"a.b","player_id":"p1","pattern":[1000,1000,1000,1000,1]}`)
	reject(`{"app_id":"a.b","player_id":"p1","name":"tap","intensity":1.5}`)
	// Valid named and custom forms get past validation to the (absent) session.
	// (Different players: a second haptic to the same phone this soon is dropped.)
	for _, ok := range []string{
		`{"app_id":"a.b","player_id":"p1","name":"win","intensity":0.5}`,
		`{"app_id":"a.b","player_id":"p2","pattern":[40,60,40]}`,
	} {
		if _, err := dispatch(t, r, "pad.app.haptic", ok); err == nil || !strings.Contains(err.Error(), "no play session") {
			t.Fatalf("%s: %v", ok, err)
		}
	}
}

func TestHapticLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := hapticLimiter{now: func() time.Time { return now }}
	if !l.allow("p1") || l.allow("p1") {
		t.Fatal("second haptic at the same instant should drop")
	}
	if !l.allow("p2") {
		t.Fatal("limit is per player")
	}
	now = now.Add(49 * time.Millisecond)
	if l.allow("p1") {
		t.Fatal("49 ms is too soon")
	}
	now = now.Add(time.Millisecond)
	if !l.allow("p1") {
		t.Fatal("50 ms apart is allowed")
	}
}
