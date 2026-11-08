package audio

import (
	"context"
	"encoding/binary"
	"math"
	"net"
	"testing"
	"time"
)

func startSocket(t *testing.T) (*PCMSocket, <-chan Chunk, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &PCMSocket{Addr: "127.0.0.1:0"}
	chunks, err := s.Start(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return s, chunks, cancel
}

func dial(t *testing.T, s *PCMSocket, f Format) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", s.ListenAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(PCMHeader(f)); err != nil {
		t.Fatal(err)
	}
	return conn
}

func collect(t *testing.T, chunks <-chan Chunk, want int) []Chunk {
	t.Helper()
	var got []Chunk
	n := 0
	deadline := time.After(5 * time.Second)
	for n < want {
		select {
		case c, ok := <-chunks:
			if !ok {
				t.Fatalf("closed after %d samples", n)
			}
			got = append(got, c)
			n += len(c.Samples)
		case <-deadline:
			t.Fatalf("timeout after %d of %d samples", n, want)
		}
	}
	return got
}

func TestPCMSocketS16(t *testing.T) {
	s, chunks, cancel := startSocket(t)
	defer cancel()
	conn := dial(t, s, Format{SampleRate: 32000, Channels: 1, Sample: S16LE})
	defer conn.Close()

	pcm := make([]byte, 2*1000)
	for i := 0; i < 1000; i++ {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(i*30-15000)))
	}
	// Split mid-sample to exercise partial reads.
	_, _ = conn.Write(pcm[:333])
	time.Sleep(10 * time.Millisecond)
	_, _ = conn.Write(pcm[333:])

	var all []float32
	for _, c := range collect(t, chunks, 1000) {
		if c.SampleRate != 32000 || c.Channels != 1 {
			t.Fatalf("format %d/%d", c.SampleRate, c.Channels)
		}
		all = append(all, c.Samples...)
	}
	for i, v := range all {
		if want := float32(int16(i*30-15000)) / 32768; v != want {
			t.Fatalf("sample %d = %v, want %v", i, v, want)
		}
	}
}

func TestPCMSocketF32AndReplace(t *testing.T) {
	s, chunks, cancel := startSocket(t)
	defer cancel()
	first := dial(t, s, Format{SampleRate: 48000, Channels: 2, Sample: S16LE})
	defer first.Close()
	_, _ = first.Write(make([]byte, 400))
	collect(t, chunks, 200)

	second := dial(t, s, Format{SampleRate: 44100, Channels: 2, Sample: F32LE})
	defer second.Close()
	buf := make([]byte, 4*8)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(0.25))
	}
	_, _ = second.Write(buf)
	got := collect(t, chunks, 8)
	last := got[len(got)-1]
	if last.SampleRate != 44100 || last.Samples[len(last.Samples)-1] != 0.25 {
		t.Fatalf("second writer chunk %+v", last)
	}
	// The first writer was replaced and its connection closed.
	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := first.Read(make([]byte, 1)); err == nil {
		t.Fatal("first connection still open")
	}
}

func TestPCMSocketRejects(t *testing.T) {
	if _, err := (&PCMSocket{Addr: "0.0.0.0:0"}).Start(context.Background()); err == nil {
		t.Fatal("non-loopback address accepted")
	}
	s, chunks, cancel := startSocket(t)
	conn, err := net.Dial("tcp", s.ListenAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("NOPE\x01\x01\x02\x00\x80\xbb\x00\x00"))
	_, _ = conn.Write(make([]byte, 100))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("bad header not rejected")
	}
	conn.Close()
	cancel()
	for range chunks {
		t.Fatal("unexpected chunk")
	}
	if s.Err() != nil {
		t.Fatalf("err after cancel: %v", s.Err())
	}
}

func TestPCMHeaderRoundTrip(t *testing.T) {
	f := Format{SampleRate: 44100, Channels: 6, Sample: F32LE}
	got, err := parsePCMHeader(PCMHeader(f))
	if err != nil || got != f {
		t.Fatalf("%+v %v", got, err)
	}
	bad := PCMHeader(Format{SampleRate: 44100, Channels: 0, Sample: F32LE})
	if _, err := parsePCMHeader(bad); err == nil {
		t.Fatal("0 channels accepted")
	}
}
