package audio

import (
	"context"
	"math"
	"sync"
	"time"
)

// Source is a capture backend. Start begins capture and returns a channel
// of PCM chunks in the source's native format. The channel is closed when
// ctx is cancelled or the source stops on its own; Err then reports why
// (nil for a cancel or a clean end). Start may be called once.
type Source interface {
	Name() string
	Start(ctx context.Context) (<-chan Chunk, error)
	Err() error
}

// srcErr is the Err half of a Source.
type srcErr struct {
	mu  sync.Mutex
	err error
}

func (s *srcErr) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// Err returns the error that stopped the source, if any.
func (s *srcErr) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// ToneSource is a test/dev source producing a sine wave.
type ToneSource struct {
	srcErr
	Freq       float64 // Hz; per channel c the tone is Freq*(1+c/2) so channels are distinguishable
	Amplitude  float64 // 0..1, default 0.5
	SampleRate int     // default 48000
	Channels   int     // default 2
	ChunkDur   time.Duration
	// Realtime paces chunks with a ticker, like a real device. Otherwise
	// chunks are produced as fast as they are consumed.
	Realtime bool
	// Duration stops the source after this much audio (0 = until cancelled).
	Duration time.Duration
}

func (s *ToneSource) Name() string { return "tone" }

func (s *ToneSource) Start(ctx context.Context) (<-chan Chunk, error) {
	rate, ch, amp, chunk := s.SampleRate, s.Channels, s.Amplitude, s.ChunkDur
	if rate == 0 {
		rate = SampleRate
	}
	if ch == 0 {
		ch = Channels
	}
	if amp == 0 {
		amp = 0.5
	}
	if chunk == 0 {
		chunk = FrameDuration
	}
	perChunk := int(int64(rate) * int64(chunk) / int64(time.Second))
	if perChunk < 1 {
		perChunk = 1
	}
	total := int64(-1)
	if s.Duration > 0 {
		total = int64(rate) * int64(s.Duration) / int64(time.Second)
	}
	out := make(chan Chunk, 4)
	go func() {
		defer close(out)
		var tick *time.Ticker
		if s.Realtime {
			tick = time.NewTicker(chunk)
			defer tick.Stop()
		}
		var n int64
		for total < 0 || n < total {
			count := perChunk
			if total >= 0 && int64(count) > total-n {
				count = int(total - n)
			}
			buf := make([]float32, count*ch)
			for i := 0; i < count; i++ {
				t := float64(n+int64(i)) / float64(rate)
				for c := 0; c < ch; c++ {
					f := s.Freq * (1 + float64(c)/2)
					buf[i*ch+c] = float32(amp * math.Sin(2*math.Pi*f*t))
				}
			}
			n += int64(count)
			if tick != nil {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- Chunk{SampleRate: rate, Channels: ch, Samples: buf}:
			}
		}
	}()
	return out, nil
}

// SliceSource replays fixed PCM (for tests or a decoded file), split into
// chunks of ChunkLen sample frames.
type SliceSource struct {
	srcErr
	SampleRate int
	Channels   int
	Samples    []float32
	ChunkLen   int
}

func (s *SliceSource) Name() string { return "slice" }

func (s *SliceSource) Start(ctx context.Context) (<-chan Chunk, error) {
	if err := (Format{SampleRate: s.SampleRate, Channels: s.Channels, Sample: F32LE}).Validate(); err != nil {
		return nil, err
	}
	step := s.ChunkLen
	if step <= 0 {
		step = s.SampleRate / 100
	}
	step *= s.Channels
	out := make(chan Chunk, 4)
	go func() {
		defer close(out)
		for i := 0; i < len(s.Samples); i += step {
			end := i + step
			if end > len(s.Samples) {
				end = len(s.Samples) - (len(s.Samples)-i)%s.Channels
			}
			buf := append([]float32(nil), s.Samples[i:end]...)
			select {
			case <-ctx.Done():
				return
			case out <- Chunk{SampleRate: s.SampleRate, Channels: s.Channels, Samples: buf}:
			}
		}
	}()
	return out, nil
}
