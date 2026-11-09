package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"kiosk/controller/internal/remotecontrol"
	"kiosk/controller/internal/supervisor"
	"kiosk/controller/internal/telemetry"
	"kiosk/controller/internal/version"
)

// KioskRemoteCommands are run from the control room through the account's
// command channel (see remoteCommandAllowed): health, restarting Chrome and
// remote control of the kiosk page.
func KioskRemoteCommands() []Command {
	return []Command{KioskStatusCommand{}, KioskRestartChromeCommand{}, KioskScreenshotCommand{}, KioskInputCommand{}}
}

type KioskStatusCommand struct{}

func (KioskStatusCommand) Name() string { return "kiosk.status" }
func (KioskStatusCommand) Handle(_ *Registry, _ json.RawMessage) (any, error) {
	return telemetry.Snapshot(version.Version, version.Commit), nil
}

type KioskRestartChromeCommand struct{}

func (KioskRestartChromeCommand) Name() string { return "kiosk.restartChrome" }
func (KioskRestartChromeCommand) Handle(_ *Registry, _ json.RawMessage) (any, error) {
	supervisor.RequestChromeRestart("requested from the control room")
	return map[string]bool{"ok": true}, nil
}

type KioskScreenshotCommand struct{}

func (KioskScreenshotCommand) Name() string { return "kiosk.screenshot" }
func (KioskScreenshotCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		MaxSide int `json:"max_side"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	if p.MaxSide <= 0 || p.MaxSide > 1600 {
		p.MaxSide = 960
	}
	return remotecontrol.Capture(context.Background(), r.snapshotConfig().PlatformDebugPort, p.MaxSide)
}

type KioskInputCommand struct{}

func (KioskInputCommand) Name() string { return "kiosk.input" }
func (KioskInputCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var in remotecontrol.Input
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, fmt.Errorf("kiosk.input: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := remotecontrol.Send(ctx, r.snapshotConfig().PlatformDebugPort, in); err != nil {
		return nil, err
	}
	telemetry.Record("remote_input", map[string]any{"action": in.Action})
	return map[string]bool{"ok": true}, nil
}
