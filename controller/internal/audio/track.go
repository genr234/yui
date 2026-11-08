package audio

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"kiosk/controller/internal/audio/opus"
)

// SampleWriter receives encoded Opus samples. *webrtc.TrackLocalStaticSample
// implements it.
type SampleWriter interface {
	WriteSample(media.Sample) error
}

// OpusCodec is the RTP codec capability of the speaker track.
var OpusCodec = webrtc.RTPCodecCapability{
	MimeType:    webrtc.MimeTypeOpus,
	ClockRate:   SampleRate,
	Channels:    Channels,
	SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1",
}

// NewWebRTCTrack creates the per-peer local track the session adds to each
// phone's PeerConnection (pc.AddTrack) when the peer is created. The Track
// then writes into whichever one belongs to the current speaker, so moving
// the speaker needs no renegotiation.
func NewWebRTCTrack(peerID string) (*webrtc.TrackLocalStaticSample, error) {
	return webrtc.NewTrackLocalStaticSample(OpusCodec, "speaker", "yui-speaker-"+peerID)
}

// TrackOptions configures the encoder. Zero values pick the defaults.
type TrackOptions struct {
	Bitrate     int              // bits/s, default 96000
	Complexity  int              // 0..10, default 5
	Application opus.Application // default AppRestrictedLowDelay (CELT only, lowest delay)
}

// Stats are cumulative counters.
type Stats struct {
	Frames      int64         // frames encoded and written
	Bytes       int64         // Opus payload bytes written
	Skipped     int64         // frames dropped because there was no target
	WriteErrors int64         // WriteSample failures
	EncodeTime  time.Duration // total time spent in gain + encode
}

// Track turns 10 ms frames into Opus media samples for the speaker phone.
// SwitchTarget and SetGain may be called from any goroutine while Run is
// going.
type Track struct {
	enc *opus.Encoder

	mu     sync.Mutex
	target SampleWriter
	reset  bool

	gainBits atomic.Uint32

	frames, bytes, skipped, writeErrs, encodeNanos atomic.Int64

	pcm [FrameLen]float32
	pkt [opus.MaxPacket]byte
}

// NewTrack creates a Track with its own Opus encoder.
func NewTrack(opts TrackOptions) (*Track, error) {
	if opts.Bitrate == 0 {
		opts.Bitrate = 96000
	}
	if opts.Complexity == 0 {
		opts.Complexity = 5
	}
	if opts.Application == 0 {
		opts.Application = opus.AppRestrictedLowDelay
	}
	enc, err := opus.NewEncoder(SampleRate, Channels, opts.Application)
	if err != nil {
		return nil, err
	}
	for _, set := range []func() error{
		func() error { return enc.SetBitrate(opts.Bitrate) },
		func() error { return enc.SetComplexity(opts.Complexity) },
		func() error { return enc.SetSignal(opus.SignalMusic) },
	} {
		if err := set(); err != nil {
			_ = enc.Close()
			return nil, err
		}
	}
	t := &Track{enc: enc}
	t.SetGain(1)
	return t, nil
}

// SwitchTarget moves the stream to w (the new speaker's track), or stops
// sending when w is nil. The encoder restarts so the new receiver's decoder
// starts from a clean state.
func (t *Track) SwitchTarget(w SampleWriter) {
	t.mu.Lock()
	if t.target != w {
		t.target = w
		t.reset = true
	}
	t.mu.Unlock()
}

// Target returns the current target.
func (t *Track) Target() SampleWriter {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.target
}

// SetGain sets the hub-side linear gain, clamped to [0, 4].
func (t *Track) SetGain(g float64) {
	if math.IsNaN(g) || g < 0 {
		g = 0
	}
	if g > 4 {
		g = 4
	}
	t.gainBits.Store(math.Float32bits(float32(g)))
}

// Gain returns the linear gain.
func (t *Track) Gain() float64 { return float64(math.Float32frombits(t.gainBits.Load())) }

// Encode applies the gain to one frame and returns the Opus sample. It is
// what Run does per frame; exposed for tests and benchmarks. Do not call it
// while Run is going (they share the encoder and scratch buffers).
func (t *Track) Encode(f *Frame) (media.Sample, error) {
	g := math.Float32frombits(t.gainBits.Load())
	for i, v := range f {
		v *= g
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		t.pcm[i] = v
	}
	n, err := t.enc.EncodeFloat(t.pcm[:], t.pkt[:])
	if err != nil {
		return media.Sample{}, err
	}
	return media.Sample{Data: append([]byte(nil), t.pkt[:n]...), Duration: FrameDuration}, nil
}

// Run encodes frames from c and writes them to the current target until
// ctx is done or the capture ends. It returns the capture's error, if any.
func (t *Track) Run(ctx context.Context, c *Capture) error {
	for {
		var f *Frame
		var ok bool
		select {
		case <-ctx.Done():
			return nil
		case f, ok = <-c.Frames():
			if !ok {
				return c.Err()
			}
		}
		t.mu.Lock()
		target, reset := t.target, t.reset
		t.reset = false
		t.mu.Unlock()
		if target == nil {
			t.skipped.Add(1)
			continue
		}
		if reset {
			_ = t.enc.Reset()
		}
		start := time.Now()
		sample, err := t.Encode(f)
		t.encodeNanos.Add(int64(time.Since(start)))
		if err != nil {
			return err
		}
		if err := target.WriteSample(sample); err != nil {
			t.writeErrs.Add(1)
			continue
		}
		t.frames.Add(1)
		t.bytes.Add(int64(len(sample.Data)))
	}
}

// Stats returns the counters.
func (t *Track) Stats() Stats {
	return Stats{
		Frames:      t.frames.Load(),
		Bytes:       t.bytes.Load(),
		Skipped:     t.skipped.Load(),
		WriteErrors: t.writeErrs.Load(),
		EncodeTime:  time.Duration(t.encodeNanos.Load()),
	}
}

// Close releases the encoder.
func (t *Track) Close() error { return t.enc.Close() }
