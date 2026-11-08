package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/session"
)

const playPauseReason = "play_session"

func PadCommands() []Command {
	return []Command{
		padPlayersCommand{},
		padSessionStartCommand{},
		padSessionEndCommand{},
		padLobbyCommand{},
		padKickCommand{},
		padHostCommand{},
		padSensitiveCommand{},
	}
}

// BindPadLifecycle ties play sessions to the controller process. Cancelling
// ctx ends an active session the same way the host's pad.session.end does.
func (r *Registry) BindPadLifecycle(ctx context.Context) {
	r.padMu.Lock()
	r.padRoot = ctx
	r.padMu.Unlock()
}

type padPlayersCommand struct{}

func (padPlayersCommand) Name() string { return "pad.players" }
func (padPlayersCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return r.padSnapshot(), nil
}

type padSessionStartCommand struct{}

func (padSessionStartCommand) Name() string { return "pad.session.start" }
func (padSessionStartCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return r.startPadSession()
}

type padSessionEndCommand struct{}

func (padSessionEndCommand) Name() string { return "pad.session.end" }
func (padSessionEndCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return r.endPadSession()
}

type padLobbyCommand struct{}

func (padLobbyCommand) Name() string { return "pad.lobby" }
func (padLobbyCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var body struct {
		Open bool `json:"open"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return nil, err
	}
	mgr, err := r.activePad()
	if err != nil {
		return nil, err
	}
	if err := mgr.SetLobbyOpen(context.Background(), body.Open); err != nil {
		return nil, err
	}
	return mgr.Snapshot(), nil
}

type padKickCommand struct{}

func (padKickCommand) Name() string { return "pad.kick" }
func (padKickCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	id, err := playerIDParam(params)
	if err != nil {
		return nil, err
	}
	mgr, err := r.activePad()
	if err != nil {
		return nil, err
	}
	if err := mgr.Kick(context.Background(), id); err != nil {
		return nil, err
	}
	return mgr.Snapshot(), nil
}

type padHostCommand struct{}

func (padHostCommand) Name() string { return "pad.host" }
func (padHostCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	id, err := playerIDParam(params)
	if err != nil {
		return nil, err
	}
	mgr, err := r.activePad()
	if err != nil {
		return nil, err
	}
	if err := mgr.TransferHost(context.Background(), id); err != nil {
		return nil, err
	}
	return mgr.Snapshot(), nil
}

type padSensitiveCommand struct{}

func (padSensitiveCommand) Name() string { return "pad.sensitive" }
func (padSensitiveCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var body struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return nil, err
	}
	r.padMu.Lock()
	r.padSensitive = body.Active
	mgr := r.padMgr
	r.padMu.Unlock()
	if mgr != nil {
		mgr.SetSensitive(body.Active)
	}
	return map[string]bool{"active": body.Active}, nil
}

func playerIDParam(params json.RawMessage) (string, error) {
	var body struct {
		PlayerID string `json:"player_id"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return "", err
	}
	if body.PlayerID == "" {
		return "", errors.New("player_id is required")
	}
	return body.PlayerID, nil
}

func (r *Registry) padSnapshot() session.Snapshot {
	r.padMu.Lock()
	mgr := r.padMgr
	r.padMu.Unlock()
	if mgr == nil {
		return session.Snapshot{Players: []protocol.Player{}}
	}
	return mgr.Snapshot()
}

func (r *Registry) activePad() (*session.Manager, error) {
	r.padMu.Lock()
	mgr := r.padMgr
	r.padMu.Unlock()
	if mgr == nil || !mgr.Active() {
		return nil, errors.New("no active play session")
	}
	return mgr, nil
}

func (r *Registry) startPadSession() (session.Snapshot, error) {
	r.padMu.Lock()
	if r.padMgr != nil && r.padMgr.Active() {
		snap := r.padMgr.Snapshot()
		r.padMu.Unlock()
		return snap, nil
	}
	root := r.padRoot
	sensitive := r.padSensitive
	r.padMu.Unlock()

	cfg, account, ok := r.refreshedAccount()
	if !ok || cfg.ServerURL == "" || account.DeviceToken == "" {
		return session.Snapshot{Players: []protocol.Player{}}, errors.New("connect this kiosk to an account before starting a play session")
	}
	mgr, err := session.Open(session.Config{
		ServerURL:   cfg.ServerURL,
		DeviceToken: account.DeviceToken,
		KioskName:   account.Name,
	})
	if err != nil {
		return session.Snapshot{Players: []protocol.Player{}}, err
	}
	mgr.SetSensitive(sensitive)
	mgr.Watch(func(snap session.Snapshot) {
		r.AssetSyncer().SetPaused(playPauseReason, snap.Session != nil)
		r.Emit("pad.players", snap)
	})

	if root == nil {
		root = context.Background()
	}
	ctx, cancel := context.WithCancel(root)
	done := make(chan struct{})
	errCh := make(chan error, 1)

	r.padMu.Lock()
	if r.padMgr != nil && r.padMgr.Active() {
		existing := r.padMgr.Snapshot()
		r.padMu.Unlock()
		cancel()
		return existing, nil
	}
	r.padMgr = mgr
	r.padCancel = cancel
	r.padDone = done
	r.padMu.Unlock()

	go func() {
		defer close(done)
		err := mgr.Run(ctx)
		errCh <- err
		if err != nil && ctx.Err() == nil {
			log.Printf("pad session: %v", err)
		}
		r.AssetSyncer().SetPaused(playPauseReason, false)
		r.Emit("pad.players", mgr.Snapshot())
	}()

	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		snap := mgr.Snapshot()
		if snap.Session != nil && snap.Session.JoinURL != "" {
			return snap, nil
		}
		select {
		case err := <-errCh:
			if err == nil {
				err = errors.New("play session stopped before it was ready")
			}
			return session.Snapshot{Players: []protocol.Player{}}, err
		case <-timer.C:
			cancel()
			return session.Snapshot{Players: []protocol.Player{}}, errors.New("play session did not become ready")
		case <-ticker.C:
		}
	}
}

func (r *Registry) endPadSession() (session.Snapshot, error) {
	r.padMu.Lock()
	cancel := r.padCancel
	done := r.padDone
	r.padMu.Unlock()
	if cancel == nil {
		return session.Snapshot{Players: []protocol.Player{}}, nil
	}
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			return session.Snapshot{Players: []protocol.Player{}}, errors.New("play session did not end")
		}
	}
	r.AssetSyncer().SetPaused(playPauseReason, false)
	return r.padSnapshot(), nil
}
