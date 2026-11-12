package session

import (
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"kiosk/controller/internal/pad/protocol"
)

// videoFmtp is the H.264 profile every phone decodes in hardware: constrained
// baseline, level 3.1, packetization mode 1.
const videoFmtp = "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"

// touchReleaseAfter is how long a touch may go quiet while a finger is down
// before the kiosk lifts it (the phone sends at 60 Hz while any point is
// down, so this is about 15 missed frames).
const touchReleaseAfter = 250 * time.Millisecond

const (
	relayedNo  = 1
	relayedYes = 2
)

// RTPWriter is one phone's video track (*webrtc.TrackLocalStaticRTP
// satisfies it).
type RTPWriter interface {
	WriteRTP(p *rtp.Packet) error
}

func newVideoTrack(playerID string) (*webrtc.TrackLocalStaticRTP, error) {
	return webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: videoFmtp,
	}, "video", "yui-"+playerID)
}

// drainRTCP reads and drops the sender's RTCP so the interceptors (NACK,
// reports) keep running. It returns when the sender closes.
func drainRTCP(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

// VideoWriter returns the player's video track while their peer is connected
// over WebRTC (never in cable fallback, and not before the control channel
// has opened).
func (m *Manager) VideoWriter(playerID string) (RTPWriter, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	peer := m.peers[playerID]
	if peer == nil || peer.video == nil || peer.closed.Load() || peer.fallback.Load() || !peer.controlReady() {
		return nil, false
	}
	if st := m.players[playerID]; st != nil && st.fallback {
		return nil, false
	}
	return peer.video, true
}

// Relayed reports whether the player's selected ICE pair uses a relay
// candidate. ok is false without a live WebRTC link.
func (m *Manager) Relayed(playerID string) (relayed bool, ok bool) {
	m.mu.Lock()
	peer := m.peers[playerID]
	m.mu.Unlock()
	if peer == nil || peer.closed.Load() || peer.fallback.Load() {
		return false, false
	}
	return peer.relayedNow()
}

// relayedNow reads the selected pair and refreshes the cache.
func (p *peer) relayedNow() (relayed, ok bool) {
	switch p.link().Path {
	case "lan", "stun":
		p.relayed.Store(relayedNo)
		return false, true
	case "turn":
		p.relayed.Store(relayedYes)
		return true, true
	}
	return false, false
}

// trackRelayed fills the peer's cached relay flag once ICE has nominated a
// pair, and republishes the roster when it learns it.
func (m *Manager) trackRelayed(p *peer) {
	for _, wait := range []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond, 4 * time.Second} {
		if wait > 0 {
			time.Sleep(wait)
		}
		if p.closed.Load() {
			return
		}
		before := p.relayed.Load()
		if _, ok := p.relayedNow(); ok {
			if p.relayed.Load() != before {
				m.emit()
			}
			return
		}
	}
}

// Caps returns what the phone said in its hello.
func (m *Manager) Caps(playerID string) (protocol.Caps, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.players[playerID]; st != nil && st.hello != nil {
		return st.hello.Caps, true
	}
	return protocol.Caps{}, false
}

// SetTouchHandler receives decoded touch frames, after stale seq values are
// dropped and including the release the kiosk makes when a phone goes quiet
// with a finger down. It runs without the manager lock. nil turns it off.
func (m *Manager) SetTouchHandler(fn func(playerID string, t protocol.TouchState)) {
	m.mu.Lock()
	m.onTouch = fn
	m.mu.Unlock()
}

// SetPeerHandler is told when a player's peer becomes usable (up=true: the
// control channel of a fresh peer opened, or ICE recovered on the same peer)
// and when it is closed or replaced, or its ICE failed (up=false). It runs
// without the manager lock. nil turns it off.
func (m *Manager) SetPeerHandler(fn func(playerID string, up bool)) {
	m.mu.Lock()
	m.onPeer = fn
	m.mu.Unlock()
}

func (m *Manager) notifyPeer(playerID string, up bool) {
	m.mu.Lock()
	fn := m.onPeer
	m.mu.Unlock()
	if fn != nil {
		fn(playerID, up)
	}
}

func (m *Manager) noteControlOpen(id string, gen int) {
	if !m.peerCurrent(id, gen) {
		return
	}
	m.notifyPeer(id, true)
}

// touchTrack is the per-player state of the touch stream.
type touchTrack struct {
	seq    uint16
	hasSeq bool
	down   bool // some point was down in the last accepted frame
	gen    uint64
	timer  *time.Timer
}

func (t *touchTrack) stop() {
	t.gen++
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.down = false
}

func anyDown(s protocol.TouchState) bool {
	return s.Points[0].Down || s.Points[1].Down
}

func (m *Manager) applyTouch(playerID string, raw []byte) {
	ts, err := protocol.DecodeTouch(raw)
	if err != nil {
		return
	}
	m.mu.Lock()
	st := m.players[playerID]
	if st == nil {
		m.mu.Unlock()
		return
	}
	tt := &st.touch
	if tt.hasSeq && !protocol.SeqNewer(ts.Seq, tt.seq) {
		m.mu.Unlock()
		return
	}
	tt.seq = ts.Seq
	tt.hasSeq = true
	now := m.now()
	m.lastActive = now
	down := anyDown(ts)
	if down && !tt.down {
		m.lastAction = now // a finger went down: a person did that
	}
	if tt.timer != nil {
		tt.timer.Stop()
		tt.timer = nil
	}
	tt.gen++
	tt.down = down
	if down {
		gen := tt.gen
		tt.timer = time.AfterFunc(touchReleaseAfter, func() { m.touchQuiet(playerID, gen) })
	}
	fn := m.onTouch
	m.mu.Unlock()
	if fn != nil {
		fn(playerID, ts)
	}
}

// touchQuiet lifts a player's fingers after touchReleaseAfter of silence.
func (m *Manager) touchQuiet(playerID string, gen uint64) {
	m.mu.Lock()
	st := m.players[playerID]
	if st == nil || st.touch.gen != gen || !st.touch.down {
		m.mu.Unlock()
		return
	}
	st.touch.down = false
	st.touch.timer = nil
	// One past the last real seq; the stored seq is left alone so a late
	// real frame is still accepted.
	release := protocol.TouchState{Seq: st.touch.seq + 1}
	fn := m.onTouch
	m.mu.Unlock()
	if fn != nil {
		fn(playerID, release)
	}
}
