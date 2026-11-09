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
	enc  *opus.Encoder
	opts TrackOptions

	mu     sync.Mutex
	target SampleWriter
	reset  bool

	// Stereo pair (SwitchPair): the capture's left and right channels are
	// encoded as two mono streams, one per phone. The mono encoders exist
	// only after the first pair; the single-stream path above never uses
	// them.
	pairL, pairR SampleWriter
	monoL, monoR *opus.Encoder

	gainBits atomic.Uint32

	frames, bytes, skipped, writeErrs, encodeNanos atomic.Int64

	pcm [FrameLen]float32
	pkt [opus.MaxPacket]byte

	monoPCM [2][FrameSamples]float32
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
	enc, err := newEncoder(opts, Channels)
	if err != nil {
		return nil, err
	}
	t := &Track{enc: enc, opts: opts}
	t.SetGain(1)
	return t, nil
}

func newEncoder(opts TrackOptions, channels int) (*opus.Encoder, error) {
	enc, err := opus.NewEncoder(SampleRate, channels, opts.Application)
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
	return enc, nil
}

// SwitchTarget moves the stream to w (the new speaker's track), or stops
// sending when w is nil. The encoder restarts so the new receiver's decoder
// starts from a clean state.
//
// It also ends a stereo pair: the full stereo mix goes to w.
func (t *Track) SwitchTarget(w SampleWriter) {
	t.mu.Lock()
	if t.target != w || t.pairL != nil || t.pairR != nil {
		t.target = w
		t.pairL, t.pairR = nil, nil
		t.reset = true
	}
	t.mu.Unlock()
}

// SwitchPair sends the left channel to l and the right channel to r, each
// as its own mono Opus stream, and stops the single stream. With a nil l or
// r (or both) it stops sending. SwitchTarget goes back to one stream.
// Needs a second and third encoder, created on the first pair.
func (t *Track) SwitchPair(l, r SampleWriter) error {
	if l == nil || r == nil {
		t.SwitchTarget(nil)
		return nil
	}
	t.mu.Lock()
	have := t.monoL != nil
	t.mu.Unlock()
	if !have {
		el, err := newEncoder(t.opts, 1)
		if err != nil {
			return err
		}
		er, err := newEncoder(t.opts, 1)
		if err != nil {
			_ = el.Close()
			return err
		}
		t.mu.Lock()
		if t.monoL == nil {
			t.monoL, t.monoR = el, er
			el, er = nil, nil
		}
		t.mu.Unlock()
		if el != nil {
			_ = el.Close()
			_ = er.Close()
		}
	}
	t.mu.Lock()
	if t.pairL != l || t.pairR != r || t.target != nil {
		t.target = nil
		t.pairL, t.pairR = l, r
		t.reset = true
	}
	t.mu.Unlock()
	return nil
}

// Pair returns the stereo pair's writers (nil, nil when not paired).
func (t *Track) Pair() (SampleWriter, SampleWriter) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pairL, t.pairR
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

// encodePair applies the gain and encodes the left and right channels of one
// frame as two mono packets. Only Run calls it.
func (t *Track) encodePair(f *Frame, el, er *opus.Encoder) (media.Sample, media.Sample, error) {
	g := math.Float32frombits(t.gainBits.Load())
	for c := 0; c < 2; c++ {
		for i := 0; i < FrameSamples; i++ {
			v := f[i*2+c] * g
			if v > 1 {
				v = 1
			} else if v < -1 {
				v = -1
			}
			t.monoPCM[c][i] = v
		}
	}
	var out [2]media.Sample
	for c, enc := range []*opus.Encoder{el, er} {
		n, err := enc.EncodeFloat(t.monoPCM[c][:], t.pkt[:])
		if err != nil {
			return media.Sample{}, media.Sample{}, err
		}
		out[c] = media.Sample{Data: append([]byte(nil), t.pkt[:n]...), Duration: FrameDuration}
	}
	return out[0], out[1], nil
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
		pl, pr, ml, mr := t.pairL, t.pairR, t.monoL, t.monoR
		t.reset = false
		t.mu.Unlock()
		if pl != nil && pr != nil && ml != nil {
			if reset {
				_ = ml.Reset()
				_ = mr.Reset()
			}
			start := time.Now()
			ls, rs, err := t.encodePair(f, ml, mr)
			t.encodeNanos.Add(int64(time.Since(start)))
			if err != nil {
				return err
			}
			for _, w := range []struct {
				to SampleWriter
				s  media.Sample
			}{{pl, ls}, {pr, rs}} {
				if err := w.to.WriteSample(w.s); err != nil {
					t.writeErrs.Add(1)
					continue
				}
				t.frames.Add(1)
				t.bytes.Add(int64(len(w.s.Data)))
			}
			continue
		}
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
func (t *Track) Close() error {
	t.mu.Lock()
	ml, mr := t.monoL, t.monoR
	t.monoL, t.monoR = nil, nil
	t.pairL, t.pairR = nil, nil
	t.mu.Unlock()
	if ml != nil {
		_ = ml.Close()
		_ = mr.Close()
	}
	return t.enc.Close()
}
