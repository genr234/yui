package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/session"
)

// Player accounts and game profiles (plan 10). ctx.profiles reaches these
// commands through the overlay, which checks the app's `profiles` permission
// and attaches app_id; like pad.app.haptic, the app must hold the input hub's
// app layer. The phone's profile_reply only wakes pad.profile.request: the
// grant itself is confirmed with Rails (P3).

const (
	profileRequestTimeout = 60 * time.Second
	profileCallTimeout    = 20 * time.Second
	maxAppNameRunes       = 80
)

func ProfileCommands() []Command {
	return []Command{
		padProfileRequestCommand{},
		padProfileGetCommand{},
		padProfileSetCommand{},
		padAccountLinkCodeCommand{},
	}
}

// profileWait is one outstanding profile_request, shared by every caller
// asking for the same player and app.
type profileWait struct {
	done    chan struct{}
	timer   *time.Timer
	granted bool
}

// profileWaits keys outstanding requests by player and app.
type profileWaits struct {
	mu      sync.Mutex
	waits   map[string]*profileWait
	timeout time.Duration
}

func profileKey(playerID, appID string) string { return playerID + "\x00" + appID }

// join returns the wait for key. first is true for the caller that created
// it, which must send the request.
func (w *profileWaits) join(key string) (wait *profileWait, first bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if existing := w.waits[key]; existing != nil {
		return existing, false
	}
	if w.waits == nil {
		w.waits = map[string]*profileWait{}
	}
	timeout := w.timeout
	if timeout <= 0 {
		timeout = profileRequestTimeout
	}
	wait = &profileWait{done: make(chan struct{})}
	wait.timer = time.AfterFunc(timeout, func() { w.resolve(key, wait, false) })
	w.waits[key] = wait
	return wait, true
}

// resolve finishes wait (a no-op once it has finished).
func (w *profileWaits) resolve(key string, wait *profileWait, granted bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.waits[key] != wait {
		return
	}
	delete(w.waits, key)
	wait.timer.Stop()
	wait.granted = granted
	close(wait.done)
}

// reply resolves whatever waits on playerID and the reply's app.
func (w *profileWaits) reply(playerID string, m protocol.ProfileReply) {
	key := profileKey(playerID, m.AppID)
	w.mu.Lock()
	wait := w.waits[key]
	w.mu.Unlock()
	if wait != nil {
		w.resolve(key, wait, m.Granted)
	}
}

// profileReplied is the router's profile_reply hook.
func (r *Registry) profileReplied(playerID string, m protocol.ProfileReply) {
	r.profiles.reply(playerID, m)
}

// profileSend delivers a control message to a phone.
func (r *Registry) profileSend(playerID string, msg any) error {
	if r.profileSendHook != nil {
		return r.profileSendHook(playerID, msg)
	}
	h, err := r.ctrlHub()
	if err != nil {
		return err
	}
	return h.Send(playerID, msg)
}

// profileError turns a Rails profile error into a bridge error whose message
// starts with its code, so the SDK can tell them apart.
func profileError(err error) error {
	var conflict *session.VersionConflictError
	switch {
	case errors.Is(err, session.ErrNotLinked):
		return errors.New("not_linked: this player is not playing as a Yui account")
	case errors.Is(err, session.ErrNotGranted):
		return errors.New("not_granted: this player has not allowed this game to use their profile")
	case errors.Is(err, session.ErrTooLarge):
		return errors.New("too_large: a profile can hold at most 64 KB")
	case errors.As(err, &conflict):
		return fmt.Errorf("version_conflict: current version %d", conflict.Version)
	}
	return err
}

type profileParams struct {
	AppID    string          `json:"app_id"`
	AppName  string          `json:"app_name"`
	PlayerID string          `json:"player_id"`
	Data     json.RawMessage `json:"data"`
	Version  int             `json:"version"`
}

// profileTarget validates the params shared by the profile commands: the
// calling app must hold the hub and the session must be running.
func (r *Registry) profileTarget(params json.RawMessage) (profileParams, *session.Manager, error) {
	var p profileParams
	if err := decodeParams(params, &p); err != nil {
		return p, nil, err
	}
	if _, err := r.padOwner(p.AppID, p.PlayerID); err != nil {
		return p, nil, err
	}
	mgr, err := r.activePad()
	return p, mgr, err
}

type padProfileRequestCommand struct{}

func (padProfileRequestCommand) Name() string { return "pad.profile.request" }
func (padProfileRequestCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	p, mgr, err := r.profileTarget(params)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.AppName)
	if name == "" {
		name = p.AppID
	}
	if runes := []rune(name); len(runes) > maxAppNameRunes {
		name = string(runes[:maxAppNameRunes])
	}

	ctx, cancel := context.WithTimeout(context.Background(), profileCallTimeout)
	defer cancel()
	// Already allowed (an earlier game, or another kiosk): no need to bother
	// the phone.
	switch _, err := mgr.Profile(ctx, p.PlayerID, p.AppID); {
	case err == nil:
		return map[string]bool{"granted": true}, nil
	case errors.Is(err, session.ErrNotGranted):
	default:
		return nil, profileError(err)
	}

	key := profileKey(p.PlayerID, p.AppID)
	wait, first := r.profiles.join(key)
	if first {
		msg := protocol.ProfileRequest{T: "profile_request", AppID: p.AppID, AppName: name}
		if err := r.profileSend(p.PlayerID, msg); err != nil {
			r.profiles.resolve(key, wait, false)
			return nil, fmt.Errorf("could not ask the player's phone: %w", err)
		}
	}
	<-wait.done
	if !wait.granted {
		return map[string]bool{"granted": false}, nil
	}

	// The reply only says the phone was asked and answered; Rails knows
	// whether the grant exists.
	confirm, cancelConfirm := context.WithTimeout(context.Background(), profileCallTimeout)
	defer cancelConfirm()
	switch _, err := mgr.Profile(confirm, p.PlayerID, p.AppID); {
	case err == nil:
		return map[string]bool{"granted": true}, nil
	case errors.Is(err, session.ErrNotGranted):
		return map[string]bool{"granted": false}, nil
	default:
		return nil, profileError(err)
	}
}

type padProfileGetCommand struct{}

func (padProfileGetCommand) Name() string { return "pad.profile.get" }
func (padProfileGetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	p, mgr, err := r.profileTarget(params)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), profileCallTimeout)
	defer cancel()
	profile, err := mgr.Profile(ctx, p.PlayerID, p.AppID)
	if err != nil {
		return nil, profileError(err)
	}
	if len(profile.Data) == 0 {
		profile.Data = json.RawMessage("null")
	}
	return map[string]any{"profile_id": profile.ProfileID, "data": profile.Data, "version": profile.Version}, nil
}

type padProfileSetCommand struct{}

func (padProfileSetCommand) Name() string { return "pad.profile.set" }
func (padProfileSetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	p, mgr, err := r.profileTarget(params)
	if err != nil {
		return nil, err
	}
	if len(p.Data) == 0 {
		return nil, errors.New("data is required")
	}
	if p.Version < 0 {
		return nil, errors.New("version must be 0 or the version from get")
	}
	ctx, cancel := context.WithTimeout(context.Background(), profileCallTimeout)
	defer cancel()
	version, err := mgr.SetProfile(ctx, p.PlayerID, p.AppID, p.Data, p.Version)
	if err != nil {
		return nil, profileError(err)
	}
	return map[string]int{"version": version}, nil
}

// padAccountLinkCodeCommand makes the code Settings shows. It is not an app
// command and stays behind the admin PIN (it is not a publicAuthMethod).
type padAccountLinkCodeCommand struct{}

func (padAccountLinkCodeCommand) Name() string { return "pad.account.link_code" }
func (padAccountLinkCodeCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	cfg, account, ok := r.refreshedAccount()
	if !ok || cfg.ServerURL == "" || account.DeviceToken == "" {
		return nil, errors.New("connect this kiosk to an account first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), profileCallTimeout)
	defer cancel()
	code, err := session.CreatePhoneLinkCode(ctx, cfg.ServerURL, account.DeviceToken)
	if err != nil {
		return nil, err
	}
	return code, nil
}
