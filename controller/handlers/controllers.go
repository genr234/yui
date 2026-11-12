package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/apps"
	"kiosk/controller/internal/pad/cdp"
	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/pads"
	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/router"
)

// App and native layers of the input hub (contracts §4 "Apps and the
// input hub"): ctx.controllers, manifest keymaps and ctx.virtualPads reach
// these commands through the overlay, which attaches app_id and checks the
// app's permissions (`controllers`, `virtual-pads`).

func ControllersCommands() []Command {
	return []Command{
		padAppFocusCommand{},
		padAppLayoutCommand{},
		padAppHapticCommand{},
		padAppSoundCommand{},
		padVPadsAttachCommand{},
		padVPadsDetachCommand{},
		padVPadsListCommand{},
		padVPadsSystemPadCommand{},
		padVPadsPressCommand{},
	}
}

// padControllers returns the app/native layer controller, building the
// input hub first when no play session has created it yet.
func (r *Registry) padControllers() *apps.Controller {
	r.padMu.Lock()
	h := r.padRouter
	r.padMu.Unlock()
	if h == nil {
		r.padStartMu.Lock()
		h = r.padHub()
		r.padStartMu.Unlock()
	}

	r.ctrlMu.Lock()
	defer r.ctrlMu.Unlock()
	if r.ctrl != nil && r.ctrlRouter == h {
		return r.ctrl
	}
	var cdpAdapter hub.Adapter
	if p := h.Pointer(); p != nil {
		cdpAdapter = p
	}
	var padSink apps.PadSink
	if p, ok := h.Pads().(apps.PadSink); ok && p != nil {
		padSink = p
	}
	ctl, err := apps.New(apps.Config{
		Stack:    h.Stack(),
		Refresh:  func() { h.Refresh(false) },
		Emit:     r.Emit,
		IsHost:   h.IsHost,
		HostName: r.padHostName,
		CDP:      cdpAdapter,
		Pads:     padSink,
		Resolver: r.dsuResolver(),
		HasHost:  r.padHasHost,
		StopNative: func(appID string) error {
			_, err := r.Native().Stop(appID, false)
			return err
		},
	})
	if err != nil {
		// Only a nil stack fails, and the router always has one.
		panic(err)
	}
	r.ctrl, r.ctrlRouter = ctl, h
	return ctl
}

func (r *Registry) padControllersIfStarted() *apps.Controller {
	r.ctrlMu.Lock()
	defer r.ctrlMu.Unlock()
	return r.ctrl
}

// dsuResolver names the DSU pads for {{virtualPad:n}} and {{systemPad}}.
func (r *Registry) dsuResolver() apps.DSUResolver {
	r.padMu.Lock()
	playersAddr, systemAddr := r.padDSUAddrs[0], r.padDSUAddrs[1]
	r.padMu.Unlock()
	if playersAddr == "" {
		playersAddr = pads.DefaultPlayersAddr
	}
	if systemAddr == "" {
		systemAddr = pads.DefaultSystemAddr
	}
	return apps.DSUResolver{Players: playersAddr, System: systemAddr}
}

func (r *Registry) padHost() (protocol.Player, bool) {
	for _, p := range r.padSnapshot().Players {
		if p.Host && p.Connected {
			return p.Player, true
		}
	}
	return protocol.Player{}, false
}

func (r *Registry) padHasHost() bool {
	_, ok := r.padHost()
	return ok
}

func (r *Registry) padHostName() string {
	p, _ := r.padHost()
	return p.Name
}

// nativeHubObserver gives native sessions the hub's native layer.
type nativeHubObserver struct{ r *Registry }

func (o nativeHubObserver) SessionStarted(info native.Info) {
	o.r.padControllers().SessionStarted(info)
}

func (o nativeHubObserver) SessionEnded(info native.Info, st native.Status) {
	if ctl := o.r.padControllersIfStarted(); ctl != nil {
		ctl.SessionEnded(info, st)
	}
}

func decodeParams(params json.RawMessage, v any) error {
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	return json.Unmarshal(params, v)
}

type padAppFocusParams struct {
	AppID  string       `json:"app_id"`
	Name   string       `json:"name"`
	Active bool         `json:"active"`
	Keymap *apps.Keymap `json:"keymap"`
	Frames bool         `json:"frames"`
	Region *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"region"`
}

type padAppFocusCommand struct{}

func (padAppFocusCommand) Name() string { return "pad.app.focus" }
func (padAppFocusCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p padAppFocusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if len([]rune(p.Name)) > 80 {
		return nil, errors.New("name is too long")
	}
	f := apps.Focus{AppID: p.AppID, Name: p.Name, Active: p.Active, Keymap: p.Keymap, Frames: p.Frames}
	if p.Region != nil {
		rect := cdp.Rect{X: p.Region.X, Y: p.Region.Y, W: p.Region.W, H: p.Region.H}
		for _, v := range []float64{rect.X, rect.Y, rect.W, rect.H} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 100_000 {
				return nil, errors.New("invalid region")
			}
		}
		if rect.W > 0 && rect.H > 0 {
			f.Region = &rect
		}
	}
	var ctl *apps.Controller
	if !p.Active {
		// Blurring never needs to build the hub.
		if ctl = r.padControllersIfStarted(); ctl == nil {
			return map[string]any{"active": false, "app_id": ""}, nil
		}
	} else {
		ctl = r.padControllers()
	}
	if err := ctl.SetFocus(f); err != nil {
		return nil, err
	}
	focused := ctl.Focused()
	return map[string]any{"active": focused != "" && focused == p.AppID, "app_id": focused}, nil
}

type padAppLayoutCommand struct{}

func (padAppLayoutCommand) Name() string { return "pad.app.layout" }
func (padAppLayoutCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID   string                    `json:"app_id"`
		Layouts map[string]*protocol.Node `json:"layouts"`
	}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return nil, r.padControllers().SetLayouts(p.AppID, p.Layouts)
}

// padOwner checks that app_id owns the controllers and names a player.
func (r *Registry) padOwner(appID, playerID string) (*apps.Controller, error) {
	ctl := r.padControllersIfStarted()
	if ctl == nil || !ctl.Owns(appID) {
		return nil, apps.ErrNotOwner
	}
	if playerID == "" {
		return nil, errors.New("player_id is required")
	}
	return ctl, nil
}

type padAppHapticCommand struct{}

func (padAppHapticCommand) Name() string { return "pad.app.haptic" }
func (padAppHapticCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID    string `json:"app_id"`
		PlayerID string `json:"player_id"`
		Pattern  []int  `json:"pattern"`
		// Name and Intensity are the named form; exactly one of Name or
		// Pattern. PlayerID may be "all" (every ready phone).
		Name      string   `json:"name"`
		Intensity *float64 `json:"intensity"`
	}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if _, err := r.padOwner(p.AppID, p.PlayerID); err != nil {
		return nil, err
	}
	msg := protocol.Haptic{T: "haptic", Name: p.Name, Intensity: p.Intensity}
	msg.Pattern = p.Pattern
	if p.Name == "" && msg.Pattern == nil {
		msg.Pattern = []int{} // neither given: Validate rejects it
	}
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	h, err := r.ctrlHub()
	if err != nil {
		return nil, err
	}
	targets := []string{p.PlayerID}
	if p.PlayerID == "all" {
		targets = h.ReadyIDs()
	}
	var firstErr error
	for _, id := range targets {
		if !r.haptics.allow(id) {
			continue // too soon after the last one: dropped
		}
		if err := h.HapticMsg(id, msg); err != nil && firstErr == nil && p.PlayerID != "all" {
			firstErr = err
		}
	}
	return nil, firstErr
}

// hapticMinGap is the shortest time between two haptics to one phone; a game
// that fires faster loses the extras rather than queueing a buzz storm.
const hapticMinGap = 50 * time.Millisecond

type hapticLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time // tests
}

// allow reports whether a haptic to the player may go out now, and records it.
func (l *hapticLimiter) allow(playerID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now
	if l.now != nil {
		now = l.now
	}
	t := now()
	if l.last == nil {
		l.last = map[string]time.Time{}
	}
	if prev, ok := l.last[playerID]; ok && t.Sub(prev) < hapticMinGap {
		return false
	}
	if len(l.last) > 64 { // players come and go; forget the old ones
		for id, at := range l.last {
			if t.Sub(at) > time.Second {
				delete(l.last, id)
			}
		}
	}
	l.last[playerID] = t
	return true
}

// Sound clips are https URLs (the pad's CSP only allows the Yui server) or
// small data:audio URIs.
const maxSoundDataURI = 256 << 10

type padAppSoundCommand struct{}

func (padAppSoundCommand) Name() string { return "pad.app.sound" }
func (padAppSoundCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID    string   `json:"app_id"`
		PlayerID string   `json:"player_id"`
		URL      string   `json:"url"`
		Volume   *float64 `json:"volume"`
	}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.PlayerID == "speaker" {
		// Until plan 06 marks a speaker phone, the host is the speaker.
		host, ok := r.padHost()
		if !ok {
			return nil, errors.New("no speaker phone")
		}
		p.PlayerID = host.ID
	}
	if _, err := r.padOwner(p.AppID, p.PlayerID); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(p.URL, "https://") && len(p.URL) <= 2048:
	case strings.HasPrefix(p.URL, "data:audio/") && len(p.URL) <= maxSoundDataURI:
	default:
		return nil, errors.New("url must be https:// or a data:audio URI up to 256 KiB")
	}
	if p.Volume != nil && (*p.Volume < 0 || *p.Volume > 1 || math.IsNaN(*p.Volume)) {
		return nil, errors.New("volume is 0-1")
	}
	h, err := r.ctrlHub()
	if err != nil {
		return nil, err
	}
	return nil, h.Send(p.PlayerID, protocol.Sound{T: "sound", URL: p.URL, Volume: p.Volume})
}

// ctrlHub returns the hub the controller was built on.
func (r *Registry) ctrlHub() (*router.Router, error) {
	r.ctrlMu.Lock()
	defer r.ctrlMu.Unlock()
	if r.ctrlRouter == nil {
		return nil, errors.New("pad: no play session")
	}
	return r.ctrlRouter, nil
}

type vpadParams struct {
	AppID        string `json:"app_id"`
	PlayerID     string `json:"player_id"`
	Backend      string `json:"backend"`
	Slot         *int   `json:"slot"`
	PointerStick bool   `json:"pointer_stick"`
	Touch        string `json:"touch"`
	Button       string `json:"button"`
	Action       string `json:"action"`
}

type padVPadsAttachCommand struct{}

func (padVPadsAttachCommand) Name() string { return "pad.vpads.attach" }
func (padVPadsAttachCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p vpadParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Slot == nil {
		return nil, errors.New("slot is required")
	}
	return r.padControllers().Attach(p.AppID, apps.Attachment{
		PlayerID: p.PlayerID, Backend: p.Backend, Slot: *p.Slot, PointerStick: p.PointerStick, Touch: p.Touch,
	})
}

type padVPadsDetachCommand struct{}

func (padVPadsDetachCommand) Name() string { return "pad.vpads.detach" }
func (padVPadsDetachCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p vpadParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	ctl := r.padControllersIfStarted()
	if ctl == nil {
		return nil, nil
	}
	return nil, ctl.Detach(p.AppID, p.PlayerID)
}

type padVPadsListCommand struct{}

func (padVPadsListCommand) Name() string { return "pad.vpads.list" }
func (padVPadsListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p vpadParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	ctl := r.padControllersIfStarted()
	if ctl == nil {
		return []apps.Attachment{}, nil
	}
	return ctl.Attachments(p.AppID), nil
}

type padVPadsSystemPadCommand struct{}

func (padVPadsSystemPadCommand) Name() string { return "pad.vpads.system_pad" }
func (padVPadsSystemPadCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	addr, err := r.dsuResolver().SystemPad("")
	if err != nil {
		return nil, err
	}
	return map[string]any{"backend": "dsu", "slot": 0, "address": addr, "buttons": hub.ButtonNames()}, nil
}

type padVPadsPressCommand struct{}

func (padVPadsPressCommand) Name() string { return "pad.vpads.press" }
func (padVPadsPressCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p vpadParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	ctl := r.padControllersIfStarted()
	if ctl == nil {
		return nil, apps.ErrNotOwner
	}
	if err := ctl.SystemButton(p.AppID, p.Button, p.Action); err != nil {
		return nil, fmt.Errorf("system pad: %w", err)
	}
	return nil, nil
}
