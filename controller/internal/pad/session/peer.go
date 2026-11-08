package session

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

func newPeerAPI() *webrtc.API {
	var settings webrtc.SettingEngine
	// Host candidates stay numeric. A phone on mobile data cannot resolve
	// the kiosk's mDNS name, and TURN/STUN cover the path that needs a relay.
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	return webrtc.NewAPI(webrtc.WithSettingEngine(settings))
}

type peer struct {
	id      string
	gen     int
	pc      *webrtc.PeerConnection
	control *webrtc.DataChannel
	input   *webrtc.DataChannel

	closed   atomic.Bool
	fallback atomic.Bool
	sendMu   sync.Mutex

	mu        sync.Mutex
	remoteSet bool
	pending   []webrtc.ICECandidateInit
}

func openPeer(api *webrtc.API, servers []webrtc.ICEServer, id string, gen int, hooks peerHooks) (*peer, error) {
	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: servers})
	if err != nil {
		return nil, err
	}
	p := &peer{id: id, gen: gen, pc: pc}
	ordered := true
	control, err := pc.CreateDataChannel("control", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	unordered := false
	var retries uint16
	input, err := pc.CreateDataChannel("input", &webrtc.DataChannelInit{
		Ordered:        &unordered,
		MaxRetransmits: &retries,
	})
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	p.control = control
	p.input = input

	control.OnMessage(func(msg webrtc.DataChannelMessage) {
		if p.closed.Load() {
			return
		}
		hooks.onControl(id, gen, append([]byte(nil), msg.Data...))
	})
	input.OnMessage(func(msg webrtc.DataChannelMessage) {
		if p.closed.Load() {
			return
		}
		hooks.onInput(id, gen, append([]byte(nil), msg.Data...))
	})
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil || p.closed.Load() {
			return
		}
		hooks.onICE(id, gen, candidate.ToJSON())
	})
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if p.closed.Load() {
			return
		}
		switch state {
		case webrtc.ICEConnectionStateFailed:
			p.fallback.Store(true)
			hooks.onICEFailed(id, gen)
		case webrtc.ICEConnectionStateConnected, webrtc.ICEConnectionStateCompleted:
			p.fallback.Store(false)
			hooks.onICEUp(id, gen)
		}
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if p.closed.Load() {
			return
		}
		if state == webrtc.PeerConnectionStateFailed {
			p.fallback.Store(true)
			hooks.onICEFailed(id, gen)
		}
	})
	return p, nil
}

type peerHooks struct {
	onControl   func(playerID string, gen int, data []byte)
	onInput     func(playerID string, gen int, data []byte)
	onICE       func(playerID string, gen int, candidate webrtc.ICECandidateInit)
	onICEFailed func(playerID string, gen int)
	onICEUp     func(playerID string, gen int)
}

func (p *peer) Close() {
	if p == nil || !p.closed.CompareAndSwap(false, true) {
		return
	}
	_ = p.pc.Close()
}

func (p *peer) offer() (webrtc.SessionDescription, error) {
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return webrtc.SessionDescription{}, err
	}
	local := p.pc.LocalDescription()
	if local == nil {
		return webrtc.SessionDescription{}, errMissingLocal
	}
	return *local, nil
}

var errMissingLocal = errString("pad session: missing local description")

type errString string

func (e errString) Error() string { return string(e) }

func (p *peer) acceptAnswer(raw json.RawMessage) error {
	var desc webrtc.SessionDescription
	if err := json.Unmarshal(raw, &desc); err != nil {
		return err
	}
	if desc.Type == webrtc.SDPTypeUnknown {
		desc.Type = webrtc.SDPTypeAnswer
	}
	if err := p.pc.SetRemoteDescription(desc); err != nil {
		return err
	}
	p.mu.Lock()
	p.remoteSet = true
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	for _, candidate := range pending {
		if err := p.pc.AddICECandidate(candidate); err != nil {
			return err
		}
	}
	return nil
}

func (p *peer) addICE(raw json.RawMessage) error {
	var init webrtc.ICECandidateInit
	if err := json.Unmarshal(raw, &init); err != nil {
		return err
	}
	if init.Candidate == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.remoteSet {
		p.pending = append(p.pending, init)
		return nil
	}
	return p.pc.AddICECandidate(init)
}

func (p *peer) sendControlText(text string) error {
	if p.closed.Load() || p.control == nil || p.control.ReadyState() != webrtc.DataChannelStateOpen {
		return errString("pad session: control channel is not open")
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	return p.control.SendText(text)
}

func (p *peer) controlReady() bool {
	return p != nil && !p.closed.Load() && p.control != nil && p.control.ReadyState() == webrtc.DataChannelStateOpen
}
