package audio

import (
	"math"
	"math/rand"
	"testing"
)

func goertzel(x []float32, freq, fs float64) float64 {
	w := 2 * math.Pi * freq / fs
	c := 2 * math.Cos(w)
	var s1, s2 float64
	for _, v := range x {
		s0 := float64(v) + c*s1 - s2
		s2, s1 = s1, s0
	}
	return s1*s1 + s2*s2 - c*s1*s2
}

func sine(rate, ch, n int, freq, amp float64) []float32 {
	out := make([]float32, n*ch)
	for i := 0; i < n; i++ {
		v := float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
		for c := 0; c < ch; c++ {
			out[i*ch+c] = v
		}
	}
	return out
}

func channel(x []float32, ch, c int) []float32 {
	out := make([]float32, 0, len(x)/ch)
	for i := c; i < len(x); i += ch {
		out = append(out, x[i])
	}
	return out
}

func rms(x []float32) float64 {
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(x)))
}

func TestResamplerPreservesFrequency(t *testing.T) {
	for _, tc := range []struct {
		rate, ch int
	}{{44100, 1}, {44100, 2}, {32000, 2}, {22050, 1}, {96000, 2}, {16000, 1}} {
		in := sine(tc.rate, tc.ch, tc.rate, 1000, 0.5) // 1 second
		r := NewResampler(tc.rate, tc.ch)
		out := r.Process(nil, in)
		frames := len(out) / 2
		if d := frames - SampleRate; d < -resampleHalfTaps*SampleRate/tc.rate-2 || d > 2 {
			t.Fatalf("%d Hz: %d output frames for 1 s", tc.rate, frames)
		}
		for c := 0; c < 2; c++ {
			x := channel(out, 2, c)[1000:]
			p := goertzel(x, 1000, SampleRate)
			for _, other := range []float64{900, 1100, 1000 * float64(tc.rate) / SampleRate} {
				if other == 1000 {
					continue
				}
				if po := goertzel(x, other, SampleRate); p < 1000*po {
					t.Fatalf("%d Hz ch%d: 1 kHz %.3g vs %.0f Hz %.3g", tc.rate, c, p, other, po)
				}
			}
			if got := rms(x); math.Abs(got-0.5/math.Sqrt2) > 0.005 {
				t.Fatalf("%d Hz ch%d: rms %.4f", tc.rate, c, got)
			}
		}
	}
}

func TestResamplerChunkBoundaries(t *testing.T) {
	const rate = 44100
	in := sine(rate, 2, rate/2, 3000, 0.8)
	whole := NewResampler(rate, 2).Process(nil, in)

	rng := rand.New(rand.NewSource(1))
	r := NewResampler(rate, 2)
	var chunked []float32
	for i := 0; i < len(in); {
		n := (1 + rng.Intn(700)) * 2
		if i+n > len(in) {
			n = len(in) - i
		}
		chunked = r.Process(chunked, in[i:i+n])
		i += n
	}
	if len(chunked) != len(whole) {
		t.Fatalf("chunked %d samples, whole %d", len(chunked), len(whole))
	}
	for i := range whole {
		if math.Abs(float64(whole[i]-chunked[i])) > 1e-6 {
			t.Fatalf("sample %d differs: %v vs %v", i, whole[i], chunked[i])
		}
	}

	// No clicks: the second difference of a clean sine is bounded by
	// A*(2*pi*f/fs)^2; allow a little filter ripple.
	x := channel(chunked, 2, 0)[100:]
	limit := 0.8 * math.Pow(2*math.Pi*3000/SampleRate, 2) * 1.05
	for i := 2; i < len(x); i++ {
		if d := math.Abs(float64(x[i] - 2*x[i-1] + x[i-2])); d > limit {
			t.Fatalf("discontinuity at %d: %.4f > %.4f", i, d, limit)
		}
	}
}

func TestResamplerPassthroughAndDownmix(t *testing.T) {
	in := []float32{0.1, -0.2, 0.3, -0.4}
	out := NewResampler(SampleRate, 2).Process(nil, in)
	if len(out) != 4 || out[0] != 0.1 || out[3] != -0.4 {
		t.Fatalf("passthrough %v", out)
	}
	mono := NewResampler(SampleRate, 1).Process(nil, []float32{0.25})
	if len(mono) != 2 || mono[0] != 0.25 || mono[1] != 0.25 {
		t.Fatalf("mono %v", mono)
	}
	// 5.1: FL FR FC LFE BL BR
	six := NewResampler(SampleRate, 6).Process(nil, []float32{0.1, 0.2, 0.2, 1, 0.1, 0})
	if len(six) != 2 || math.Abs(float64(six[0])-(0.1+0.7071*0.2+0.7071*0.1)) > 1e-4 || math.Abs(float64(six[1])-(0.2+0.7071*0.2)) > 1e-4 {
		t.Fatalf("5.1 downmix %v", six)
	}
}

func TestResamplerDownsamplingRejectsAliases(t *testing.T) {
	// 30 kHz at 96 kHz would alias to 18 kHz at 48 kHz.
	in := sine(96000, 1, 96000, 30000, 0.5)
	out := NewResampler(96000, 1).Process(nil, in)
	x := channel(out, 2, 0)[1000:]
	if got := rms(x); got > 0.005 {
		t.Fatalf("alias rms %.4f", got)
	}
}
