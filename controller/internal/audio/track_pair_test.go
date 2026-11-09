package audio

import (
	"context"
	"testing"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"

	"kiosk/controller/internal/audio/opus"
)

// decodeMono decodes mono Opus samples to PCM, dropping the first 20 ms.
func decodeMono(t *testing.T, samples []media.Sample) []float32 {
	t.Helper()
	dec, err := opus.NewDecoder(SampleRate, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var out []float32
	pcm := make([]float32, FrameSamples)
	for _, s := range samples {
		n, err := dec.DecodeFloat(s.Data, pcm)
		if err != nil || n != FrameSamples {
			t.Fatalf("decode %d %v", n, err)
		}
		out = append(out, pcm...)
	}
	return out[960:]
}

func TestTrackStereoPairSplitsChannels(t *testing.T) {
	tr, err := NewTrack(TrackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	left, right, whole := &recorder{}, &recorder{}, &recorder{}
	if err := tr.SwitchPair(left, right); err != nil {
		t.Fatal(err)
	}
	if l, r := tr.Pair(); l != left || r != right {
		t.Fatal("pair not set")
	}
	run := func() {
		t.Helper()
		c, err := StartCapture(context.Background(), &ToneSource{Freq: 1000, Duration: 600 * time.Millisecond}, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Run(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	run() // ToneSource: left is 1000 Hz, right is 1500 Hz
	if left.len() < 55 || right.len() != left.len() {
		t.Fatalf("left %d right %d packets", left.len(), right.len())
	}
	l := decodeMono(t, left.samples)
	r := decodeMono(t, right.samples)
	if p, po := goertzel(l, 1000, SampleRate), goertzel(l, 1500, SampleRate); p < 100*po {
		t.Fatalf("left channel: 1 kHz %.3g vs 1.5 kHz %.3g", p, po)
	}
	if p, po := goertzel(r, 1500, SampleRate), goertzel(r, 1000, SampleRate); p < 100*po {
		t.Fatalf("right channel: 1.5 kHz %.3g vs 1 kHz %.3g", p, po)
	}

	// Leaving the pair goes back to one stereo stream with both tones.
	tr.SwitchTarget(whole)
	if l, r := tr.Pair(); l != nil || r != nil {
		t.Fatal("pair still set")
	}
	before := left.len()
	run()
	if left.len() != before || whole.len() < 55 {
		t.Fatalf("after leaving the pair: left %d (was %d), whole %d", left.len(), before, whole.len())
	}
	dec, err := opus.NewDecoder(SampleRate, Channels)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var lw []float32
	pcm := make([]float32, FrameLen)
	for _, s := range whole.samples {
		if _, err := dec.DecodeFloat(s.Data, pcm); err != nil {
			t.Fatal(err)
		}
		lw = append(lw, channel(pcm, 2, 0)...)
	}
	lw = lw[960:]
	if p, po := goertzel(lw, 1000, SampleRate), goertzel(lw, 1500, SampleRate); p < 100*po {
		t.Fatalf("full mix left: %.3g vs %.3g", p, po)
	}

	// A half pair stops sending.
	if err := tr.SwitchPair(left, nil); err != nil {
		t.Fatal(err)
	}
	if tr.Target() != nil {
		t.Fatal("half pair should stop")
	}
}
