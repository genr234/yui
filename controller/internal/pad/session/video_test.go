package session

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"kiosk/controller/internal/pad/protocol"
)

func mlines(sdp string) []string {
	var out []string
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "m=") {
			out = append(out, strings.Fields(strings.TrimPrefix(line, "m="))[0])
		}
	}
	return out
}

func noHooks() peerHooks {
	return peerHooks{
		onControl:     func(string, int, []byte) {},
		onInput:       func(string, int, []byte) {},
		onICE:         func(string, int, webrtc.ICECandidateInit) {},
		onICEFailed:   func(string, int) {},
		onICEUp:       func(string, int) {},
		onControlOpen: func(string, int) {},
	}
}

func offerOf(t *testing.T, audio, video bool) string {
	t.Helper()
	p, err := openPeer(newPeerAPI(), nil, "p1", 1, audio, video, noHooks())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	desc, err := p.offer()
	if err != nil {
		t.Fatal(err)
	}
	return desc.SDP
}

func TestOfferHasVideoOnlyWhenAsked(t *testing.T) {
	// Without Config.Video the offer is what it always was.
	if got := strings.Join(mlines(offerOf(t, false, false)), ","); got != "application" {
		t.Fatalf("no media: %s", got)
	}
	plain := offerOf(t, true, false)
	if got := strings.Join(mlines(plain), ","); got != "audio,application" {
		t.Fatalf("audio only: %s", got)
	}
	if strings.Contains(strings.ToLower(plain), "h264") || strings.Contains(plain, "m=video") {
		t.Fatal("video leaked into an offer with Video off")
	}

	with := offerOf(t, true, true)
	if got := strings.Join(mlines(with), ","); got != "audio,video,application" {
		t.Fatalf("with video: %s", got)
	}
	if !strings.Contains(with, "H264/90000") || !strings.Contains(with, "profile-level-id=42e01f") || !strings.Contains(with, "packetization-mode=1") {
		t.Fatalf("video codec line missing:\n%s", with)
	}
	if !strings.Contains(with, "a=sendonly") && !strings.Contains(with, "a=sendrecv") {
		t.Fatal("video track is not sending")
	}
}

func TestVideoTrackTouchAndCaps(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t)
	mgr.cfg.Video = true
	mgr.cfg.PingInterval = time.Hour

	var evMu sync.Mutex
	var peerEvents []bool
	mgr.SetPeerHandler(func(id string, up bool) {
		if id != "p1" {
			return
		}
		evMu.Lock()
		peerEvents = append(peerEvents, up)
		evMu.Unlock()
	})
	var touchMu sync.Mutex
	var touches []protocol.TouchState
	mgr.SetTouchHandler(func(id string, ts protocol.TouchState) {
		touchMu.Lock()
		touches = append(touches, ts)
		touchMu.Unlock()
	})
	seenTouches := func() []protocol.TouchState {
		touchMu.Lock()
		defer touchMu.Unlock()
		return append([]protocol.TouchState(nil), touches...)
	}
	events := func() []bool {
		evMu.Lock()
		defer evMu.Unlock()
		return append([]bool(nil), peerEvents...)
	}

	if _, ok := mgr.VideoWriter("p1"); ok {
		t.Fatal("writer before any peer")
	}
	if _, ok := mgr.Caps("p1"); ok {
		t.Fatal("caps before hello")
	}

	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	phone, err := newPeerAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = phone.Close() }()

	var rtpIn atomic.Int64
	var mime atomic.Value
	phone.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		mime.Store(remote.Codec().MimeType)
		for {
			if _, _, err := remote.ReadRTP(); err != nil {
				return
			}
			rtpIn.Add(1)
		}
	})
	controlCh := make(chan *webrtc.DataChannel, 1)
	inputCh := make(chan *webrtc.DataChannel, 1)
	phone.OnDataChannel(func(dc *webrtc.DataChannel) {
		switch dc.Label() {
		case "control":
			controlCh <- dc
		case "input":
			inputCh <- dc
		}
	})
	remoteSet := false
	var pending []webrtc.ICECandidateInit
	phone.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			f.push(t, map[string]any{"from": "p1", "type": "ice", "data": c.ToJSON()})
		}
	})
	stopFwd := make(chan struct{})
	defer close(stopFwd)
	go forwardSignals(t, f, phone, &remoteSet, &pending, stopFwd)

	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 1, true, true)})

	var control, input *webrtc.DataChannel
	select {
	case control = <-controlCh:
	case <-time.After(8 * time.Second):
		t.Fatal("no control channel")
	}
	select {
	case input = <-inputCh:
	case <-time.After(3 * time.Second):
		t.Fatal("no input channel")
	}
	waitOpen(t, control)
	waitOpen(t, input)
	waitFor(t, "peer up event", func() bool { ev := events(); return len(ev) > 0 && ev[len(ev)-1] })

	hello := protocol.Hello{T: "hello", V: 1,
		Caps:   protocol.Caps{Motion: true, Video: true},
		Screen: protocol.Screen{W: 844, H: 390, Orientation: "landscape"}}
	if err := control.SendText(string(mustJSON(hello))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "caps stored", func() bool { c, ok := mgr.Caps("p1"); return ok && c.Video && c.Motion })

	// The roster carries caps and, once ICE has nominated a pair, relayed.
	waitFor(t, "roster caps and relayed", func() bool {
		p := mgr.Snapshot().Players
		return len(p) == 1 && p[0].Caps != nil && p[0].Caps.Video && p[0].Relayed != nil && !*p[0].Relayed
	})
	if relayed, ok := mgr.Relayed("p1"); !ok || relayed {
		t.Fatalf("relayed = %v %v (both ends are local)", relayed, ok)
	}

	// Video reaches the phone through the writer.
	w, ok := mgr.VideoWriter("p1")
	if !ok {
		t.Fatal("no writer on a live peer")
	}
	var seq uint16
	deadline := time.Now().Add(5 * time.Second)
	for rtpIn.Load() < 5 && time.Now().Before(deadline) {
		seq++
		err := w.WriteRTP(&rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: seq, Timestamp: uint32(seq) * 3000, Marker: true},
			Payload: []byte{0x65, 1, 2, 3},
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rtpIn.Load() < 5 {
		t.Fatalf("phone received %d RTP packets", rtpIn.Load())
	}
	if m, _ := mime.Load().(string); !strings.EqualFold(m, webrtc.MimeTypeH264) {
		t.Fatalf("track mime %q", m)
	}

	// Touch frames: one delivery each, stale seq dropped.
	send := func(ts protocol.TouchState) {
		if err := input.Send(protocol.EncodeTouch(ts)); err != nil {
			t.Fatal(err)
		}
	}
	down := func(seq uint16, x uint16) protocol.TouchState {
		return protocol.TouchState{Seq: seq, TMs: uint32(seq), Points: [2]protocol.TouchPoint{{Down: true, X: x, Y: 9}}}
	}
	send(down(10, 100))
	waitFor(t, "first touch", func() bool { return len(seenTouches()) == 1 })
	send(down(9, 200)) // stale
	send(down(11, 300))
	waitFor(t, "second touch", func() bool { return len(seenTouches()) >= 2 })
	got := seenTouches()
	if len(got) != 2 || got[0].Points[0].X != 100 || got[1].Points[0].X != 300 || got[0].Seq != 10 || got[1].Seq != 11 {
		t.Fatalf("touches %+v", got)
	}
	// A state frame still works beside touch frames, with its own seq.
	if err := input.Send(protocol.EncodeInput(protocol.InputState{Seq: 1, Buttons: protocol.ButtonSouth})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "state frame", func() bool { s, ok := mgr.LatestInput("p1"); return ok && s.Buttons == protocol.ButtonSouth })

	// Silence with a finger down: the kiosk lifts it after ~250 ms.
	waitFor(t, "kiosk release", func() bool {
		g := seenTouches()
		last := g[len(g)-1]
		return len(g) >= 3 && !last.Points[0].Down && !last.Points[1].Down
	})
	// A normal release is not followed by another synthetic one.
	send(down(12, 1))
	send(protocol.TouchState{Seq: 13})
	time.Sleep(450 * time.Millisecond)
	if n := len(seenTouches()); n != 5 {
		t.Fatalf("expected 5 frames (incl. 1 synthetic), saw %d: %+v", n, seenTouches())
	}

	// Cable fallback: no video, no relay answer.
	mgr.mu.Lock()
	peer := mgr.peers["p1"]
	mgr.mu.Unlock()
	peer.fallback.Store(true)
	if _, ok := mgr.VideoWriter("p1"); ok {
		t.Fatal("writer in cable fallback")
	}
	if _, ok := mgr.Relayed("p1"); ok {
		t.Fatal("relayed answered in cable fallback")
	}
	peer.fallback.Store(false)

	// The player leaves: the peer handler hears about it.
	f.push(t, map[string]any{"type": "player_left", "player": playerFixture("p1", 1, true, false)})
	waitFor(t, "peer down event", func() bool { ev := events(); return len(ev) > 0 && !ev[len(ev)-1] })
	if _, ok := mgr.VideoWriter("p1"); ok {
		t.Fatal("writer after the player left")
	}
}

func TestNoVideoTrackWhenOff(t *testing.T) {
	f := newFake(t)
	mgr := f.manager(t) // Config.Video false
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)
	phone, err := newPeerAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = phone.Close() }()
	controlCh := make(chan *webrtc.DataChannel, 1)
	phone.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() == "control" {
			controlCh <- dc
		}
	})
	remoteSet := false
	var pending []webrtc.ICECandidateInit
	phone.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			f.push(t, map[string]any{"from": "p1", "type": "ice", "data": c.ToJSON()})
		}
	})
	stopFwd := make(chan struct{})
	defer close(stopFwd)
	go forwardSignals(t, f, phone, &remoteSet, &pending, stopFwd)
	f.push(t, map[string]any{"type": "player_joined", "player": playerFixture("p1", 1, true, true)})
	select {
	case dc := <-controlCh:
		waitOpen(t, dc)
	case <-time.After(8 * time.Second):
		t.Fatal("no control channel")
	}
	if _, ok := mgr.VideoWriter("p1"); ok {
		t.Fatal("writer without Config.Video")
	}
	for _, tr := range phone.GetTransceivers() {
		if tr.Kind() == webrtc.RTPCodecTypeVideo {
			t.Fatal("offer carried a video m-line with Video off")
		}
	}
}
