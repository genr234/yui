package session

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"kiosk/controller/internal/audio"
	"kiosk/controller/internal/pad/protocol"
)

// toneAudio is an Audio whose capture is a real-time tone. pids records the
// process each capture asked for.
func toneAudio(t *testing.T) (*Audio, *pidLog) {
	t.Helper()
	pids := &pidLog{}
	a := NewAudio(AudioConfig{
		BrowserPID: func(context.Context) (int, error) { return 4242, nil },
		NewSource: func(pid int) audio.Source {
			pids.add(pid)
			return &audio.ToneSource{Freq: 440, Realtime: true}
		},
	})
	t.Cleanup(func() { _ = a.Close() })
	return a, pids
}

type pidLog struct {
	mu   sync.Mutex
	pids []int
}

func (p *pidLog) add(pid int) {
	p.mu.Lock()
	p.pids = append(p.pids, pid)
	p.mu.Unlock()
}

func (p *pidLog) last() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pids) == 0 {
		return -1
	}
	return p.pids[len(p.pids)-1]
}

type countWriter struct{ n atomic.Int64 }

func (w *countWriter) WriteSample(media.Sample) error {
	w.n.Add(1)
	return nil
}

func TestAudioCapturesOnlyWithATarget(t *testing.T) {
	a, pids := toneAudio(t)
	if a.Status().Capturing {
		t.Fatal("capturing before any target")
	}
	w := &countWriter{}
	a.SetTarget(w)
	waitFor(t, "samples", func() bool { return w.n.Load() > 5 })
	if st := a.Status(); !st.Capturing || st.Process != "browser" || st.PID != 4242 || st.Source != RouteAuto {
		t.Fatalf("status %+v", st)
	}

	// A native runtime registers: the capture restarts on its process.
	a.SetCapturePID(777)
	waitFor(t, "native capture", func() bool { return pids.last() == 777 && a.Status().PID == 777 })
	if st := a.Status(); st.Process != "native" {
		t.Fatalf("status %+v", st)
	}
	// The app asks for its own sound instead.
	if err := a.SetRoute(RouteApp); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "browser again", func() bool { return a.Status().PID == 4242 })
	if err := a.SetRoute("speakers"); err == nil {
		t.Fatal("bad route accepted")
	}
	if err := a.SetRoute(RouteNative); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "native route", func() bool { return a.Status().PID == 777 })
	a.ResetCapture()
	waitFor(t, "reset to browser", func() bool { return a.Status().PID == 4242 })

	if got := a.SetVolume(3); got != 1 {
		t.Fatalf("volume clamp %v", got)
	}
	if got := a.SetVolume(0.25); got != 0.25 || a.Volume() != 0.25 {
		t.Fatalf("volume %v", got)
	}

	a.SetTarget(nil)
	if a.Status().Capturing {
		t.Fatal("still capturing without a target")
	}
	n := w.n.Load()
	time.Sleep(60 * time.Millisecond)
	if w.n.Load() != n {
		t.Fatal("samples written after the target was cleared")
	}
}

// audioPhone is a fake pad: a pion peer that answers the kiosk's offers,
// says hello, counts Opus RTP and keeps the control messages it gets.
type audioPhone struct {
	id      string
	pc      *webrtc.PeerConnection
	packets atomic.Int64
	offers  atomic.Int64
	mime    atomic.Value

	mu        sync.Mutex
	control   *webrtc.DataChannel
	remoteSet bool
	pending   []webrtc.ICECandidateInit
	msgs      []map[string]any
}

func newAudioPhone(t *testing.T, f *fakeRails, id string) *audioPhone {
	t.Helper()
	pc, err := newPeerAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	p := &audioPhone{id: id, pc: pc}
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		p.mime.Store(remote.Codec().MimeType)
		for {
			pkt, _, err := remote.ReadRTP()
			if err != nil {
				return
			}
			if len(pkt.Payload) > 0 {
				p.packets.Add(1)
			}
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "control" {
			return
		}
		dc.OnOpen(func() {
			hello := protocol.Hello{T: "hello", V: 1, Screen: protocol.Screen{W: 390, H: 844, Orientation: "portrait"}}
			_ = dc.SendText(string(mustJSON(hello)))
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			var obj map[string]any
			if json.Unmarshal(msg.Data, &obj) != nil {
				return
			}
			p.mu.Lock()
			p.msgs = append(p.msgs, obj)
			p.mu.Unlock()
		})
		p.mu.Lock()
		p.control = dc
		p.mu.Unlock()
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			f.push(t, map[string]any{"from": id, "type": "ice", "data": c.ToJSON()})
		}
	})
	return p
}

func (p *audioPhone) send(t *testing.T, msg any) {
	t.Helper()
	p.mu.Lock()
	dc := p.control
	p.mu.Unlock()
	if dc == nil {
		t.Fatalf("%s has no control channel", p.id)
	}
	if err := dc.SendText(string(mustJSON(msg))); err != nil {
		t.Fatal(err)
	}
}

// lastPlayer is the newest welcome or player message's player object.
func (p *audioPhone) lastPlayer() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.msgs) - 1; i >= 0; i-- {
		if t := p.msgs[i]["t"]; t == "welcome" || t == "player" {
			player, _ := p.msgs[i]["player"].(map[string]any)
			return player
		}
	}
	return nil
}

func (p *audioPhone) volumeSeen() (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.msgs) - 1; i >= 0; i-- {
		if p.msgs[i]["t"] == "system" && p.msgs[i]["action"] == "volume" {
			v, ok := p.msgs[i]["value"].(float64)
			return v, ok
		}
	}
	return 0, false
}

func (p *audioPhone) handle(t *testing.T, f *fakeRails, sig savedSignal) {
	switch sig.Type {
	case signalOffer:
		p.offers.Add(1)
		var desc webrtc.SessionDescription
		if json.Unmarshal(sig.Data, &desc) != nil || p.pc.SetRemoteDescription(desc) != nil {
			return
		}
		p.mu.Lock()
		p.remoteSet = true
		pending := p.pending
		p.pending = nil
		p.mu.Unlock()
		for _, c := range pending {
			_ = p.pc.AddICECandidate(c)
		}
		answer, err := p.pc.CreateAnswer(nil)
		if err != nil || p.pc.SetLocalDescription(answer) != nil {
			return
		}
		f.push(t, map[string]any{"from": p.id, "type": "answer", "data": p.pc.LocalDescription()})
	case signalICE:
		var c webrtc.ICECandidateInit
		if json.Unmarshal(sig.Data, &c) != nil {
			return
		}
		p.mu.Lock()
		if !p.remoteSet {
			p.pending = append(p.pending, c)
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		_ = p.pc.AddICECandidate(c)
	}
}

// routeSignals hands each kiosk offer/ice to the phone it is addressed to.
func routeSignals(t *testing.T, f *fakeRails, phones map[string]*audioPhone, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		f.mu.Lock()
		var kept, batch []savedSignal
		for _, sig := range f.sigs {
			if (sig.Type == signalOffer || sig.Type == signalICE) && phones[sig.To] != nil {
				batch = append(batch, sig)
			} else {
				kept = append(kept, sig)
			}
		}
		f.sigs = kept
		f.mu.Unlock()
		for _, sig := range batch {
			phones[sig.To].handle(t, f, sig)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func rosterJSON(id string, slot int, host, speaker, connected bool) Player {
	return Player{Player: playerFixture(id, slot, host, connected), Speaker: speaker}
}

// waitPackets waits until phone has received more than n packets past from.
func waitPackets(t *testing.T, p *audioPhone, from int64, n int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for p.packets.Load()-from <= n {
		if time.Now().After(deadline) {
			t.Fatalf("%s got %d packets, want > %d", p.id, p.packets.Load()-from, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertQuiet checks phone gets (almost) nothing over a while: a couple of
// packets already in flight at a switch are fine.
func assertQuiet(t *testing.T, p *audioPhone, d time.Duration) {
	t.Helper()
	time.Sleep(40 * time.Millisecond)
	before := p.packets.Load()
	time.Sleep(d)
	if got := p.packets.Load() - before; got > 2 {
		t.Fatalf("%s still received %d packets", p.id, got)
	}
}

// TestSpeakerAudioFollowsTheSpeaker runs the kiosk manager against two pion
// phones: only the speaker receives Opus RTP, be_speaker moves the sound
// with no new offer, and when the speaker leaves the host takes over.
func TestSpeakerAudioFollowsTheSpeaker(t *testing.T) {
	f := newFake(t)
	a, _ := toneAudio(t)
	mgr := f.manager(t)
	mgr.cfg.Audio = a
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	host := newAudioPhone(t, f, "p1")
	guest := newAudioPhone(t, f, "p2")
	stopRoute := make(chan struct{})
	defer close(stopRoute)
	go routeSignals(t, f, map[string]*audioPhone{"p1": host, "p2": guest}, stopRoute)

	f.push(t, map[string]any{"type": "player_joined", "player": rosterJSON("p1", 1, true, true, true)})
	f.push(t, map[string]any{"type": "player_joined", "player": rosterJSON("p2", 2, false, false, true)})

	waitPackets(t, host, 0, 20)
	if mime := host.mime.Load(); mime != webrtc.MimeTypeOpus {
		t.Fatalf("host track %v", mime)
	}
	waitFor(t, "guest ready", func() bool { return guest.lastPlayer() != nil })
	if guest.packets.Load() != 0 {
		t.Fatalf("guest received %d packets before being the speaker", guest.packets.Load())
	}
	waitFor(t, "host welcome", func() bool { p := host.lastPlayer(); return p != nil && p["speaker"] == true })
	if p := guest.lastPlayer(); p["speaker"] != false {
		t.Fatalf("guest welcome %+v", p)
	}
	if v, ok := host.volumeSeen(); !ok || v != 1 {
		t.Fatalf("host volume %v %v", v, ok)
	}
	if mgr.AudioTarget() != "p1" {
		t.Fatalf("target %q", mgr.AudioTarget())
	}
	offers := host.offers.Load() + guest.offers.Load()

	// Only the phone playing the sound may change the volume.
	guest.send(t, protocol.System{T: "system", Action: "volume", Value: 0.1})
	host.send(t, protocol.System{T: "system", Action: "volume", Value: 0.5})
	waitFor(t, "host volume", func() bool { return a.Volume() == 0.5 })

	// The guest taps "Be the speaker".
	guest.send(t, protocol.System{T: "system", Action: "be_speaker"})
	waitFor(t, "speaker moved", func() bool { return mgr.AudioTarget() == "p2" })
	if !f.hasCall("POST /api/kiosk/play_sessions/sess-1/players/p2/speaker") {
		t.Fatal("rails was not told about the new speaker")
	}
	waitPackets(t, guest, 0, 20)
	assertQuiet(t, host, 200*time.Millisecond)
	if got := host.offers.Load() + guest.offers.Load(); got != offers {
		t.Fatalf("speaker switch renegotiated: %d offers, had %d", got, offers)
	}
	waitFor(t, "player messages", func() bool {
		g, h := guest.lastPlayer(), host.lastPlayer()
		return g["speaker"] == true && h["speaker"] == false
	})
	if v, ok := guest.volumeSeen(); !ok || v != 0.5 {
		t.Fatalf("new speaker volume %v %v", v, ok)
	}
	snap := mgr.Snapshot()
	if len(snap.Players) != 2 || snap.Players[0].Speaker || !snap.Players[1].Speaker {
		t.Fatalf("snapshot %+v", snap.Players)
	}
	raw, _ := json.Marshal(snap.Players[1])
	var flat map[string]any
	_ = json.Unmarshal(raw, &flat)
	if flat["speaker"] != true || flat["id"] != "p2" || flat["host"] != false {
		t.Fatalf("player json %s", raw)
	}

	// Rails confirms the move (in either order); nothing changes.
	f.push(t, map[string]any{"type": "player_updated", "player": rosterJSON("p2", 2, false, true, true)})
	f.push(t, map[string]any{"type": "player_updated", "player": rosterJSON("p1", 1, true, false, true)})
	time.Sleep(100 * time.Millisecond)
	if mgr.Speaker() != "p2" || mgr.AudioTarget() != "p2" {
		t.Fatalf("speaker %q target %q", mgr.Speaker(), mgr.AudioTarget())
	}

	// The speaker's phone leaves: the host plays the sound again.
	from := host.packets.Load()
	f.push(t, map[string]any{"type": "player_left", "player": rosterJSON("p2", 2, false, true, false)})
	waitFor(t, "host takes over", func() bool { return mgr.AudioTarget() == "p1" })
	waitPackets(t, host, from, 20)
	f.push(t, map[string]any{"type": "player_updated", "player": rosterJSON("p2", 2, false, false, false)})
	f.push(t, map[string]any{"type": "player_updated", "player": rosterJSON("p1", 1, true, true, true)})
	waitFor(t, "role on host", func() bool { return mgr.Speaker() == "p1" })

	// Nobody left to play it: the capture stops.
	f.push(t, map[string]any{"type": "player_left", "player": rosterJSON("p1", 1, true, true, false)})
	waitFor(t, "capture stopped", func() bool { return mgr.AudioTarget() == "" && !a.Status().Capturing })
}
