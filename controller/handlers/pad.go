package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"kiosk/controller/internal/firewall"
	"kiosk/controller/internal/hotspot"
	"kiosk/controller/internal/native"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/pad/cdp"
	"kiosk/controller/internal/pad/pads"
	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/router"
	"kiosk/controller/internal/pad/session"
	"kiosk/controller/internal/useractivity"
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
		padSpeakerCommand{},
		padVolumeCommand{},
		audioRouteCommand{},
		audioStatusCommand{},
	}
}

func emptyPadSnapshot() session.Snapshot {
	return session.EmptySnapshot()
}

// BindPadLifecycle ties play sessions to the controller process. Cancelling
// ctx ends an active session the same way the host's pad.session.end does.
func (r *Registry) BindPadLifecycle(ctx context.Context) {
	r.padMu.Lock()
	r.padRoot = ctx
	r.padMu.Unlock()
	// A hotspot left on by a controller that crashed mid-session.
	if hotspot.Supported() {
		go stopHotspot(r.snapshotConfig().ConfigDir)
	}
	go func() {
		state, err := firewall.Ensure(ctx)
		if err != nil {
			log.Printf("pad firewall: %v", err)
		} else if state != "present" {
			log.Printf("pad firewall: rule %s", state)
		}
	}()
}

// runHotspot opens the kiosk's own Wi-Fi for a session's phones and, when
// the session ends (done), closes it and puts the kiosk's own hotspot
// settings back. A failure only costs the shortcut: phones still join over
// whatever network they are on.
func runHotspot(mgr *session.Manager, code, configDir string, done <-chan struct{}) {
	ssid, password := hotspot.SSID(code), hotspot.Password()
	mgr.SetWiFiPending(true)
	hotspotMu.Lock()
	hotspotGen++
	gen := hotspotGen
	band, orig, err := hotspot.Start(context.Background(), ssid, password)
	hotspotMu.Unlock()
	if serr := hotspot.SaveOriginal(hotspotOriginalPath(configDir), orig); serr != nil {
		log.Printf("pad hotspot: save original settings: %v", serr)
	}
	if err != nil {
		log.Printf("pad hotspot: %v", err)
		mgr.SetWiFiPending(false)
	} else {
		log.Printf("pad hotspot: %s %s", ssid, band)
		mgr.SetWiFi(session.NewWiFi(ssid, password))
	}
	<-done
	hotspotMu.Lock()
	defer hotspotMu.Unlock()
	// A newer session's hotspot is not this one's to stop.
	if gen == hotspotGen {
		stopHotspotLocked(configDir)
	}
}

// hotspotMu orders starts and stops across sessions; hotspotGen is the
// newest session's start.
var (
	hotspotMu  sync.Mutex
	hotspotGen int
)

// stopHotspot turns off a hotspot this controller started (also one left on
// by a crash) and restores the saved settings.
func stopHotspot(configDir string) {
	hotspotMu.Lock()
	defer hotspotMu.Unlock()
	stopHotspotLocked(configDir)
}

func stopHotspotLocked(configDir string) {
	path := hotspotOriginalPath(configDir)
	orig, err := hotspot.LoadOriginal(path)
	if err != nil {
		log.Printf("pad hotspot: %v", err)
	}
	state, err := hotspot.Stop(context.Background(), orig)
	if err != nil {
		log.Printf("pad hotspot: %v", err)
		return
	}
	if state != "off" && !strings.HasPrefix(state, "left alone") {
		log.Printf("pad hotspot: %s", state)
	}
	if orig != nil && strings.Contains(state, "restored") {
		_ = os.Remove(path)
	}
}

func hotspotOriginalPath(configDir string) string {
	return filepath.Join(configDir, "hotspot-original.json")
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
	// Applied under padMu so it orders against padHub publishing the hub.
	// The router takes only its own locks.
	if r.padRouter != nil {
		r.padRouter.SetSensitive(body.Active)
	}
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
		return emptyPadSnapshot()
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

// WaitPadStopped waits up to timeout for a running play session to finish
// after the lifecycle context is cancelled, so its DELETE reaches Rails
// before the process exits.
func (r *Registry) WaitPadStopped(timeout time.Duration) {
	r.padMu.Lock()
	done := r.padDone
	r.padMu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("pad session: still ending after %s", timeout)
	}
}

func (r *Registry) startPadSession() (session.Snapshot, error) {
	r.padStartMu.Lock()
	defer r.padStartMu.Unlock()

	r.padMu.Lock()
	if r.padMgr != nil && r.padMgr.Active() {
		snap := r.padMgr.Snapshot()
		r.padMu.Unlock()
		return snap, nil
	}
	root := r.padRoot
	sensitive := r.padSensitive
	prevCancel, prevDone := r.padCancel, r.padDone
	r.padMu.Unlock()

	// A previous manager that is still running but not active (reconnecting,
	// or a start that timed out) is stopped and drained first. Its final
	// unpause must not land after the new session pauses the syncer.
	if prevCancel != nil {
		prevCancel()
		if prevDone != nil {
			select {
			case <-prevDone:
			case <-time.After(8 * time.Second):
				return emptyPadSnapshot(), errors.New("previous play session did not stop")
			}
		}
	}

	cfg, account, ok := r.refreshedAccount()
	if !ok || cfg.ServerURL == "" || account.DeviceToken == "" {
		return emptyPadSnapshot(), errors.New("connect this kiosk to an account before starting a play session")
	}
	mgr, err := session.Open(session.Config{
		ServerURL:       cfg.ServerURL,
		DeviceToken:     account.DeviceToken,
		KioskName:       account.Name,
		Audio:           r.Audio(),
		OpenLobby:       cfg.EventMode,
		IdleTimeout:     padEmptyTimeout(cfg),
		InactiveTimeout: padInactiveTimeout(cfg),
	})
	if err != nil {
		return emptyPadSnapshot(), err
	}
	mgr.SetSensitive(sensitive)
	hub := r.padHub()
	hub.Attach(mgr)
	// gate orders watcher snapshots against the final unpause. Once the run
	// goroutine has ended the session, a late snapshot from a goroutine
	// still inside the manager is dropped instead of pausing sync again.
	var gate sync.Mutex
	ended := false
	mgr.Watch(func(snap session.Snapshot) {
		gate.Lock()
		defer gate.Unlock()
		if ended {
			return
		}
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
	r.padMgr = mgr
	r.padCancel = cancel
	r.padDone = done
	r.padMu.Unlock()

	// A play session is the kiosk in use even when the phones send nothing
	// Windows can see (idle tools ignore injected input). The session's own
	// empty and inactive timeouts bound the hold.
	hold := useractivity.Default().AcquirePinned("pad", "play session")

	go func() {
		defer close(done)
		defer useractivity.Default().Release(hold.ID)
		err := mgr.Run(ctx)
		hub.Detach(mgr)
		errCh <- err
		if err != nil && ctx.Err() == nil {
			log.Printf("pad session: %v", err)
		}
		gate.Lock()
		ended = true
		r.AssetSyncer().SetPaused(playPauseReason, false)
		r.Emit("pad.players", mgr.Snapshot())
		gate.Unlock()
		if mgr.Abandoned() {
			r.closeAbandonedPlay()
		}
	}()

	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		snap := mgr.Snapshot()
		if snap.Session != nil && snap.Session.JoinURL != "" {
			if !cfg.PadHotspotOff && hotspot.Supported() {
				go runHotspot(mgr, snap.Session.Code, cfg.ConfigDir, done)
			}
			return snap, nil
		}
		select {
		case err := <-errCh:
			if err == nil {
				err = errors.New("play session stopped before it was ready")
			}
			return emptyPadSnapshot(), err
		case <-timer.C:
			cancel()
			return emptyPadSnapshot(), errors.New("play session did not become ready")
		case <-ticker.C:
		}
	}
}

// closeAbandonedPlay runs when a play session ended because nobody was
// there. A native game left running would otherwise stay on screen for hours
// (it holds a wake lock, so no screensaver or idle tool covers it): it is
// closed the normal way, so it can save, and the screen goes back home.
func (r *Registry) closeAbandonedPlay() {
	if mgr := r.nativeIfStarted(); mgr != nil {
		if st := mgr.Status(); st.State == native.StateRunning || st.State == native.StatePreparing {
			log.Printf("pad session: nobody is playing; closing %s", st.AppID)
			if _, err := mgr.Stop("", false); err != nil {
				log.Printf("pad session: closing %s: %v", st.AppID, err)
			}
		}
	}
	r.Emit("pad.abandoned", map[string]any{})
}

func (r *Registry) endPadSession() (session.Snapshot, error) {
	r.padMu.Lock()
	cancel := r.padCancel
	done := r.padDone
	r.padMu.Unlock()
	if cancel == nil {
		return emptyPadSnapshot(), nil
	}
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			return emptyPadSnapshot(), errors.New("play session did not end")
		}
	}
	// The run goroutine has already unpaused the syncer. Doing it again here
	// could undo the pause of a session started in the meantime.
	return r.padSnapshot(), nil
}

// padHub returns the input hub router, creating it on first use. It lives
// as long as the controller: providers pushed by apps survive play sessions,
// and its DSU servers and CDP worker stop with the pad lifecycle context.
// Callers hold padStartMu, so only one hub is ever built; padMu is not held
// while the config is read (see the lock order on Registry).
func (r *Registry) padHub() *router.Router {
	r.padMu.Lock()
	if r.padRouter != nil {
		hub := r.padRouter
		r.padMu.Unlock()
		return hub
	}
	root := r.padRoot
	playersAddr, systemAddr := r.padDSUAddrs[0], r.padDSUAddrs[1]
	r.padMu.Unlock()
	if root == nil {
		root = context.Background()
	}
	cfg := r.snapshotConfig()
	if playersAddr == "" {
		playersAddr = pads.DefaultPlayersAddr
	}
	if systemAddr == "" {
		systemAddr = pads.DefaultSystemAddr
	}

	var hub *router.Router
	pointer := cdp.New(cdp.Options{
		Caller:    cdp.NewPageClient(fmt.Sprintf("http://127.0.0.1:%d", cfg.PlatformDebugPort)),
		Sensitive: func() bool { return hub.Sensitive() },
		Emit:      r.Emit,
		Color:     func(id string) string { return hub.Color(id) },
	})
	padAdapter, err := pads.Listen(root, playersAddr, systemAddr, func(id string) (int, bool) {
		return hub.SlotOf(id)
	})
	if err != nil {
		log.Printf("pad: virtual pads: %v", err)
	}
	hub, err = router.New(router.Config{
		Emit:     r.Emit,
		Pointer:  pointer,
		Pads:     padAdapter,
		StartURL: cfg.URL,
		// Phone input is invisible to Windows, so idle tools on the kiosk
		// (screensaver, vendor "no mouse for 5 min" Chrome killers) would
		// fire mid-game without this.
		Activity: useractivity.Default().Touch,
		Snap:     r.snapGameArea,
		ProfileReply: func(playerID string, reply protocol.ProfileReply) {
			r.profileReplied(playerID, reply)
		},
	})
	if err != nil {
		// Only nil providers or adapters fail here, and none are nil.
		panic(err)
	}
	go pointer.Run(root)
	// Publish and apply D9 under padMu, so a pad.sensitive that lands while
	// the hub was being built is not lost.
	r.padMu.Lock()
	r.padRouter = hub
	hub.SetSensitive(r.padSensitive)
	r.padMu.Unlock()
	return hub
}

// eventIdleTimeout is how long an empty session may sit before it ends by
// itself: only in event mode, never shorter than a minute.
// padEmptyTimeout is how long a session with no phone connected lasts.
func padEmptyTimeout(cfg config.Config) time.Duration {
	if cfg.EventMode {
		return eventIdleTimeout(cfg)
	}
	return minutesOr(cfg.PadEmptyMinutes, 5)
}

// padInactiveTimeout is how long connected phones may go unused.
func padInactiveTimeout(cfg config.Config) time.Duration {
	return minutesOr(cfg.PadIdleMinutes, 15)
}

func minutesOr(minutes, fallback int) time.Duration {
	switch {
	case minutes < 0:
		return 0
	case minutes == 0:
		minutes = fallback
	}
	return time.Duration(minutes) * time.Minute
}

func eventIdleTimeout(cfg config.Config) time.Duration {
	if !cfg.EventMode {
		return 0
	}
	minutes := cfg.EventIdleMinutes
	if minutes <= 0 {
		minutes = 5
	}
	return time.Duration(minutes) * time.Minute
}
