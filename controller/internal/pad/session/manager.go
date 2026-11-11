package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"kiosk/controller/internal/pad/protocol"
)

const (
	defaultPingInterval = 5 * time.Second
	reconnectDelay      = 2 * time.Second
	shutdownBudget      = 5 * time.Second
	// Rails frees a silent phone's slot without always telling the kiosk, so
	// the roster is re-read this often.
	defaultResyncInterval = 30 * time.Second
)

// Config is the kiosk side of one play-session runner.
type Config struct {
	ServerURL    string
	DeviceToken  string
	KioskName    string
	HTTPClient   *http.Client
	Dialer       *websocket.Dialer
	Now          func() time.Time
	PingInterval time.Duration
	// ResyncInterval is how often the roster is re-read from Rails while
	// signaling is up (default 30 s).
	ResyncInterval time.Duration
	// Audio sends the kiosk's sound to the speaker phone. nil leaves the
	// peers without an audio track.
	Audio *Audio
	// OpenLobby opens the lobby (code-only joins) as soon as the session is
	// up, including a reused one. Event mode sets it.
	OpenLobby bool
	// IdleTimeout ends the session by itself once no player is connected
	// and nothing has happened (join, leave, input) for this long. Zero
	// leaves idle sessions alone. IdleTick is how often that is checked
	// (default 5 s); the elapsed time is measured with Now.
	IdleTimeout time.Duration
	IdleTick    time.Duration
	// InactiveTimeout ends the session when phones are connected but nobody
	// has done anything on purpose (button, stick, pointer, a layout control,
	// joining) for this long, so phones forgotten in pockets don't keep it
	// open. Wake locks don't count: a running game holds one for as long
	// as it runs, played or not. Phones get an "idle" warning
	// InactiveWarning before the end (default 60 s). Zero disables it.
	InactiveTimeout time.Duration
	InactiveWarning time.Duration
}

// Player is a roster entry: the contracts §2 Player JSON, which carries the
// speaker role (the one phone that plays the kiosk's sound).
type Player struct {
	protocol.Player
	Speaker bool `json:"speaker"`
	// SpeakerChannel is "left" or "right" while two speakers form a stereo
	// pair; empty for a mono speaker (the whole mix) and for non-speakers.
	SpeakerChannel string `json:"speaker_channel,omitempty"`
	// Link health for the overlay (pad.players only): the last control
	// round trip and accepted input frames per second over the last ping
	// interval. Absent until measured.
	RTTMillis *int     `json:"rtt_ms,omitempty"`
	InputHz   *float64 `json:"input_hz,omitempty"`
}

// SessionView is the bridge session object: code, the current QR URL and
// whether anyone with the code can join without scanning the QR.
type SessionView struct {
	Code      string `json:"code"`
	JoinURL   string `json:"join_url"`
	LobbyOpen bool   `json:"lobby_open"`
	// WiFi is the kiosk hotspot, when it is on.
	WiFi *WiFi `json:"wifi,omitempty"`
	// WiFiPending is true while the hotspot is still starting, so the
	// overlay can hold its place instead of jumping when it appears.
	WiFiPending bool `json:"wifi_pending,omitempty"`
}

// Snapshot is the pad.players payload (session is null when none is running).
type Snapshot struct {
	Session *SessionView `json:"session"`
	Players []Player     `json:"players"`
	// Seq orders snapshots across every manager in the process: a reader
	// that got a newer one (as an event or a command reply) drops an older
	// one that arrives late.
	Seq uint64 `json:"seq"`
}

// snapshotSeq numbers snapshots process-wide, so a new session's snapshots
// always sort after the last one of the session before it.
var snapshotSeq atomic.Uint64

// EmptySnapshot is the no-session view, numbered like any other snapshot.
func EmptySnapshot() Snapshot {
	return Snapshot{Players: []Player{}, Seq: snapshotSeq.Add(1)}
}

// PlayerInput is the latest accepted input frame for one player.
type PlayerInput struct {
	PlayerID string
	State    protocol.InputState
}

// Manager is safe for concurrent callers. Run blocks until the context ends
// or signaling is rejected.
type Manager struct {
	cfg     Config
	api     *apiClient
	peerAPI *webrtc.API

	audioMu sync.Mutex // see syncSpeaker

	mu      sync.Mutex
	emitMu  sync.Mutex
	running bool
	active  bool
	record  *SessionRecord
	token   string
	joinURL string
	wifi    *WiFi
	// wifiPending: the hotspot is starting and wifi is not set yet.
	wifiPending bool
	ice         ICEConfig
	iceAt       time.Time
	players     map[string]*playerState
	peers       map[string]*peer
	negGen      map[string]int
	pings       map[string]pendingPing
	watchers    map[int]func(Snapshot)
	nextWatch   int
	sensitive   bool
	// speakerID holds the speaker role as Rails reports it. audioTo is the
	// player whose track the encoder writes into: the speaker, or the host
	// while the speaker is not connected, or nobody.
	//
	// With a stereo pair speakerID is the left phone and rightID the right
	// one (both empty or only speakerID set otherwise). audioTo is then the
	// left phone's player and audioRight the right one's; audioRight is ""
	// for a single stream.
	speakerID        string
	rightID          string
	audioTo          string
	audioRight       string
	audioTarget      *webrtc.TrackLocalStaticSample
	audioRightTarget *webrtc.TrackLocalStaticSample
	onControl        func(playerID string, msg any)
	onInput          func(playerID string, state protocol.InputState)
	onPlayer         func(ev PlayerEvent)
	cable            *cable
	// lastActive is the clock reading of the last join, leave or accepted
	// input, or the session start. Used by the idle timeout.
	lastActive time.Time
	// lastAction is the last thing a person did (see InactiveTimeout);
	// idleWarned is set while the phones show the idle warning.
	lastAction time.Time
	idleWarned bool
	// abandoned is set when the session ended on its own because nobody was
	// there (no phones, or nobody played); see Abandoned.
	abandoned bool
}

type playerState struct {
	protocol.Player
	input    protocol.InputState
	hasInput bool
	rtt      time.Duration
	hasRTT   bool
	// frames counts accepted input frames since rateSince; pingAll turns
	// them into inputHz.
	frames    int
	rateSince time.Time
	inputHz   float64
	hasRate   bool
	hello     *protocol.Hello
	fallback  bool
}

type pendingPing struct {
	id   int64
	sent time.Time
}

// Open validates config and does not contact the server.
func Open(cfg Config) (*Manager, error) {
	if cfg.ServerURL == "" || cfg.DeviceToken == "" {
		return nil, errors.New("pad session: server URL and device token are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = defaultPingInterval
	}
	if cfg.ResyncInterval <= 0 {
		cfg.ResyncInterval = defaultResyncInterval
	}
	return &Manager{
		cfg:     cfg,
		api:     newAPI(cfg.ServerURL, cfg.DeviceToken, cfg.HTTPClient),
		peerAPI: newPeerAPI(),
		players: map[string]*playerState{},
		peers:   map[string]*peer{},
		negGen:  map[string]int{},
		pings:   map[string]pendingPing{},
	}, nil
}

// Run creates or reuses the kiosk session, subscribes to signaling, and
// deletes the session when ctx is cancelled (controller stop or host end).
func (m *Manager) Run(ctx context.Context) error {
	// The idle timeout ends the run like a host end: cancelling this
	// derived context deletes the session on the way out.
	ctx, stopSelf := context.WithCancel(ctx)
	defer stopSelf()
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return errors.New("pad session: already running")
	}
	m.running = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.running = false
		m.active = false
		m.record = nil
		m.token = ""
		m.joinURL = ""
		m.speakerID = ""
		m.rightID = ""
		left := sortedPlayers(m.players)
		m.players = map[string]*playerState{}
		m.mu.Unlock()
		m.closePeers()
		m.syncSpeaker()
		for _, p := range left {
			m.notifyPlayer(PlayerLeft, p)
		}
		m.emit()
	}()

	record, created, err := m.api.Create(ctx)
	if err != nil {
		return err
	}
	if created {
		log.Printf("pad session: started %s code %s", record.ID, record.Code)
	} else {
		log.Printf("pad session: reusing %s code %s", record.ID, record.Code)
	}
	if m.cfg.Audio != nil {
		// Each session starts at full volume; the speaker's slider lowers it.
		m.cfg.Audio.SetVolume(1)
	}
	m.mu.Lock()
	m.record = &record
	m.mergePlayersLocked(record.Players)
	m.mu.Unlock()
	if err := m.publishQR(); err != nil {
		log.Printf("pad session: qr: %v", err)
	}
	m.mu.Lock()
	m.active = true
	m.lastActive = m.now()
	m.lastAction = m.lastActive
	m.mu.Unlock()
	m.emit()
	if m.cfg.OpenLobby && !record.LobbyOpen {
		if err := m.SetLobbyOpen(ctx, true); err != nil {
			log.Printf("pad session: open lobby: %v", err)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	// Deferred calls run last-in first-out: delete, cancel the loops, wait
	// for them, then the reset above. A QR tick that snapshotted the live
	// session therefore emits before the final, ended snapshot.
	var loops sync.WaitGroup
	defer loops.Wait()
	defer cancel()
	defer m.deleteIfStopped(ctx, record.ID)

	loops.Add(3)
	if m.cfg.IdleTimeout > 0 || m.cfg.InactiveTimeout > 0 {
		loops.Add(1)
		go func() { defer loops.Done(); m.idleLoop(runCtx, stopSelf) }()
	}
	go func() { defer loops.Done(); m.qrLoop(runCtx) }()
	go func() { defer loops.Done(); m.pingLoop(runCtx) }()
	go func() { defer loops.Done(); m.resyncLoop(runCtx, record.ID) }()

	for {
		if ctx.Err() != nil {
			return nil
		}
		err := m.connectOnce(runCtx, record.ID)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errSubscriptionRejected) || errors.Is(err, errSessionGone) {
			return err
		}
		log.Printf("pad session: signaling: %v", err)
		timer := time.NewTimer(reconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (m *Manager) deleteIfStopped(ctx context.Context, id string) {
	if ctx.Err() == nil {
		return
	}
	m.sendByes()
	endCtx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancel()
	if err := m.api.Destroy(endCtx, id); err != nil {
		log.Printf("pad session: end %s: %v", id, err)
	}
}

// Active reports whether a play session is up. The asset syncer pauses on this.
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// SetSensitive stores the D9 flag. Plan 03/04 drop pointer and text input
// while it is set; this package only keeps the bit.
func (m *Manager) SetSensitive(active bool) {
	m.mu.Lock()
	m.sensitive = active
	m.mu.Unlock()
}

// Sensitive reports the D9 flag.
func (m *Manager) Sensitive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sensitive
}

// SetControlHandler receives control messages other than hello and ping/pong,
// which this package answers itself. A nil handler discards them.
func (m *Manager) SetControlHandler(fn func(playerID string, msg any)) {
	m.mu.Lock()
	m.onControl = fn
	m.mu.Unlock()
}

// SetInputHandler receives every accepted input frame (stale seq values are
// already dropped), after LatestInput has been updated. It runs on the
// peer's data-channel goroutine without the manager lock. nil turns it off.
func (m *Manager) SetInputHandler(fn func(playerID string, state protocol.InputState)) {
	m.mu.Lock()
	m.onInput = fn
	m.mu.Unlock()
}

// SetPlayerHandler receives player lifecycle events (see PlayerEvent). It
// runs without the manager lock. nil turns it off.
func (m *Manager) SetPlayerHandler(fn func(ev PlayerEvent)) {
	m.mu.Lock()
	m.onPlayer = fn
	m.mu.Unlock()
}

// SendControl sends one §3 control object to a player over the control
// channel, or over signaling while that player's ICE has failed.
func (m *Manager) SendControl(playerID string, msg any) error {
	return m.sendControl(playerID, msg)
}

// Snapshot copies the roster and the current join URL.
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() Snapshot {
	snap := Snapshot{Players: m.rosterLocked(), Seq: snapshotSeq.Add(1)}
	if m.record != nil && m.active {
		snap.Session = &SessionView{Code: m.record.Code, JoinURL: m.joinURL, LobbyOpen: m.record.LobbyOpen, WiFi: m.wifi, WiFiPending: m.wifi == nil && m.wifiPending}
	}
	return snap
}

// rosterLocked is the sorted roster with the speaker flag. m.mu held.
func (m *Manager) rosterLocked() []Player {
	sorted := sortedPlayers(m.players)
	out := make([]Player, len(sorted))
	for i, p := range sorted {
		out[i] = m.withSpeakerLocked(p)
		if st := m.players[p.ID]; st != nil {
			if st.hasRTT {
				ms := int(st.rtt.Round(time.Millisecond) / time.Millisecond)
				out[i].RTTMillis = &ms
			}
			if st.hasRate {
				hz := st.inputHz
				out[i].InputHz = &hz
			}
		}
	}
	return out
}

func (m *Manager) withSpeakerLocked(p protocol.Player) Player {
	rp := Player{Player: p, Speaker: p.ID != "" && (p.ID == m.speakerID || p.ID == m.rightID)}
	if m.speakerID != "" && m.rightID != "" {
		switch p.ID {
		case m.speakerID:
			rp.SpeakerChannel = "left"
		case m.rightID:
			rp.SpeakerChannel = "right"
		}
	}
	return rp
}

// Speaker returns the player holding the speaker role ("" when none).
func (m *Manager) Speaker() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.speakerID
}

// AudioTarget returns the player whose phone gets the sound now: the
// speaker, or the host while the speaker is away, or "" for nobody. With a
// stereo pair it is the left phone (see AudioRightTarget).
func (m *Manager) AudioTarget() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.audioTo
}

// AudioRightTarget is the right-channel phone while a stereo pair plays,
// else "".
func (m *Manager) AudioRightTarget() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.audioRight
}

// speakerIDsLocked lists the phones holding a speaker role (left or mono
// first). m.mu held.
func (m *Manager) speakerIDsLocked() []string {
	var ids []string
	for _, id := range []string{m.speakerID, m.rightID} {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// SetSpeaker moves the speaker role to playerID, alone and mono (system
// be_speaker, or the overlay's pad.speaker). Rails stores it; the sound moves
// to that phone right away, without renegotiation.
func (m *Manager) SetSpeaker(ctx context.Context, playerID string) error {
	return m.SetSpeakerChannel(ctx, playerID, "")
}

// SetSpeakerChannel is SetSpeaker with an optional channel: "left" or
// "right" makes playerID that half of a stereo pair with the current speaker,
// who plays the other half (a phone already holding the requested channel is
// dropped as a speaker). Needs another speaker to pair with.
func (m *Manager) SetSpeakerChannel(ctx context.Context, playerID, channel string) error {
	if channel != "" && channel != "left" && channel != "right" {
		return fmt.Errorf("pad session: speaker channel must be left or right, got %q", channel)
	}
	id, err := m.sessionID()
	if err != nil {
		return err
	}
	m.mu.Lock()
	st := m.players[playerID]
	before := m.speakerIDsLocked()
	others := map[string]string{} // other speakers -> current channel
	for _, pid := range before {
		if pid != playerID {
			others[pid] = m.withSpeakerLocked(protocol.Player{ID: pid}).SpeakerChannel
		}
	}
	cur := m.withSpeakerLocked(protocol.Player{ID: playerID}).SpeakerChannel
	isSpeaker := playerID == m.speakerID || playerID == m.rightID
	m.mu.Unlock()
	if st == nil {
		return fmt.Errorf("pad session: unknown player %s", playerID)
	}
	if channel == "" && isSpeaker && cur == "" && len(others) == 0 {
		return nil
	}
	if channel != "" {
		if len(others) == 0 {
			return errors.New("pad session: no speaker to pair with")
		}
		if cur == channel {
			return nil
		}
	}
	if err := m.api.TransferSpeaker(ctx, id, playerID, channel); err != nil {
		return err
	}
	m.mu.Lock()
	if m.players[playerID] == nil {
		m.mu.Unlock()
		return nil
	}
	m.speakerID, m.rightID = "", ""
	if channel == "" {
		m.speakerID = playerID
	} else {
		opposite := "left"
		if channel == "left" {
			opposite = "right"
		}
		partner := ""
		for pid, ch := range others { // at most two speakers: one other, or two
			if ch != channel && (partner == "" || ch == opposite) {
				partner = pid
			}
		}
		holders := map[string]string{channel: playerID, opposite: partner}
		m.speakerID, m.rightID = holders["left"], holders["right"]
	}
	after := m.speakerIDsLocked()
	var changed []Player
	seen := map[string]bool{}
	for _, pid := range append(append([]string{}, before...), after...) {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if st := m.players[pid]; st != nil {
			changed = append(changed, m.withSpeakerLocked(st.Player))
		}
	}
	m.mu.Unlock()
	if channel == "" {
		log.Printf("pad session: speaker is now %s", playerID)
	} else {
		log.Printf("pad session: %s is the %s speaker", playerID, channel)
	}
	m.emit()
	for _, p := range changed {
		_ = m.pushPlayer(p, false)
	}
	for _, pid := range after {
		m.sendVolume(pid)
	}
	return nil
}

// SetVolume sets the hub gain (0–1, linear) and returns the value applied.
// The speaker phone also gets it back as system volume, so its slider
// matches (pad.volume from the overlay, or another phone becoming speaker).
func (m *Manager) SetVolume(v float64) (float64, error) {
	if m.cfg.Audio == nil {
		return 0, errors.New("pad session: no audio")
	}
	v = m.cfg.Audio.SetVolume(v)
	m.mu.Lock()
	speakers := m.speakerIDsLocked()
	m.mu.Unlock()
	for _, speaker := range speakers {
		m.sendVolume(speaker)
	}
	return v, nil
}

// systemValue is a §3 system message whose value is always present (the
// protocol type omits a zero value).
type systemValue struct {
	T      string `json:"t"`
	Action string `json:"action"`
	Value  any    `json:"value"`
}

func (m *Manager) sendVolume(playerID string) {
	if m.cfg.Audio == nil || playerID == "" {
		return
	}
	msg := systemValue{T: "system", Action: "volume", Value: m.cfg.Audio.Volume()}
	m.mu.Lock()
	peer := m.peers[playerID]
	st := m.players[playerID]
	ready := (peer != nil && (peer.fallback.Load() || peer.controlReady())) || (st != nil && st.fallback)
	m.mu.Unlock()
	if !ready {
		return // sent with welcome instead
	}
	if err := m.sendControl(playerID, msg); err != nil {
		log.Printf("pad session: volume to %s: %v", playerID, err)
	}
}

// syncSpeaker points the session audio at the right phone's track (or the
// two tracks of a stereo pair) and starts or stops the capture with it.
func (m *Manager) syncSpeaker() {
	a := m.cfg.Audio
	if a == nil {
		return
	}
	// audioMu orders whole syncs, so the last roster state wins; m.mu is not
	// held while Audio starts or stops a capture.
	m.audioMu.Lock()
	defer m.audioMu.Unlock()
	m.mu.Lock()
	to, right := "", ""
	var target, rightTarget *webrtc.TrackLocalStaticSample
	if m.active {
		to, right = m.effectiveSpeakersLocked()
		target = m.audioTrackLocked(to)
		if right != "" {
			rightTarget = m.audioTrackLocked(right)
		}
		if target == nil || (right != "" && rightTarget == nil) {
			if right != "" && target != nil {
				// The pair's second phone has no peer yet: the first plays
				// the whole mix until it does.
				right, rightTarget = "", nil
			} else {
				to, right, target, rightTarget = "", "", nil, nil
			}
		}
	}
	if to == m.audioTo && right == m.audioRight && target == m.audioTarget && rightTarget == m.audioRightTarget {
		m.mu.Unlock()
		return
	}
	m.audioTo, m.audioRight = to, right
	m.audioTarget, m.audioRightTarget = target, rightTarget
	m.mu.Unlock()
	switch {
	case target == nil:
		a.SetTarget(nil) // an untyped nil: no speaker
		log.Printf("pad session: no speaker phone; audio capture stops")
	case rightTarget != nil:
		a.SetPair(target, rightTarget)
		log.Printf("pad session: stereo sound goes to %s (left) and %s (right)", to, right)
	default:
		a.SetTarget(target)
		log.Printf("pad session: sound goes to %s", to)
	}
}

// audioTrackLocked is the connected player's open Opus track, or nil.
func (m *Manager) audioTrackLocked(id string) *webrtc.TrackLocalStaticSample {
	if p := m.peers[id]; p != nil && !p.closed.Load() && p.audio != nil {
		return p.audio
	}
	return nil
}

// effectiveSpeakersLocked says where the sound goes. A stereo pair with both
// phones connected is (left, right). Otherwise one phone gets everything:
// the first connected speaker, else the connected host.
func (m *Manager) effectiveSpeakersLocked() (string, string) {
	connected := func(id string) bool {
		st := m.players[id]
		return id != "" && st != nil && st.Connected
	}
	if connected(m.speakerID) && connected(m.rightID) {
		return m.speakerID, m.rightID
	}
	for _, id := range []string{m.speakerID, m.rightID} {
		if connected(id) {
			return id, ""
		}
	}
	for id, st := range m.players {
		if st.Host && st.Connected {
			return id, ""
		}
	}
	return "", ""
}

// Watch calls fn after roster changes and after each QR rotation.
// The first call is not automatic; read Snapshot for the current value.
func (m *Manager) Watch(fn func(Snapshot)) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextWatch++
	id := m.nextWatch
	if m.watchers == nil {
		m.watchers = map[int]func(Snapshot){}
	}
	m.watchers[id] = fn
	return func() {
		m.mu.Lock()
		delete(m.watchers, id)
		m.mu.Unlock()
	}
}

// SetLobbyOpen updates the session and keeps the response roster.
func (m *Manager) SetLobbyOpen(ctx context.Context, open bool) error {
	id, err := m.sessionID()
	if err != nil {
		return err
	}
	rec, err := m.api.SetLobbyOpen(ctx, id, open)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.record != nil {
		m.record.LobbyOpen = rec.LobbyOpen
		m.mergePlayersLocked(rec.Players)
	}
	m.mu.Unlock()
	m.emit()
	return nil
}

// Kick removes the player. Rails tells the phone; we also send signaling bye.
func (m *Manager) Kick(ctx context.Context, playerID string) error {
	id, err := m.sessionID()
	if err != nil {
		return err
	}
	if err := m.api.Kick(ctx, id, playerID); err != nil {
		return err
	}
	_ = m.sendSignal(playerID, signalBye, map[string]any{})
	m.removePlayer(playerID)
	return nil
}

// TransferHost asks Rails to move the host flag. The roster updates when
// player_updated arrives.
func (m *Manager) TransferHost(ctx context.Context, playerID string) error {
	id, err := m.sessionID()
	if err != nil {
		return err
	}
	return m.api.TransferHost(ctx, id, playerID)
}

// LatestInput returns the newest frame whose seq was not stale.
func (m *Manager) LatestInput(playerID string) (protocol.InputState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.players[playerID]
	if st == nil || !st.hasInput {
		return protocol.InputState{}, false
	}
	return st.input, true
}

// Inputs lists every player that has an accepted frame.
func (m *Manager) Inputs() []PlayerInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PlayerInput, 0, len(m.players))
	for id, st := range m.players {
		if !st.hasInput {
			continue
		}
		out = append(out, PlayerInput{PlayerID: id, State: st.input})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PlayerID < out[j].PlayerID })
	return out
}

// RoundTrip is the last control-channel RTT measured from our ping.
func (m *Manager) RoundTrip(playerID string) (time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.players[playerID]
	if st == nil || !st.hasRTT {
		return 0, false
	}
	return st.rtt, true
}

func (m *Manager) sessionID() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record == nil || m.record.ID == "" {
		return "", errors.New("pad session: no active session")
	}
	return m.record.ID, nil
}

func (m *Manager) connectOnce(ctx context.Context, sessionID string) error {
	ice, err := m.api.ICE(ctx, sessionID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.ice = ice
	m.iceAt = m.now()
	m.mu.Unlock()

	conn, err := dialCable(ctx, m.cfg.ServerURL, m.cfg.DeviceToken, sessionID, m.cfg.Dialer)
	if err != nil {
		return err
	}
	defer conn.Close()

	m.mu.Lock()
	m.cable = conn
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.cable == conn {
			m.cable = nil
		}
		m.mu.Unlock()
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.readLoop(ctx, m.onCable)
	}()
	if err := conn.subscribe(ctx); err != nil {
		return err
	}
	// Roster events sent while signaling was down are lost, so take the
	// roster from Rails before offering. Joins and leaves that arrive after
	// the subscribe are applied on top by onCable.
	if err := m.resync(ctx, sessionID); err != nil {
		if errors.Is(err, errSessionGone) {
			return err
		}
		log.Printf("pad session: roster resync: %v", err)
	}
	m.offerConnected()

	select {
	case <-ctx.Done():
		m.sendByes()
		return nil
	case err := <-errCh:
		return err
	}
}

// resync replaces the roster with the server's and closes peers of players
// who left while signaling was down.
func (m *Manager) resync(ctx context.Context, sessionID string) error {
	rec, err := m.api.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.record == nil || m.record.ID != sessionID {
		m.mu.Unlock()
		return nil
	}
	m.record.LobbyOpen = rec.LobbyOpen
	before := make([]string, 0, len(m.players))
	for id := range m.players {
		before = append(before, id)
	}
	m.mergePlayersLocked(rec.Players)
	var gone []string
	for _, id := range before {
		if m.players[id] == nil {
			gone = append(gone, id)
			delete(m.pings, id)
		}
	}
	present := sortedPlayers(m.players)
	m.mu.Unlock()
	for _, id := range gone {
		m.closePeer(id)
		m.notifyPlayer(PlayerLeft, protocol.Player{ID: id})
	}
	for _, p := range present {
		if p.Connected {
			m.notifyPlayer(PlayerUpdated, p)
		} else {
			m.notifyPlayer(PlayerLeft, p)
		}
	}
	m.emit()
	return nil
}

func (m *Manager) onCable(msg cableMessage) {
	switch msg.Type {
	case "player_joined", "player_left", "player_updated":
		if msg.Player == nil || msg.Player.ID == "" {
			return
		}
		m.applyPlayer(*msg.Player, msg.Type)
	case signalAnswer:
		m.acceptAnswer(msg.From, msg.Data)
	case signalICE:
		m.acceptICE(msg.From, msg.Data)
	case signalHello:
		m.onSignalHello(msg.From)
	case signalControl:
		if msg.From == "" || len(msg.Data) == 0 {
			return
		}
		m.handleControl(msg.From, msg.Data, 0, true)
	}
}

func (m *Manager) applyPlayer(rp Player, event string) {
	p := rp.Player
	m.mu.Lock()
	st := m.players[p.ID]
	if st == nil {
		st = &playerState{}
		m.players[p.ID] = st
	}
	st.Player = p
	m.lastActive = m.now()
	m.noteSpeakerLocked(rp)
	wasFallback := st.fallback
	pushed := m.withSpeakerLocked(p)
	m.mu.Unlock()
	m.emit()

	switch event {
	case "player_left":
		m.closePeer(p.ID)
		m.notifyPlayer(PlayerLeft, p)
	case "player_joined":
		m.noteAction()
		m.negotiate(p)
	case "player_updated":
		if p.Connected {
			m.negotiateIfIdle(p)
		}
		_ = m.pushPlayer(pushed, wasFallback)
		if p.Connected {
			m.notifyPlayer(PlayerUpdated, p)
		} else {
			m.notifyPlayer(PlayerLeft, p)
		}
	}
}

// noteSpeakerLocked applies one player's speaker flag from Rails. Rails
// sends each changed phone as a separate update in any order, so a false only
// clears a role if that player still holds it.
func (m *Manager) noteSpeakerLocked(p Player) {
	switch {
	case p.Speaker && p.SpeakerChannel == "right":
		m.rightID = p.ID
		if m.speakerID == p.ID {
			m.speakerID = ""
		}
	case p.Speaker:
		m.speakerID = p.ID
		if m.rightID == p.ID {
			m.rightID = ""
		}
	default:
		if m.speakerID == p.ID {
			m.speakerID = ""
		}
		if m.rightID == p.ID {
			m.rightID = ""
		}
	}
}

func (m *Manager) negotiateIfIdle(p protocol.Player) {
	m.mu.Lock()
	peer := m.peers[p.ID]
	idle := peer == nil || peer.closed.Load()
	m.mu.Unlock()
	if idle {
		m.negotiate(p)
	}
}

func (m *Manager) offerConnected() {
	m.mu.Lock()
	var due []protocol.Player
	for _, st := range m.players {
		if !st.Connected {
			continue
		}
		peer := m.peers[st.ID]
		if peer == nil || peer.closed.Load() {
			due = append(due, st.Player)
		}
	}
	m.mu.Unlock()
	for _, p := range due {
		m.negotiate(p)
	}
}

func (m *Manager) negotiate(p protocol.Player) {
	if p.ID == "" {
		return
	}
	m.mu.Lock()
	if !m.active {
		m.mu.Unlock()
		return
	}
	m.negGen[p.ID]++
	gen := m.negGen[p.ID]
	m.forgetSeqLocked(p.ID)
	old := m.peers[p.ID]
	m.peers[p.ID] = nil
	servers := iceServers(m.ice)
	api := m.peerAPI
	m.mu.Unlock()
	if old != nil {
		old.Close()
	}

	if old != nil {
		// The replaced peer may have been the sound's target.
		m.syncSpeaker()
	}
	created, err := openPeer(api, servers, p.ID, gen, m.cfg.Audio != nil, m.hooks())
	if err != nil {
		log.Printf("pad session: peer %s: %v", p.ID, err)
		return
	}
	m.mu.Lock()
	if !m.active || m.negGen[p.ID] != gen {
		m.mu.Unlock()
		created.Close()
		return
	}
	m.peers[p.ID] = created
	m.mu.Unlock()
	m.syncSpeaker()

	desc, err := created.offer()
	if err != nil {
		log.Printf("pad session: offer %s: %v", p.ID, err)
		created.Close()
		m.mu.Lock()
		if m.peers[p.ID] == created {
			delete(m.peers, p.ID)
		}
		m.mu.Unlock()
		return
	}
	if err := m.sendSignal(p.ID, signalOffer, desc); err != nil {
		log.Printf("pad session: send offer %s: %v", p.ID, err)
	}
}

func (m *Manager) hooks() peerHooks {
	return peerHooks{
		onControl: func(id string, gen int, data []byte) {
			m.handleControl(id, data, gen, false)
		},
		onInput: func(id string, gen int, data []byte) {
			if !m.peerCurrent(id, gen) {
				return
			}
			m.applyInput(id, data)
		},
		onICE: func(id string, gen int, candidate webrtc.ICECandidateInit) {
			if !m.peerCurrent(id, gen) {
				return
			}
			if err := m.sendSignal(id, signalICE, candidate); err != nil {
				log.Printf("pad session: ice %s: %v", id, err)
			}
		},
		onICEFailed: m.noteICEFailed,
		onICEUp:     m.noteICEUp,
	}
}

func (m *Manager) peerCurrent(id string, gen int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	peer := m.peers[id]
	return peer != nil && peer.gen == gen && !peer.closed.Load()
}

func (m *Manager) noteICEFailed(id string, gen int) {
	m.mu.Lock()
	peer := m.peers[id]
	if peer == nil || peer.gen != gen {
		m.mu.Unlock()
		return
	}
	st := m.players[id]
	if st != nil {
		st.fallback = true
	}
	peer.fallback.Store(true)
	m.mu.Unlock()
	log.Printf("pad link player=%s path=server: ICE failed; control falls back to ActionCable", id)
}

func (m *Manager) noteICEUp(id string, gen int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	peer := m.peers[id]
	if peer == nil || peer.gen != gen {
		return
	}
	if st := m.players[id]; st != nil {
		st.fallback = false
	}
	peer.fallback.Store(false)
	go logLink(id, peer)
}

// logLink writes the phone's link to the controller log as it comes up and
// once round-trip times have settled, so `debug.logs grep "pad link"` shows
// how every past session connected.
func logLink(id string, p *peer) {
	for i, wait := range []time.Duration{time.Second, 10 * time.Second} {
		time.Sleep(wait)
		if p.closed.Load() {
			return
		}
		l := p.link()
		log.Printf("pad link player=%s check=%d path=%s ice=%s %s %s(%s) -> %s(%s) rtt=%.1fms",
			id, i+1, l.Path, l.ICEState, l.Protocol, l.Local, l.LocalType, l.Remote, l.RemoteType, l.RTTMs)
	}
}

func (m *Manager) onSignalHello(playerID string) {
	m.mu.Lock()
	st := m.players[playerID]
	if st == nil {
		m.mu.Unlock()
		return
	}
	player := st.Player
	peer := m.peers[playerID]
	renegotiate := peer == nil || peer.closed.Load() || peer.fallback.Load() || st.fallback
	m.mu.Unlock()
	if renegotiate {
		m.negotiate(player)
	}
}

func (m *Manager) acceptAnswer(playerID string, raw json.RawMessage) {
	m.mu.Lock()
	peer := m.peers[playerID]
	m.mu.Unlock()
	if peer == nil || peer.closed.Load() {
		return
	}
	if err := peer.acceptAnswer(raw); err != nil {
		log.Printf("pad session: answer %s: %v", playerID, err)
	}
}

func (m *Manager) acceptICE(playerID string, raw json.RawMessage) {
	m.mu.Lock()
	peer := m.peers[playerID]
	m.mu.Unlock()
	if peer == nil || peer.closed.Load() {
		return
	}
	if err := peer.addICE(raw); err != nil {
		log.Printf("pad session: remote ice %s: %v", playerID, err)
	}
}

// forgetSeqLocked makes the next frame from playerID count whatever its seq.
// A new peer is often a reloaded or woken phone whose seq (anchored to its
// clock) can be more than half the u16 range away from the last one seen, so
// keeping the old seq would drop its input for minutes. m.mu held.
func (m *Manager) forgetSeqLocked(playerID string) {
	if st := m.players[playerID]; st != nil {
		st.hasInput = false
	}
}

func (m *Manager) applyInput(playerID string, raw []byte) {
	state, err := protocol.DecodeInput(raw)
	if err != nil {
		log.Printf("pad session: input %s: %v", playerID, err)
		return
	}
	m.mu.Lock()
	st := m.players[playerID]
	if st == nil {
		m.mu.Unlock()
		return
	}
	if st.hasInput && !protocol.SeqNewer(state.Seq, st.input.Seq) {
		m.mu.Unlock()
		return
	}
	if st.hasInput && protocol.Acted(st.input, state) {
		m.lastAction = m.now()
	}
	st.input = state
	st.hasInput = true
	st.frames++
	m.lastActive = m.now()
	fn := m.onInput
	m.mu.Unlock()
	if fn != nil {
		fn(playerID, state)
	}
}

func (m *Manager) removePlayer(playerID string) {
	m.closePeer(playerID)
	m.mu.Lock()
	player := protocol.Player{ID: playerID}
	if st := m.players[playerID]; st != nil {
		player = st.Player
	}
	delete(m.players, playerID)
	delete(m.pings, playerID)
	// Rails hands the role to the host (or the other half of a pair goes back
	// to mono) and says so with player_updated.
	m.lastActive = m.now()
	if m.speakerID == playerID {
		m.speakerID = ""
	}
	if m.rightID == playerID {
		m.rightID = ""
	}
	m.mu.Unlock()
	m.notifyPlayer(PlayerLeft, player)
	m.emit()
}

// closePeer also advances the player's negotiation generation, so a
// negotiate already past its first check drops the peer it is building.
func (m *Manager) closePeer(playerID string) {
	m.mu.Lock()
	m.negGen[playerID]++
	peer := m.peers[playerID]
	delete(m.peers, playerID)
	m.mu.Unlock()
	if peer != nil {
		m.syncSpeaker()
		peer.Close()
	}
}

func (m *Manager) closePeers() {
	m.mu.Lock()
	peers := m.peers
	m.peers = map[string]*peer{}
	m.mu.Unlock()
	m.syncSpeaker()
	for _, peer := range peers {
		peer.Close()
	}
}

func (m *Manager) mergePlayersLocked(list []Player) {
	seen := map[string]bool{}
	speaker, right := "", ""
	for _, p := range list {
		seen[p.ID] = true
		st := m.players[p.ID]
		if st == nil {
			st = &playerState{}
			m.players[p.ID] = st
		}
		st.Player = p.Player
		switch {
		case p.Speaker && p.SpeakerChannel == "right":
			right = p.ID
		case p.Speaker && (speaker == "" || p.ID == m.speakerID):
			speaker = p.ID
		}
	}
	for id := range m.players {
		if !seen[id] {
			delete(m.players, id)
		}
	}
	m.speakerID, m.rightID = speaker, right
}

func (m *Manager) sendSignal(to, typ string, data any) error {
	m.mu.Lock()
	conn := m.cable
	m.mu.Unlock()
	if conn == nil {
		return errors.New("pad session: signaling is down")
	}
	return conn.signal(to, typ, data)
}

func (m *Manager) sendByes() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.players))
	for id := range m.players {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.sendSignal(id, signalBye, map[string]any{})
	}
}

func (m *Manager) sendControl(playerID string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) > maxSignalBytes {
		return fmt.Errorf("pad session: control message is %d bytes", len(raw))
	}
	m.mu.Lock()
	st := m.players[playerID]
	peer := m.peers[playerID]
	fallback := st != nil && st.fallback
	if peer != nil && peer.fallback.Load() {
		fallback = true
	}
	m.mu.Unlock()
	if !fallback && peer != nil && peer.controlReady() {
		return peer.sendControlText(string(raw))
	}
	if !fallback {
		return errors.New("pad session: control channel is not open")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	return m.sendSignal(playerID, signalControl, obj)
}

// playerMsg is §3 "player" with the roster Player (it carries speaker).
type playerMsg struct {
	T      string `json:"t"`
	Player Player `json:"player"`
}

// welcomeMsg is §3 "welcome" with the roster Player (it carries speaker).
type welcomeMsg struct {
	T         string `json:"t"`
	V         int    `json:"v"`
	Player    Player `json:"player"`
	KioskName string `json:"kiosk_name"`
	// WiFiSSID names the kiosk hotspot, for the pad's slow-link hint.
	WiFiSSID string `json:"wifi_ssid,omitempty"`
}

func (m *Manager) pushPlayer(p Player, fallback bool) error {
	m.mu.Lock()
	peer := m.peers[p.ID]
	st := m.players[p.ID]
	ready := fallback || (st != nil && st.fallback) || (peer != nil && (peer.fallback.Load() || peer.controlReady()))
	m.mu.Unlock()
	if !ready {
		return nil
	}
	return m.sendControl(p.ID, playerMsg{T: "player", Player: p})
}

func (m *Manager) publishQR() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.record == nil {
		return nil
	}
	token, err := QRToken(m.record.QRKey, m.now().Unix())
	if err != nil {
		return err
	}
	join, err := JoinURL(m.record.JoinBaseURL, token)
	if err != nil {
		return err
	}
	if token == m.token && join == m.joinURL {
		return nil
	}
	m.token = token
	m.joinURL = join
	return nil
}

func (m *Manager) qrLoop(ctx context.Context) {
	for {
		// Sleep on the wall clock. The token uses Config.Now, but a stalled
		// test clock must not busy-loop this goroutine.
		wait := time.Until(nextQRDeadline(time.Now()))
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := m.publishQR(); err != nil {
				log.Printf("pad session: qr: %v", err)
				continue
			}
			m.emit()
		}
	}
}

// IdleExpired reports whether a session with this many connected players,
// last active at lastActive, has been idle for timeout at now. A zero
// timeout never expires.
func IdleExpired(now, lastActive time.Time, timeout time.Duration, connected int) bool {
	if timeout <= 0 || connected > 0 {
		return false
	}
	return now.Sub(lastActive) >= timeout
}

func (m *Manager) idleExpired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return IdleExpired(m.now(), m.lastActive, m.cfg.IdleTimeout, m.connectedLocked())
}

func (m *Manager) connectedLocked() int {
	connected := 0
	for _, st := range m.players {
		if st.Connected {
			connected++
		}
	}
	return connected
}

// noteAction records that a person did something on purpose.
func (m *Manager) noteAction() {
	m.mu.Lock()
	m.lastAction = m.now()
	m.mu.Unlock()
}

// Idle is the kiosk-to-phone warning that the session is about to end for
// inactivity: EndsIn seconds, or 0 when activity resumed and it is off.
type Idle struct {
	T      string `json:"t"`
	EndsIn int    `json:"ends_in"`
}

// inactiveStep checks the connected-but-inactive timeout. It returns true
// when the session should end, and otherwise the warning to send all
// phones (nil for none).
func (m *Manager) inactiveStep() (end bool, warn *Idle) {
	if m.cfg.InactiveTimeout <= 0 {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectedLocked() == 0 {
		// No phones: the empty-session timeout applies instead.
		m.lastAction = m.now()
	}
	warning := m.cfg.InactiveWarning
	if warning <= 0 {
		warning = time.Minute
	}
	warning = min(warning, m.cfg.InactiveTimeout)
	idle := m.now().Sub(m.lastAction)
	switch {
	case idle >= m.cfg.InactiveTimeout:
		return true, nil
	case idle >= m.cfg.InactiveTimeout-warning && !m.idleWarned:
		m.idleWarned = true
		left := int((m.cfg.InactiveTimeout - idle + time.Second - 1) / time.Second)
		return false, &Idle{T: "idle", EndsIn: left}
	case idle < m.cfg.InactiveTimeout-warning && m.idleWarned:
		m.idleWarned = false
		return false, &Idle{T: "idle", EndsIn: 0}
	}
	return false, nil
}

func (m *Manager) markAbandoned() {
	m.mu.Lock()
	m.abandoned = true
	m.mu.Unlock()
}

// Abandoned reports whether the session ended by itself because nobody was
// there: no phones for IdleTimeout, or nobody played for InactiveTimeout.
// Whatever the session was playing was abandoned too.
func (m *Manager) Abandoned() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.abandoned
}

func (m *Manager) idleLoop(ctx context.Context, end context.CancelFunc) {
	tick := m.cfg.IdleTick
	if tick <= 0 {
		tick = 5 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.idleExpired() {
				log.Printf("pad session: no phones for %s, ending", m.cfg.IdleTimeout)
				m.markAbandoned()
				end()
				return
			}
			stop, warn := m.inactiveStep()
			if stop {
				log.Printf("pad session: nobody played for %s, ending", m.cfg.InactiveTimeout)
				m.markAbandoned()
				end()
				return
			}
			if warn != nil {
				m.mu.Lock()
				ids := make([]string, 0, len(m.players))
				for id, st := range m.players {
					if st.Connected {
						ids = append(ids, id)
					}
				}
				m.mu.Unlock()
				for _, id := range ids {
					_ = m.sendControl(id, warn)
				}
			}
		}
	}
}

func (m *Manager) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pingAll()
		}
	}
}

func (m *Manager) resyncLoop(ctx context.Context, sessionID string) {
	ticker := time.NewTicker(m.cfg.ResyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.cableUp() {
				if err := m.resync(ctx, sessionID); err == nil {
					m.offerConnected()
				}
			}
		}
	}
}

func (m *Manager) cableUp() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cable != nil
}

func (m *Manager) pingAll() {
	m.mu.Lock()
	now := m.now()
	var ids []string
	for id, st := range m.players {
		if !st.rateSince.IsZero() {
			if elapsed := now.Sub(st.rateSince).Seconds(); elapsed > 0 {
				st.inputHz = math.Round(float64(st.frames)/elapsed*10) / 10
				st.hasRate = true
			}
		}
		st.frames = 0
		st.rateSince = now
		peer := m.peers[id]
		ready := peer != nil && peer.controlReady()
		fallback := st.fallback || (peer != nil && peer.fallback.Load())
		if ready || fallback {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.sendPing(id)
	}
	// Refresh the overlay's link health (last round's RTT, this interval's rate).
	if len(ids) > 0 {
		m.emit()
	}
}

func (m *Manager) sendPing(playerID string) {
	m.mu.Lock()
	next := m.pings[playerID].id + 1
	if next < 1 {
		next = 1
	}
	sent := m.now()
	m.pings[playerID] = pendingPing{id: next, sent: sent}
	m.mu.Unlock()
	ping := protocol.Ping{T: "ping", ID: next, TS: sent.UnixMilli()}
	if err := m.sendControl(playerID, ping); err != nil {
		log.Printf("pad session: ping %s: %v", playerID, err)
	}
}

func (m *Manager) notifyPlayer(kind PlayerEventKind, p protocol.Player) {
	m.mu.Lock()
	fn := m.onPlayer
	m.mu.Unlock()
	if fn != nil {
		fn(PlayerEvent{Kind: kind, Player: p})
	}
}

func (m *Manager) emit() {
	// Every roster change comes through here; keep the sound on the right
	// phone before telling the overlay.
	m.syncSpeaker()
	// emitMu keeps delivery in snapshot order. Without it a QR tick and a
	// roster change racing here could hand watchers the older snapshot
	// last, and the overlay would show it (say, no QR) until the next tick.
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.mu.Lock()
	snap := m.snapshotLocked()
	fns := make([]func(Snapshot), 0, len(m.watchers))
	for _, fn := range m.watchers {
		fns = append(fns, fn)
	}
	m.mu.Unlock()
	for _, fn := range fns {
		fn(snap)
	}
}

func (m *Manager) now() time.Time {
	if m.cfg.Now != nil {
		return m.cfg.Now()
	}
	return time.Now()
}

func sortedPlayers(players map[string]*playerState) []protocol.Player {
	out := make([]protocol.Player, 0, len(players))
	for _, st := range players {
		out = append(out, st.Player)
	}
	sort.Slice(out, func(i, j int) bool {
		si, sj := out[i].Slot, out[j].Slot
		switch {
		case si == nil && sj == nil:
			return out[i].ID < out[j].ID
		case si == nil:
			return false
		case sj == nil:
			return true
		case *si != *sj:
			return *si < *sj
		default:
			return out[i].ID < out[j].ID
		}
	})
	return out
}

func iceServers(cfg ICEConfig) []webrtc.ICEServer {
	out := make([]webrtc.ICEServer, 0, len(cfg.IceServers))
	for _, server := range cfg.IceServers {
		if len(server.URLs) == 0 {
			continue
		}
		item := webrtc.ICEServer{URLs: append([]string(nil), server.URLs...)}
		if server.Username != "" || server.Credential != "" {
			item.Username = server.Username
			item.Credential = server.Credential
			item.CredentialType = webrtc.ICECredentialTypePassword
		}
		out = append(out, item)
	}
	return out
}
