package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"kiosk/controller/internal/pad/protocol"
)

const (
	defaultPingInterval = 5 * time.Second
	reconnectDelay      = 2 * time.Second
	shutdownBudget      = 5 * time.Second
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
	// Audio sends the kiosk's sound to the speaker phone. nil leaves the
	// peers without an audio track.
	Audio *Audio
}

// Player is a roster entry: the contracts §2 Player JSON, which carries the
// speaker role (the one phone that plays the kiosk's sound).
type Player struct {
	protocol.Player
	Speaker bool `json:"speaker"`
}

// SessionView is the bridge session object: code, the current QR URL and
// whether anyone with the code can join without scanning the QR.
type SessionView struct {
	Code      string `json:"code"`
	JoinURL   string `json:"join_url"`
	LobbyOpen bool   `json:"lobby_open"`
}

// Snapshot is the pad.players payload (session is null when none is running).
type Snapshot struct {
	Session *SessionView `json:"session"`
	Players []Player     `json:"players"`
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

	mu        sync.Mutex
	running   bool
	active    bool
	record    *SessionRecord
	token     string
	joinURL   string
	ice       ICEConfig
	iceAt     time.Time
	players   map[string]*playerState
	peers     map[string]*peer
	negGen    map[string]int
	pings     map[string]pendingPing
	watchers  map[int]func(Snapshot)
	nextWatch int
	sensitive bool
	// speakerID holds the speaker role as Rails reports it. audioTo is the
	// player whose track the encoder writes into: the speaker, or the host
	// while the speaker is not connected, or nobody.
	speakerID   string
	audioTo     string
	audioTarget *webrtc.TrackLocalStaticSample
	onControl   func(playerID string, msg any)
	onInput     func(playerID string, state protocol.InputState)
	onPlayer    func(ev PlayerEvent)
	cable       *cable
}

type playerState struct {
	protocol.Player
	input    protocol.InputState
	hasInput bool
	rtt      time.Duration
	hasRTT   bool
	hello    *protocol.Hello
	fallback bool
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
	m.mu.Unlock()
	m.emit()

	runCtx, cancel := context.WithCancel(ctx)
	// Deferred calls run last-in first-out: delete, cancel the loops, wait
	// for them, then the reset above. A QR tick that snapshotted the live
	// session therefore emits before the final, ended snapshot.
	var loops sync.WaitGroup
	defer loops.Wait()
	defer cancel()
	defer m.deleteIfStopped(ctx, record.ID)

	loops.Add(2)
	go func() { defer loops.Done(); m.qrLoop(runCtx) }()
	go func() { defer loops.Done(); m.pingLoop(runCtx) }()

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
	snap := Snapshot{Players: m.rosterLocked()}
	if m.record != nil && m.active {
		snap.Session = &SessionView{Code: m.record.Code, JoinURL: m.joinURL, LobbyOpen: m.record.LobbyOpen}
	}
	return snap
}

// rosterLocked is the sorted roster with the speaker flag. m.mu held.
func (m *Manager) rosterLocked() []Player {
	sorted := sortedPlayers(m.players)
	out := make([]Player, len(sorted))
	for i, p := range sorted {
		out[i] = m.withSpeakerLocked(p)
	}
	return out
}

func (m *Manager) withSpeakerLocked(p protocol.Player) Player {
	return Player{Player: p, Speaker: p.ID != "" && p.ID == m.speakerID}
}

// Speaker returns the player holding the speaker role ("" when none).
func (m *Manager) Speaker() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.speakerID
}

// AudioTarget returns the player whose phone gets the sound now: the
// speaker, or the host while the speaker is away, or "" for nobody.
func (m *Manager) AudioTarget() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.audioTo
}

// SetSpeaker moves the speaker role to playerID (system be_speaker, or the
// overlay's pad.speaker). Rails stores it; the sound moves to that phone
// right away, without renegotiation.
func (m *Manager) SetSpeaker(ctx context.Context, playerID string) error {
	id, err := m.sessionID()
	if err != nil {
		return err
	}
	m.mu.Lock()
	st := m.players[playerID]
	prev := m.speakerID
	m.mu.Unlock()
	if st == nil {
		return fmt.Errorf("pad session: unknown player %s", playerID)
	}
	if prev == playerID {
		return nil
	}
	if err := m.api.TransferSpeaker(ctx, id, playerID); err != nil {
		return err
	}
	m.mu.Lock()
	if m.players[playerID] == nil {
		m.mu.Unlock()
		return nil
	}
	m.speakerID = playerID
	var changed []Player
	for _, pid := range []string{prev, playerID} {
		if st := m.players[pid]; st != nil {
			changed = append(changed, m.withSpeakerLocked(st.Player))
		}
	}
	m.mu.Unlock()
	log.Printf("pad session: speaker is now %s", playerID)
	m.emit()
	for _, p := range changed {
		_ = m.pushPlayer(p, false)
	}
	m.sendVolume(playerID)
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
	speaker := m.speakerID
	m.mu.Unlock()
	if speaker != "" {
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

// syncSpeaker points the session audio at the right phone's track and
// starts or stops the capture with it.
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
	to := ""
	var target *webrtc.TrackLocalStaticSample
	if m.active {
		to = m.effectiveSpeakerLocked()
		if p := m.peers[to]; p != nil && !p.closed.Load() && p.audio != nil {
			target = p.audio
		} else {
			to = ""
		}
	}
	if to == m.audioTo && target == m.audioTarget {
		m.mu.Unlock()
		return
	}
	m.audioTo = to
	m.audioTarget = target
	m.mu.Unlock()
	if target == nil {
		a.SetTarget(nil) // an untyped nil: no speaker
	} else {
		a.SetTarget(target)
	}
	if to == "" {
		log.Printf("pad session: no speaker phone; audio capture stops")
	} else {
		log.Printf("pad session: sound goes to %s", to)
	}
}

// effectiveSpeakerLocked: the speaker if connected, else the connected host.
func (m *Manager) effectiveSpeakerLocked() string {
	if st := m.players[m.speakerID]; st != nil && st.Connected {
		return m.speakerID
	}
	for id, st := range m.players {
		if st.Host && st.Connected {
			return id
		}
	}
	return ""
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
// sends the old and the new speaker as separate updates in either order, so
// a false only clears the role if that player still holds it.
func (m *Manager) noteSpeakerLocked(p Player) {
	switch {
	case p.Speaker:
		m.speakerID = p.ID
	case m.speakerID == p.ID:
		m.speakerID = ""
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
	log.Printf("pad session: player %s ICE failed; control falls back to ActionCable", id)
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
	st.input = state
	st.hasInput = true
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
	if m.speakerID == playerID {
		// Rails hands the role to the host and says so with player_updated.
		m.speakerID = ""
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
	speaker := ""
	for _, p := range list {
		seen[p.ID] = true
		st := m.players[p.ID]
		if st == nil {
			st = &playerState{}
			m.players[p.ID] = st
		}
		st.Player = p.Player
		if p.Speaker && (speaker == "" || p.ID == m.speakerID) {
			speaker = p.ID
		}
	}
	for id := range m.players {
		if !seen[id] {
			delete(m.players, id)
		}
	}
	m.speakerID = speaker
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

func (m *Manager) pingAll() {
	m.mu.Lock()
	var ids []string
	for id, st := range m.players {
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
