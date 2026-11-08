package audio

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"kiosk/controller/internal/audio/opus"
)

type recorder struct {
	mu      sync.Mutex
	samples []media.Sample
}

func (r *recorder) WriteSample(s media.Sample) error {
	r.mu.Lock()
	r.samples = append(r.samples, s)
	r.mu.Unlock()
	return nil
}

func (r *recorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.samples)
}

func TestCaptureFramesAnyRate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &ToneSource{Freq: 440, SampleRate: 44100, Channels: 1, ChunkDur: 7 * time.Millisecond, Duration: time.Second}
	c, err := StartCapture(ctx, src, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var left []float32
	n := 0
	for f := range c.Frames() {
		n++
		left = append(left, channel(f[:], 2, 0)...)
	}
	if n < 99 || n > 100 {
		t.Fatalf("%d frames for 1 s", n)
	}
	if p, po := goertzel(left[500:], 440, SampleRate), goertzel(left[500:], 480, SampleRate); p < 1000*po {
		t.Fatalf("tone lost: %.3g vs %.3g", p, po)
	}
}

func TestCaptureBacklogDropsOldest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := StartCapture(ctx, &ToneSource{Freq: 440, Duration: 500 * time.Millisecond}, 3)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // consumer stalls; source keeps going
	n := 0
	for range c.Frames() {
		n++
	}
	if n > 3+1 || c.Dropped() < 40 {
		t.Fatalf("kept %d frames, dropped %d", n, c.Dropped())
	}
}

func TestTrackSwitchTargetAndGain(t *testing.T) {
	tr, err := NewTrack(TrackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := StartCapture(ctx, &ToneSource{Freq: 1000, Realtime: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- tr.Run(ctx, c) }()

	a, b := &recorder{}, &recorder{}
	waitFor := func(r *recorder, n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for r.len() < n {
			if time.Now().After(deadline) {
				t.Fatalf("only %d samples", r.len())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	time.Sleep(50 * time.Millisecond) // no target yet: frames are skipped
	tr.SwitchTarget(a)
	waitFor(a, 10)
	tr.SwitchTarget(b)
	waitFor(b, 30)
	aAfter := a.len()
	time.Sleep(50 * time.Millisecond)
	if a.len() != aAfter {
		t.Fatal("old target still receiving after switch")
	}
	tr.SetGain(0)
	tr.SwitchTarget(nil)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	st := tr.Stats()
	if st.Skipped == 0 || st.Frames < 40 || st.Bytes == 0 {
		t.Fatalf("stats %+v", st)
	}

	// Decode what b received: one clean 1 kHz tone at unity gain.
	dec, err := opus.NewDecoder(SampleRate, Channels)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var left []float32
	pcm := make([]float32, FrameLen)
	for _, s := range b.samples {
		if s.Duration != FrameDuration {
			t.Fatalf("duration %v", s.Duration)
		}
		n, err := dec.DecodeFloat(s.Data, pcm)
		if err != nil || n != FrameSamples {
			t.Fatalf("decode %d %v", n, err)
		}
		left = append(left, channel(pcm, 2, 0)...)
	}
	left = left[960:]
	if p, po := goertzel(left, 1000, SampleRate), goertzel(left, 1300, SampleRate); p < 100*po {
		t.Fatalf("tone %.3g vs %.3g", p, po)
	}
}

func TestTrackGainApplied(t *testing.T) {
	tr, err := NewTrack(TrackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	dec, err := opus.NewDecoder(SampleRate, Channels)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	level := func(gain float64) float64 {
		tr.SetGain(gain)
		_ = tr.enc.Reset()
		var f Frame
		pcm := make([]float32, FrameLen)
		var out []float32
		for i := 0; i < 30; i++ {
			for j := 0; j < FrameSamples; j++ {
				v := float32(0.4 * sinAt(i*FrameSamples+j, 1000))
				f[2*j], f[2*j+1] = v, v
			}
			s, err := tr.Encode(&f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := dec.DecodeFloat(s.Data, pcm); err != nil {
				t.Fatal(err)
			}
			if i >= 10 {
				out = append(out, pcm...)
			}
		}
		return rms(out)
	}
	full, half, mute := level(1), level(0.5), level(0)
	if r := half / full; r < 0.45 || r > 0.55 {
		t.Fatalf("half gain ratio %.3f", r)
	}
	if mute > 0.01 {
		t.Fatalf("mute rms %.4f", mute)
	}
	tr.SetGain(100)
	if tr.Gain() != 4 {
		t.Fatalf("gain clamp %v", tr.Gain())
	}
}

func sinAt(n int, freq float64) float64 {
	return math.Sin(2 * math.Pi * float64(n) * freq / SampleRate)
}

// TestPionLoopback writes the Track's samples into a TrackLocalStaticSample
// on one PeerConnection and checks the other receives Opus RTP.
func TestPionLoopback(t *testing.T) {
	var se webrtc.SettingEngine
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetIncludeLoopbackCandidate(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))

	kiosk, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer kiosk.Close()
	phone, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()

	local, err := NewWebRTCTrack("p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kiosk.AddTrack(local); err != nil {
		t.Fatal(err)
	}

	type result struct {
		mime    string
		packets int
		bytes   int
	}
	got := make(chan result, 1)
	phone.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		var r result
		r.mime = remote.Codec().MimeType
		for r.packets < 20 {
			pkt, _, err := remote.ReadRTP()
			if err != nil {
				break
			}
			if len(pkt.Payload) > 0 {
				r.packets++
				r.bytes += len(pkt.Payload)
			}
		}
		got <- r
	})

	offer, err := kiosk.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gather := webrtc.GatheringCompletePromise(kiosk)
	if err := kiosk.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gather
	if err := phone.SetRemoteDescription(*kiosk.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := phone.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gather = webrtc.GatheringCompletePromise(phone)
	if err := phone.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-gather
	if err := kiosk.SetRemoteDescription(*phone.LocalDescription()); err != nil {
		t.Fatal(err)
	}

	tr, err := NewTrack(TrackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	tr.SwitchTarget(local)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := StartCapture(ctx, &ToneSource{Freq: 660, Realtime: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = tr.Run(ctx, c) }()

	select {
	case r := <-got:
		if r.mime != webrtc.MimeTypeOpus || r.packets < 20 {
			t.Fatalf("received %+v", r)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("no Opus packets received (stats %+v)", tr.Stats())
	}
}

// BenchmarkResampleEncode measures the per-frame work the pipeline adds:
// resample 10 ms of 44.1 kHz stereo to 48 kHz, gain, Opus encode.
func BenchmarkResampleEncode(b *testing.B) {
	for _, tc := range []struct {
		name       string
		rate       int
		complexity int
	}{{"44k1-c5", 44100, 5}, {"48k-c5", 48000, 5}, {"44k1-c10", 44100, 10}} {
		b.Run(tc.name, func(b *testing.B) {
			tr, err := NewTrack(TrackOptions{Complexity: tc.complexity})
			if err != nil {
				b.Fatal(err)
			}
			defer tr.Close()
			in := sine(tc.rate, 2, tc.rate, 997, 0.5)
			step := tc.rate / 100 * 2
			rs := NewResampler(tc.rate, 2)
			var pending []float32
			var f Frame
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				off := (i * step) % (len(in) - step)
				pending = rs.Process(pending, in[off:off+step])
				for len(pending) >= FrameLen {
					copy(f[:], pending[:FrameLen])
					pending = pending[:copy(pending, pending[FrameLen:])]
					if _, err := tr.Encode(&f); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkResampleOnly(b *testing.B) {
	in := sine(44100, 2, 441, 997, 0.5)
	rs := NewResampler(44100, 2)
	var out []float32
	for i := 0; i < b.N; i++ {
		out = rs.Process(out[:0], in)
	}
}
