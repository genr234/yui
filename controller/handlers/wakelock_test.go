package handlers

import (
	"encoding/json"
	"testing"
	"time"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/useractivity"
)

type quietPlatform struct{ awake bool }

func (*quietPlatform) Nudge() error             { return nil }
func (p *quietPlatform) SetAwake(on bool) error { p.awake = on; return nil }

func runWake(t *testing.T, cmd Command, params any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := cmd.Handle(nil, raw)
	if err != nil {
		t.Fatalf("%s: %v", cmd.Name(), err)
	}
	b, _ := json.Marshal(out)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWakeLockCommandsRoundTrip(t *testing.T) {
	k := useractivity.Default()
	t.Cleanup(k.Close)

	got := runWake(t, wakeLockAcquireCommand{}, map[string]string{"app_id": "dev.genr.youtube", "reason": "video"})
	id, _ := got["id"].(string)
	if id == "" || got["owner"] != "dev.genr.youtube" || got["ttl_ms"].(float64) != float64(useractivity.DefaultHoldTTL.Milliseconds()) {
		t.Fatalf("acquire = %v", got)
	}
	list := runWake(t, wakeLockListCommand{}, nil)
	if list["active"] != true || len(list["holds"].([]any)) != 1 {
		t.Fatalf("list = %v", list)
	}
	if r := runWake(t, wakeLockRenewCommand{}, map[string]string{"id": id}); r["ok"] != true {
		t.Fatalf("renew = %v", r)
	}
	if r := runWake(t, wakeLockReleaseCommand{}, map[string]string{"id": id}); r["ok"] != true {
		t.Fatalf("release = %v", r)
	}
	if r := runWake(t, wakeLockRenewCommand{}, map[string]string{"id": id}); r["ok"] != false {
		t.Fatalf("renew after release = %v, want ok=false so the app re-acquires", r)
	}
	if list := runWake(t, wakeLockListCommand{}, nil); list["active"] != false {
		t.Fatalf("list after release = %v", list)
	}
}

func TestWakeLockAcquireValidates(t *testing.T) {
	for _, params := range []string{`{}`, `{"app_id":"  "}`, `{"app_id":"native"}`} {
		if _, err := (wakeLockAcquireCommand{}).Handle(nil, json.RawMessage(params)); err == nil {
			t.Errorf("acquire %s: want an error", params)
		}
	}
	if _, err := (wakeLockReleaseCommand{}).Handle(nil, json.RawMessage(`{}`)); err == nil {
		t.Error("release without id: want an error")
	}
}

func TestNativeProgramHoldsTheKioskAwakeUntilItEnds(t *testing.T) {
	p := &quietPlatform{}
	k := &useractivity.Keeper{Interval: time.Minute, HoldTTL: time.Minute, Tick: time.Hour, Now: time.Now, Platform: p}
	t.Cleanup(k.Close)

	for _, state := range []string{native.StatePreparing, native.StateRunning, native.StateRunning, native.StateStopping} {
		nativeWakeLock(k, native.Status{State: state, AppID: "dev.genr.wii-arcade"})
		if holds := k.Holds(); len(holds) != 1 || !p.awake {
			t.Fatalf("%s: holds=%v awake=%v, want exactly one native hold", state, holds, p.awake)
		}
	}
	for _, end := range []string{native.StateExited, native.StateCrashed, native.StateFailed} {
		nativeWakeLock(k, native.Status{State: native.StateRunning})
		nativeWakeLock(k, native.Status{State: end})
		if k.Active() || p.awake {
			t.Fatalf("after %s: still holding", end)
		}
	}
}
