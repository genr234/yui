package handlers

import (
	"encoding/json"
	"errors"
	"strings"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/useractivity"
)

// Wake locks let an app keep the kiosk awake while it shows something that
// gets no input (a video, a cutscene, a game in attract mode): no
// screensaver, no display or system sleep, and no idle tool (kiosk 1's
// DetectMouseActivity closes Chrome after five minutes without mouse input)
// acting on the machine. See internal/useractivity.
//
// Holds are leases: the platform renews each one every ttl_ms/3 while the
// app that asked is open, and a hold nobody renews ends by itself after
// ttl_ms. So a closed app, a reloaded page or a dead Chrome always gives
// idle control back to the OS. While a native program runs the controller
// holds one itself (owner "native"), released when the program ends.
//
// The platform hears every change as the "wakelock.changed" event (the
// active holds), which also pauses the attract screen.

const nativeWakeOwner = "native"

func WakeLockCommands() []Command {
	return []Command{wakeLockAcquireCommand{}, wakeLockRenewCommand{}, wakeLockReleaseCommand{}, wakeLockListCommand{}}
}

// initWakeLocks forwards hold changes to the platform. Called once by
// NewRegistry.
func (r *Registry) initWakeLocks() {
	useractivity.Default().SetOnChange(func(holds []useractivity.Hold) {
		r.Emit("wakelock.changed", wakeLockList(holds))
	})
}

type wakeLockView struct {
	Holds  []useractivity.Hold `json:"holds"`
	Active bool                `json:"active"`
}

func wakeLockList(holds []useractivity.Hold) wakeLockView {
	if holds == nil {
		holds = []useractivity.Hold{}
	}
	return wakeLockView{Holds: holds, Active: len(holds) > 0}
}

type wakeLockGrant struct {
	useractivity.Hold
	TTLMs int64 `json:"ttl_ms"`
}

func grant(h useractivity.Hold) wakeLockGrant {
	return wakeLockGrant{Hold: h, TTLMs: useractivity.Default().HoldTTL.Milliseconds()}
}

type wakeLockAcquireCommand struct{}

func (wakeLockAcquireCommand) Name() string { return "wakelock.acquire" }

// wakelock.acquire {app_id, reason?} -> {id, owner, reason, since, expires, ttl_ms}
func (wakeLockAcquireCommand) Handle(_ *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID  string `json:"app_id"`
		Reason string `json:"reason"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	p.AppID = strings.TrimSpace(p.AppID)
	if p.AppID == "" {
		return nil, errors.New("wakelock.acquire: app_id is required")
	}
	if p.AppID == nativeWakeOwner {
		return nil, errors.New("wakelock.acquire: app_id is reserved")
	}
	if len(p.Reason) > 200 {
		p.Reason = p.Reason[:200]
	}
	return grant(useractivity.Default().Acquire(p.AppID, p.Reason)), nil
}

type wakeLockRenewCommand struct{}

func (wakeLockRenewCommand) Name() string { return "wakelock.renew" }

// wakelock.renew {id} -> {ok, hold?}. ok=false means the hold is gone (for
// example the controller restarted): acquire a new one.
func (wakeLockRenewCommand) Handle(_ *Registry, params json.RawMessage) (any, error) {
	id, err := wakeLockID(params)
	if err != nil {
		return nil, err
	}
	h, ok := useractivity.Default().Renew(id)
	if !ok {
		return map[string]any{"ok": false}, nil
	}
	return map[string]any{"ok": true, "hold": grant(h)}, nil
}

type wakeLockReleaseCommand struct{}

func (wakeLockReleaseCommand) Name() string { return "wakelock.release" }

// wakelock.release {id} -> {ok}; ok=false when it had already ended.
func (wakeLockReleaseCommand) Handle(_ *Registry, params json.RawMessage) (any, error) {
	id, err := wakeLockID(params)
	if err != nil {
		return nil, err
	}
	return map[string]bool{"ok": useractivity.Default().Release(id)}, nil
}

type wakeLockListCommand struct{}

func (wakeLockListCommand) Name() string { return "wakelock.list" }

func (wakeLockListCommand) Handle(_ *Registry, _ json.RawMessage) (any, error) {
	return wakeLockList(useractivity.Default().Holds()), nil
}

func wakeLockID(params json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return "", err
		}
	}
	if p.ID == "" {
		return "", errors.New("wake lock id is required")
	}
	return p.ID, nil
}

// nativeWakeLock holds the kiosk awake while a native program is preparing,
// running or stopping, and lets go when it has ended for any reason.
func nativeWakeLock(k *useractivity.Keeper, st native.Status) {
	if st.Active() {
		if !k.HasOwner(nativeWakeOwner) {
			k.AcquirePinned(nativeWakeOwner, "native program "+st.AppID)
		}
		return
	}
	k.ReleaseOwner(nativeWakeOwner)
}
