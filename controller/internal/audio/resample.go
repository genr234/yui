package audio

import (
	"math"
	"sync"
)

// Resampler converts a stream of interleaved PCM at any rate and channel
// count to 48 kHz stereo. It is a windowed-sinc (Kaiser) interpolator with
// an exact rational clock, so it neither drifts nor clicks at chunk
// boundaries: history carries over between Process calls and a stream cut
// into arbitrary chunks gives the same output as one long call.
//
// Added latency is resampleHalfTaps input samples (0.36 ms at 44.1 kHz);
// 48 kHz input skips the filter entirely.
type Resampler struct {
	inRate, inCh int
	num, den     int // step = num/den input frames per output frame
	pass         bool

	buf  []float32 // stereo history, interleaved
	ipos int       // integer part of the next output's time (frame index into buf)
	frac int       // fractional part, numerator over den
	w    [2 * resampleHalfTaps]float32
}

const (
	resampleHalfTaps = 16
	resamplePhases   = 256
	kaiserBeta       = 8.0
)

// kernelTables are cached per (num, den) cutoff; building one is cheap but
// there is no reason to repeat it for every stream.
type kernel struct {
	rows [resamplePhases + 1][2 * resampleHalfTaps]float32
}

var (
	kernelMu    sync.Mutex
	kernelCache = map[float64]*kernel{}
)

// NewResampler returns a resampler from inRate/inCh to 48 kHz stereo.
func NewResampler(inRate, inCh int) *Resampler {
	g := gcd(inRate, SampleRate)
	r := &Resampler{
		inRate: inRate,
		inCh:   inCh,
		num:    inRate / g,
		den:    SampleRate / g,
		pass:   inRate == SampleRate,
	}
	if !r.pass {
		// Prime with silence so the first output has its past taps.
		r.buf = make([]float32, 2*(resampleHalfTaps-1))
		r.ipos = resampleHalfTaps - 1
	}
	return r
}

// InputRate and InputChannels report what the resampler was built for.
func (r *Resampler) InputRate() int     { return r.inRate }
func (r *Resampler) InputChannels() int { return r.inCh }

// Process appends the 48 kHz stereo output for in (interleaved, inCh
// channels) to dst and returns it.
func (r *Resampler) Process(dst, in []float32) []float32 {
	if r.pass {
		return appendStereo(dst, in, r.inCh)
	}
	r.buf = appendStereo(r.buf, in, r.inCh)
	frames := len(r.buf) / 2
	k := getKernel(r.num, r.den)
	for r.ipos+resampleHalfTaps < frames {
		// Interpolate the kernel for this fractional phase.
		ph := float64(r.frac) * resamplePhases / float64(r.den)
		p := int(ph)
		a := float32(ph - float64(p))
		r0, r1 := &k.rows[p], &k.rows[p+1]
		for j := range r.w {
			r.w[j] = r0[j] + a*(r1[j]-r0[j])
		}
		base := (r.ipos - resampleHalfTaps + 1) * 2
		var l, rr float32
		for j, wj := range r.w {
			l += wj * r.buf[base+2*j]
			rr += wj * r.buf[base+2*j+1]
		}
		dst = append(dst, l, rr)

		r.frac += r.num
		r.ipos += r.frac / r.den
		r.frac %= r.den
	}
	// Drop history no future output needs.
	if drop := r.ipos - resampleHalfTaps + 1; drop > 0 {
		n := copy(r.buf, r.buf[drop*2:])
		r.buf = r.buf[:n]
		r.ipos -= drop
	}
	return dst
}

// appendStereo maps interleaved PCM with ch channels to stereo. Layouts with
// more than two channels follow the WAVE order (FL FR FC LFE BL BR SL SR):
// the centre goes to both sides at -3 dB, surrounds to their side at -3 dB,
// LFE is dropped.
func appendStereo(dst, in []float32, ch int) []float32 {
	switch ch {
	case 1:
		for _, v := range in {
			dst = append(dst, v, v)
		}
		return dst
	case 2:
		return append(dst, in...)
	}
	const m3db = 0.70710678
	for i := 0; i+ch <= len(in); i += ch {
		l, r := in[i], in[i+1]
		if ch >= 3 {
			l += m3db * in[i+2]
			r += m3db * in[i+2]
		}
		for c := 4; c < ch; c++ {
			if c%2 == 0 {
				l += m3db * in[i+c]
			} else {
				r += m3db * in[i+c]
			}
		}
		dst = append(dst, l, r)
	}
	return dst
}

func getKernel(num, den int) *kernel {
	// Cutoff relative to the input Nyquist: full band when upsampling,
	// the output Nyquist when downsampling, with a little transition room.
	cut := 0.97
	if num > den {
		cut *= float64(den) / float64(num)
	}
	kernelMu.Lock()
	defer kernelMu.Unlock()
	if k, ok := kernelCache[cut]; ok {
		return k
	}
	k := &kernel{}
	i0b := besselI0(kaiserBeta)
	for p := 0; p <= resamplePhases; p++ {
		frac := float64(p) / resamplePhases
		var sum float64
		var row [2 * resampleHalfTaps]float64
		for j := 0; j < 2*resampleHalfTaps; j++ {
			x := float64(j-resampleHalfTaps+1) - frac // tap offset from the output time
			v := cut * sinc(cut*x)
			wx := x / resampleHalfTaps
			if wx <= -1 || wx >= 1 {
				v = 0
			} else {
				v *= besselI0(kaiserBeta*math.Sqrt(1-wx*wx)) / i0b
			}
			row[j] = v
			sum += v
		}
		for j := range row {
			k.rows[p][j] = float32(row[j] / sum) // unity DC gain at every phase
		}
	}
	kernelCache[cut] = k
	return k
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 50; k++ {
		term *= (x / (2 * float64(k))) * (x / (2 * float64(k)))
		sum += term
		if term < 1e-12*sum {
			break
		}
	}
	return sum
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
