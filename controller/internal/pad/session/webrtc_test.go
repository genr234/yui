package session

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"kiosk/controller/internal/pad/protocol"
)

func TestICEServerConversion(t *testing.T) {
	servers := iceServers(ICEConfig{IceServers: []ICEServer{
		{URLs: []string{"stun:stun.example:3478"}},
		{URLs: []string{"turn:turn.example:3478?transport=udp"}, Username: "1767225600:k7", Credential: "abc"},
	}})
	if len(servers) != 2 || servers[0].URLs[0] != "stun:stun.example:3478" {
		t.Fatalf("stun %+v", servers)
	}
	if servers[1].Username != "1767225600:k7" || servers[1].Credential != "abc" || servers[1].CredentialType != webrtc.ICECredentialTypePassword {
		t.Fatalf("turn %+v", servers[1])
	}
}

func TestDataChannelHelloInputAndRTT(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	mgr.cfg.PingInterval = 50 * time.Millisecond
	readyCh := make(chan protocol.Player, 4)
	mgr.SetPlayerHandler(func(ev PlayerEvent) {
		if ev.Kind == PlayerReady {
			readyCh <- ev.Player
		}
	})
	var hookMu sync.Mutex
	var hooked []uint16
	mgr.SetInputHandler(func(id string, state protocol.InputState) {
		hookMu.Lock()
		hooked = append(hooked, state.Seq)
		hookMu.Unlock()
	})
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	phone, err := newPeerAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = phone.Close() }()

	controlCh := make(chan *webrtc.DataChannel, 1)
	inputCh := make(chan *webrtc.DataChannel, 1)
	welcomeCh := make(chan protocol.Welcome, 1)
	pongCh := make(chan protocol.Ping, 1)
	phone.OnDataChannel(func(dc *webrtc.DataChannel) {
		switch dc.Label() {
		case "control":
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				parsed, err := protocol.ParseControl(msg.Data)
				if err != nil {
					return
				}
				switch typed := parsed.(type) {
				case protocol.Welcome:
					select {
					case welcomeCh <- typed:
					default:
					}
				case protocol.Ping:
					if typed.T == "ping" {
						_ = dc.SendText(string(mustJSON(protocol.Ping{T: "pong", ID: typed.ID, TS: typed.TS})))
						return
					}
					select {
					case pongCh <- typed:
					default:
					}
				}
			})
			controlCh <- dc
		case "input":
			inputCh <- dc
		}
	})

	remoteSet := false
	var pending []webrtc.ICECandidateInit
	phone.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		f.push(t, map[string]any{"from": "p1", "type": "ice", "data": candidate.ToJSON()})
	})

	stopForward := make(chan struct{})
	defer close(stopForward)
	go forwardSignals(t, f, phone, &remoteSet, &pending, stopForward)

	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 1, true, true)})

	var control *webrtc.DataChannel
	select {
	case control = <-controlCh:
	case <-time.After(8 * time.Second):
		t.Fatal("control channel was not created")
	}
	waitOpen(t, control)
	hello := protocol.Hello{
		T: "hello", V: 1,
		Caps:   protocol.Caps{Motion: false, Haptics: false, WakeLock: false},
		Screen: protocol.Screen{W: 390, H: 844, Orientation: "portrait"},
	}
	if err := control.SendText(string(mustJSON(hello))); err != nil {
		t.Fatal(err)
	}
	select {
	case welcome := <-welcomeCh:
		if welcome.Player.ID != "p1" || welcome.KioskName != "Lobby" || welcome.V != 1 {
			t.Fatalf("welcome %+v", welcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("welcome was not received")
	}
	select {
	case p := <-readyCh:
		if p.ID != "p1" || !p.Host {
			t.Fatalf("ready %+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ready event missing")
	}
	if err := mgr.SendControl("p1", protocol.Haptic{T: "haptic", Pattern: []int{10}}); err != nil {
		t.Fatalf("SendControl: %v", err)
	}
	if err := control.SendText(string(mustJSON(protocol.Ping{T: "ping", ID: "pad", TS: 42}))); err != nil {
		t.Fatal(err)
	}
	select {
	case pong := <-pongCh:
		if pong.T != "pong" || pong.ID != "pad" || pong.TS != 42 {
			t.Fatalf("pong %+v", pong)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pong was not received")
	}

	var input *webrtc.DataChannel
	select {
	case input = <-inputCh:
	case <-time.After(2 * time.Second):
		t.Fatal("input channel missing")
	}
	if input.Ordered() {
		t.Fatal("remote input channel is ordered")
	}
	retries := input.MaxRetransmits()
	if retries == nil || *retries != 0 {
		t.Fatalf("remote maxRetransmits %+v", retries)
	}
	waitOpen(t, input)
	if err := input.Send(protocol.EncodeInput(protocol.InputState{Seq: 5, Buttons: protocol.ButtonSouth})); err != nil {
		t.Fatal(err)
	}
	if err := input.Send(protocol.EncodeInput(protocol.InputState{Seq: 4, Buttons: protocol.ButtonEast})); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, ok := mgr.LatestInput("p1")
		if ok && state.Seq == 4 {
			t.Fatalf("stale seq applied: %+v", state)
		}
		if ok && state.Seq == 5 && state.Buttons == protocol.ButtonSouth {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	state, ok := mgr.LatestInput("p1")
	if !ok || state.Seq != 5 || state.Buttons != protocol.ButtonSouth {
		t.Fatalf("latest %+v %v", state, ok)
	}
	hookMu.Lock()
	if len(hooked) == 0 || hooked[0] != 5 {
		t.Fatalf("input handler saw %v", hooked)
	}
	for _, seq := range hooked {
		if seq == 4 {
			t.Fatalf("input handler saw the stale frame: %v", hooked)
		}
	}
	hookMu.Unlock()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rtt, ok := mgr.RoundTrip("p1"); ok {
			t.Logf("control rtt %s", rtt)
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("rtt was not recorded")
}

func forwardSignals(t *testing.T, f *fakeRails, phone *webrtc.PeerConnection, remoteSet *bool, pending *[]webrtc.ICECandidateInit, stop <-chan struct{}) {
	t.Helper()
	for {
		select {
		case <-stop:
			return
		default:
		}
		f.mu.Lock()
		var kept, batch []savedSignal
		for _, sig := range f.sigs {
			if sig.Type == signalOffer || sig.Type == signalICE {
				batch = append(batch, sig)
			} else {
				kept = append(kept, sig)
			}
		}
		f.sigs = kept
		f.mu.Unlock()
		for _, sig := range batch {
			switch sig.Type {
			case signalOffer:
				var desc webrtc.SessionDescription
				if json.Unmarshal(sig.Data, &desc) != nil {
					continue
				}
				if phone.SetRemoteDescription(desc) != nil {
					continue
				}
				*remoteSet = true
				for _, candidate := range *pending {
					_ = phone.AddICECandidate(candidate)
				}
				*pending = nil
				answer, err := phone.CreateAnswer(nil)
				if err != nil {
					continue
				}
				if phone.SetLocalDescription(answer) != nil || phone.LocalDescription() == nil {
					continue
				}
				f.push(t, map[string]any{"from": "p1", "type": "answer", "data": phone.LocalDescription()})
			case signalICE:
				var candidate webrtc.ICECandidateInit
				if json.Unmarshal(sig.Data, &candidate) != nil {
					continue
				}
				if !*remoteSet {
					*pending = append(*pending, candidate)
					continue
				}
				_ = phone.AddICECandidate(candidate)
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func waitOpen(t *testing.T, dc *webrtc.DataChannel) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for dc.ReadyState() != webrtc.DataChannelStateOpen {
		if time.Now().After(deadline) {
			t.Fatalf("channel %s state %s", dc.Label(), dc.ReadyState())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
