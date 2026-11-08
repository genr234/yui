package audio

import (
	"context"
	"sync/atomic"
)

// DefaultMaxBacklog bounds how many 10 ms frames may wait for the encoder.
// A source that bursts (or a stalled consumer) loses its oldest frames
// instead of building up delay.
const DefaultMaxBacklog = 6

// Capture is a running source converted to 48 kHz stereo 10 ms frames.
type Capture struct {
	src     Source
	frames  chan *Frame
	dropped atomic.Int64
}

// StartCapture starts src and converts its chunks (any rate or channel count,
// and the format may change between chunks) into Frames. maxBacklog <= 0
// uses DefaultMaxBacklog. The frame channel closes when the source ends.
func StartCapture(ctx context.Context, src Source, maxBacklog int) (*Capture, error) {
	if maxBacklog <= 0 {
		maxBacklog = DefaultMaxBacklog
	}
	chunks, err := src.Start(ctx)
	if err != nil {
		return nil, err
	}
	c := &Capture{src: src, frames: make(chan *Frame, maxBacklog)}
	go c.run(ctx, chunks)
	return c, nil
}

// Frames returns the frame channel.
func (c *Capture) Frames() <-chan *Frame { return c.frames }

// Dropped counts frames discarded because the backlog was full.
func (c *Capture) Dropped() int64 { return c.dropped.Load() }

// Err reports why the source stopped (after Frames is closed).
func (c *Capture) Err() error { return c.src.Err() }

func (c *Capture) run(ctx context.Context, chunks <-chan Chunk) {
	defer close(c.frames)
	var rs *Resampler
	var pending []float32 // 48 kHz stereo not yet framed
	for {
		var chunk Chunk
		var ok bool
		select {
		case <-ctx.Done():
			return
		case chunk, ok = <-chunks:
			if !ok {
				return
			}
		}
		if chunk.Channels <= 0 || chunk.SampleRate <= 0 {
			continue
		}
		if rs == nil || rs.InputRate() != chunk.SampleRate || rs.InputChannels() != chunk.Channels {
			rs = NewResampler(chunk.SampleRate, chunk.Channels)
		}
		pending = rs.Process(pending, chunk.Samples)
		off := 0
		for len(pending)-off >= FrameLen {
			f := new(Frame)
			copy(f[:], pending[off:off+FrameLen])
			off += FrameLen
			c.push(f)
		}
		pending = pending[:copy(pending, pending[off:])]
	}
}

func (c *Capture) push(f *Frame) {
	for {
		select {
		case c.frames <- f:
			return
		default:
		}
		select {
		case <-c.frames:
			c.dropped.Add(1)
		default:
		}
	}
}
