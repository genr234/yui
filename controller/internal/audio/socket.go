package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

// DefaultPCMSocketAddr is where the PCM socket listens unless configured
// otherwise. (DSU uses 26760 and 26761.)
const DefaultPCMSocketAddr = "127.0.0.1:26770"

// PCM socket wire format, version 1. A writer connects over TCP to the
// loopback address and sends a 12-byte header followed by raw interleaved
// little-endian PCM until it closes the connection:
//
//	offset size field
//	0      4    magic "YPCM"
//	4      1    version (1)
//	5      1    sample format: 1 = s16le, 2 = f32le
//	6      1    channels (1..8)
//	7      1    reserved (0)
//	8      4    sample rate, uint32 little-endian (8000..384000)
//
// Only one writer is live: a new connection replaces the previous one. To
// change format, reconnect with a new header. Write in real time (small
// chunks, 5-20 ms); a burst is played as fast as it arrives and the excess
// beyond the capture backlog is dropped.
const (
	pcmMagic      = "YPCM"
	pcmVersion    = 1
	pcmHeaderSize = 12
)

// PCMHeader encodes a version 1 header for f.
func PCMHeader(f Format) []byte {
	h := make([]byte, pcmHeaderSize)
	copy(h, pcmMagic)
	h[4] = pcmVersion
	h[5] = byte(f.Sample)
	h[6] = byte(f.Channels)
	binary.LittleEndian.PutUint32(h[8:], uint32(f.SampleRate))
	return h
}

func parsePCMHeader(h []byte) (Format, error) {
	if len(h) < pcmHeaderSize || string(h[:4]) != pcmMagic {
		return Format{}, errors.New("audio: pcm socket: bad magic")
	}
	if h[4] != pcmVersion {
		return Format{}, fmt.Errorf("audio: pcm socket: unsupported version %d", h[4])
	}
	f := Format{
		SampleRate: int(binary.LittleEndian.Uint32(h[8:])),
		Channels:   int(h[6]),
		Sample:     SampleFormat(h[5]),
	}
	return f, f.Validate()
}

// PCMSocket is the fallback capture backend: a loopback TCP listener that
// any runtime can write PCM into (e.g. a Dolphin build with a YuiStream
// backend). It works on every OS.
type PCMSocket struct {
	srcErr
	// Addr to listen on; empty means DefaultPCMSocketAddr. Must be a
	// loopback address.
	Addr string

	mu      sync.Mutex
	ln      net.Listener
	current net.Conn
}

func (s *PCMSocket) Name() string { return "pcm-socket" }

// ListenAddr returns the bound address once Start has returned.
func (s *PCMSocket) ListenAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

func (s *PCMSocket) Start(ctx context.Context) (<-chan Chunk, error) {
	addr := s.Addr
	if addr == "" {
		addr = DefaultPCMSocketAddr
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("audio: pcm socket must listen on loopback, got %q", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("audio: pcm socket: %w", err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	out := make(chan Chunk, 8)
	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		s.mu.Lock()
		if s.current != nil {
			_ = s.current.Close()
		}
		s.mu.Unlock()
	}()
	go func() {
		defer func() {
			wg.Wait()
			close(out)
		}()
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() == nil {
					s.setErr(fmt.Errorf("audio: pcm socket accept: %w", err))
				}
				return
			}
			if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok && !ta.IP.IsLoopback() {
				_ = conn.Close()
				continue
			}
			s.mu.Lock()
			if s.current != nil {
				_ = s.current.Close()
			}
			s.current = conn
			s.mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.serve(ctx, conn, out)
				s.mu.Lock()
				if s.current == conn {
					s.current = nil
				}
				s.mu.Unlock()
				_ = conn.Close()
			}()
		}
	}()
	return out, nil
}

func (s *PCMSocket) serve(ctx context.Context, conn net.Conn, out chan<- Chunk) {
	h := make([]byte, pcmHeaderSize)
	if _, err := io.ReadFull(conn, h); err != nil {
		return
	}
	f, err := parsePCMHeader(h)
	if err != nil {
		return
	}
	fb := f.FrameBytes()
	// Read up to ~10 ms at a time; deliver whatever whole sample frames
	// arrived so latency is not padded by our buffer size.
	buf := make([]byte, max(fb, f.SampleRate/100*fb))
	have := 0
	for {
		n, err := conn.Read(buf[have:])
		have += n
		if whole := have - have%fb; whole > 0 {
			samples := decodePCM(make([]float32, 0, whole/f.Sample.Bytes()), buf[:whole], f.Sample)
			have = copy(buf, buf[whole:have])
			s.mu.Lock()
			live := s.current == conn
			s.mu.Unlock()
			if !live {
				return
			}
			select {
			case <-ctx.Done():
				return
			case out <- Chunk{SampleRate: f.SampleRate, Channels: f.Channels, Samples: samples}:
			}
		}
		if err != nil {
			return
		}
	}
}
