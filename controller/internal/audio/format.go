// Package audio captures a program's audio on the kiosk and turns it into
// 10 ms Opus frames for the speaker phone (plan decision D7, phase 6).
//
// Pipeline:
//
//	Source (process loopback | PCM socket | tone)  any rate/channels
//	  -> Frames: Resampler to 48 kHz stereo, cut into 10 ms frames
//	  -> Track: gain, Opus encode (libopus in wasm), media.Sample
//	  -> SampleWriter (the speaker peer's *webrtc.TrackLocalStaticSample)
package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// Output format of the pipeline: what Opus and WebRTC get.
const (
	SampleRate    = 48000
	Channels      = 2
	FrameDuration = 10 * time.Millisecond
	FrameSamples  = SampleRate / 100        // per channel, 480
	FrameLen      = FrameSamples * Channels // interleaved values, 960
)

// SampleFormat is the encoding of one PCM sample.
type SampleFormat uint8

const (
	S16LE SampleFormat = 1 // signed 16-bit little-endian
	F32LE SampleFormat = 2 // IEEE float32 little-endian, nominal [-1, 1]
)

// Bytes returns the size of one sample.
func (f SampleFormat) Bytes() int {
	switch f {
	case S16LE:
		return 2
	case F32LE:
		return 4
	}
	return 0
}

func (f SampleFormat) String() string {
	switch f {
	case S16LE:
		return "s16le"
	case F32LE:
		return "f32le"
	}
	return fmt.Sprintf("format(%d)", uint8(f))
}

// Format describes interleaved PCM.
type Format struct {
	SampleRate int
	Channels   int
	Sample     SampleFormat
}

// Validate rejects formats the pipeline cannot handle.
func (f Format) Validate() error {
	if f.SampleRate < 8000 || f.SampleRate > 384000 {
		return fmt.Errorf("audio: unsupported sample rate %d", f.SampleRate)
	}
	if f.Channels < 1 || f.Channels > 8 {
		return fmt.Errorf("audio: unsupported channel count %d", f.Channels)
	}
	if f.Sample.Bytes() == 0 {
		return fmt.Errorf("audio: unsupported sample format %d", f.Sample)
	}
	return nil
}

// FrameBytes is the size of one multi-channel sample frame.
func (f Format) FrameBytes() int { return f.Channels * f.Sample.Bytes() }

func (f Format) String() string {
	return fmt.Sprintf("%d Hz %dch %s", f.SampleRate, f.Channels, f.Sample)
}

// Chunk is a block of interleaved PCM in a source's native rate and channel
// count, already converted to float32. Sources may reuse nothing: a Chunk's
// slice belongs to the receiver.
type Chunk struct {
	SampleRate int
	Channels   int
	Samples    []float32 // interleaved, len is a multiple of Channels
}

// Frame is 10 ms of 48 kHz stereo interleaved float32 PCM.
type Frame [FrameLen]float32

// decodePCM converts raw little-endian PCM bytes to float32. len(src) must be
// a multiple of the sample size.
func decodePCM(dst []float32, src []byte, sf SampleFormat) []float32 {
	switch sf {
	case S16LE:
		for i := 0; i+1 < len(src); i += 2 {
			dst = append(dst, float32(int16(binary.LittleEndian.Uint16(src[i:])))/32768)
		}
	case F32LE:
		for i := 0; i+3 < len(src); i += 4 {
			v := math.Float32frombits(binary.LittleEndian.Uint32(src[i:]))
			if v != v { // NaN from a misbehaving writer
				v = 0
			}
			dst = append(dst, v)
		}
	}
	return dst
}
