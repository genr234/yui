package opus

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestVersion(t *testing.T) {
	v, err := Version()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v, "1.5.2") {
		t.Fatalf("version %q", v)
	}
}

// goertzel returns the power of freq in x (mono, sample rate fs).
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

func TestEncodeDecodeSineRoundTrip(t *testing.T) {
	const fs, ch, frame = 48000, 2, 480
	enc, err := NewEncoder(fs, ch, AppRestrictedLowDelay)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	if err := enc.SetBitrate(96000); err != nil {
		t.Fatal(err)
	}
	dec, err := NewDecoder(fs, ch)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	look, err := enc.Lookahead()
	if err != nil || look <= 0 || look > 480 {
		t.Fatalf("lookahead %d %v", look, err)
	}

	const freqL, freqR = 1000.0, 1500.0
	var left, right []float32
	pcm := make([]float32, frame*ch)
	out := make([]float32, frame*ch)
	pkt := make([]byte, MaxPacket)
	n := 0
	for f := 0; f < 100; f++ { // 1 second
		for i := 0; i < frame; i++ {
			tt := float64(n) / fs
			pcm[2*i] = float32(0.5 * math.Sin(2*math.Pi*freqL*tt))
			pcm[2*i+1] = float32(0.5 * math.Sin(2*math.Pi*freqR*tt))
			n++
		}
		size, err := enc.EncodeFloat(pcm, pkt)
		if err != nil {
			t.Fatal(err)
		}
		if size < 2 || size > 400 {
			t.Fatalf("frame %d: packet size %d", f, size)
		}
		got, err := dec.DecodeFloat(pkt[:size], out)
		if err != nil {
			t.Fatal(err)
		}
		if got != frame {
			t.Fatalf("decoded %d samples, want %d", got, frame)
		}
		for i := 0; i < got; i++ {
			left = append(left, out[2*i])
			right = append(right, out[2*i+1])
		}
	}
	if d, err := dec.LastPacketDuration(); err != nil || d != frame {
		t.Fatalf("last packet duration %d %v", d, err)
	}

	// Skip the first 100 ms (codec start-up), then check that each channel
	// keeps its own tone, at its amplitude, and nothing else dominates.
	left, right = left[4800:], right[4800:]
	check := func(name string, x []float32, want, other float64) {
		pw := goertzel(x, want, fs)
		po := goertzel(x, other, fs)
		pn := goertzel(x, want*1.37, fs)
		if pw < 100*po || pw < 100*pn {
			t.Fatalf("%s: tone %.3g, other %.3g, off %.3g", name, pw, po, pn)
		}
		var rms float64
		for _, v := range x {
			rms += float64(v) * float64(v)
		}
		rms = math.Sqrt(rms / float64(len(x)))
		if want := 0.5 / math.Sqrt2; math.Abs(rms-want) > 0.05 {
			t.Fatalf("%s: rms %.3f, want ~%.3f", name, rms, want)
		}
	}
	check("left", left, freqL, freqR)
	check("right", right, freqR, freqL)
}

func TestInt16AndPLC(t *testing.T) {
	enc, err := NewEncoder(48000, 1, AppAudio)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec, err := NewDecoder(48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	pcm := make([]int16, 960)
	for i := range pcm {
		pcm[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/48000))
	}
	pkt := make([]byte, MaxPacket)
	n, err := enc.Encode(pcm, pkt)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]int16, 960)
	if got, err := dec.Decode(pkt[:n], out); err != nil || got != 960 {
		t.Fatalf("decode %d %v", got, err)
	}
	if got, err := dec.Decode(nil, out); err != nil || got != 960 {
		t.Fatalf("plc %d %v", got, err)
	}
	if _, err := enc.Encode(pcm[:7], pkt); err == nil {
		t.Fatal("expected an error for an illegal frame size")
	}
}

func TestIndependentInstancesConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enc, err := NewEncoder(48000, 2, AppRestrictedLowDelay)
			if err != nil {
				t.Error(err)
				return
			}
			defer enc.Close()
			pcm := make([]float32, 960)
			pkt := make([]byte, MaxPacket)
			for i := 0; i < 50; i++ {
				if _, err := enc.EncodeFloat(pcm, pkt); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestClosed(t *testing.T) {
	enc, err := NewEncoder(48000, 2, AppAudio)
	if err != nil {
		t.Fatal(err)
	}
	_ = enc.Close()
	if _, err := enc.EncodeFloat(make([]float32, 960), make([]byte, 100)); err == nil {
		t.Fatal("expected error after close")
	}
}
