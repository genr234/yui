package clips

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/audio"
)

// The replay buffer keeps the last minute or so of the kiosk's screen and
// game sound on disk, so a player can clip a moment after it happened.
//
// One ffmpeg grabs the screen (Desktop Duplication on Windows), encodes it on
// the GPU when it can, and writes 2-second MPEG-TS segments in a ring of
// files. The game's sound is fed to it on stdin, paced to the wall clock (with
// silence when nothing plays), so the audio track never stalls the muxer. The
// buffer runs only while someone could ask for a clip (a phone is connected).
//
// Segments are mapped to wall time from the moment ffmpeg lists each one: the
// smallest (seen - end) over the segments so far is the stream's start. That
// is what the facecam footage and reactions are lined up against.

const (
	segSecs    = 2
	segWrap    = 45 // files in the ring (90 s)
	keepSegs   = 38 // newest segments offered for clips; the rest may be reused
	replayFPS  = 30
	replayKbps = 6000
	replayMaxW = 1280
	// stopGrace keeps the buffer running a little after the last phone
	// leaves, so a reconnect does not lose the footage.
	stopGrace = 30 * time.Second
	listPoll  = 100 * time.Millisecond
)

func itoa(n int) string { return strconv.Itoa(n) }

// ErrUnavailable means the kiosk cannot clip (no screen grab or encoder).
var ErrUnavailable = errors.New("clips: not available")

// ErrNotReady means the buffer is not recording (yet).
var ErrNotReady = errors.New("clips: not recording")

// BufferConfig builds a Buffer.
type BufferConfig struct {
	// Helper returns the ffmpeg path.
	Helper func() (string, error)
	// Encoder returns the video codec options for the helper, false when no
	// encoder works.
	Encoder func(helper string) (func(kbps, fps, gop int) []string, bool)
	// Audio starts a capture of the game's sound. It returns 10 ms 48 kHz
	// stereo frames until ctx ends or the source stops (the channel closes);
	// the buffer then asks again. nil = silence.
	Audio func(ctx context.Context) (<-chan *audio.Frame, error)
	// Source is the lavfi screen grab; "" = the platform's (none off Windows).
	Source string
	// Dir holds the segment ring; "" = a temp dir.
	Dir string
	// Command starts ffmpeg; default exec.Command.
	Command func(name string, args ...string) *exec.Cmd
	Now     func() time.Time
}

// Segment is one finished screen segment on disk.
type Segment struct {
	Path       string
	Start, End time.Time // wall time
}

type listed struct {
	file       string
	start, end float64 // stream seconds
	seen       time.Time
}

// Buffer is the screen replay ring.
type Buffer struct {
	cfg BufferConfig

	mu      sync.Mutex
	wanted  bool
	running bool
	stopAt  time.Time
	segs    []listed
	anchor  time.Time // wall time of stream second 0
	gen     int       // bumps on every ffmpeg start; segs belong to one gen
	dir     string
	lastErr error
	wake    chan struct{}
	closed  bool
	done    chan struct{}
}

// NewBuffer starts the buffer's supervisor. Nothing records until SetWanted.
func NewBuffer(cfg BufferConfig) *Buffer {
	if cfg.Command == nil {
		cfg.Command = exec.Command
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Source == "" {
		cfg.Source = captureSource(replayFPS)
	}
	b := &Buffer{cfg: cfg, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go b.supervise()
	return b
}

// Supported reports whether this kiosk can grab its screen at all.
func (b *Buffer) Supported() bool { return b.cfg.Source != "" }

// SetWanted starts recording, or stops it after a grace period.
func (b *Buffer) SetWanted(on bool) {
	b.mu.Lock()
	if on {
		b.wanted = true
		b.stopAt = time.Time{}
	} else if b.wanted {
		b.wanted = false
		b.stopAt = b.cfg.Now().Add(stopGrace)
	}
	b.mu.Unlock()
	b.poke()
}

// Close stops recording for good and removes the ring.
func (b *Buffer) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()
	b.poke()
	<-b.done
}

func (b *Buffer) poke() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// Available is how much footage there is before t (0 when not recording).
func (b *Buffer) Available(t time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	segs := b.usableLocked()
	if len(segs) == 0 || b.anchor.IsZero() {
		return 0
	}
	first := b.wall(segs[0].start)
	if t.Before(first) {
		return 0
	}
	return t.Sub(first)
}

// Err is why the last recording stopped, if it failed.
func (b *Buffer) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastErr
}

func (b *Buffer) wall(stream float64) time.Time {
	return b.anchor.Add(time.Duration(stream * float64(time.Second)))
}

func (b *Buffer) usableLocked() []listed {
	if len(b.segs) > keepSegs {
		return b.segs[len(b.segs)-keepSegs:]
	}
	return b.segs
}

// Window returns the segments that cover [from, to], oldest first. It waits
// (up to wait) for the segment holding `to` to be finished. The files are
// valid for about keepSegs-segWrap segments' time: copy them at once.
func (b *Buffer) Window(ctx context.Context, from, to time.Time, wait time.Duration) ([]Segment, error) {
	deadline := b.cfg.Now().Add(wait)
	for {
		b.mu.Lock()
		segs := append([]listed(nil), b.usableLocked()...)
		anchor, dir, running := b.anchor, b.dir, b.running
		b.mu.Unlock()
		if !running && len(segs) == 0 {
			return nil, ErrNotReady
		}
		covered := len(segs) > 0 && !anchor.IsZero() &&
			!anchor.Add(time.Duration(segs[len(segs)-1].end*float64(time.Second))).Before(to)
		if covered || !b.cfg.Now().Before(deadline) || !running {
			var out []Segment
			for _, s := range segs {
				start := anchor.Add(time.Duration(s.start * float64(time.Second)))
				end := anchor.Add(time.Duration(s.end * float64(time.Second)))
				if end.After(from) && start.Before(to) {
					out = append(out, Segment{Path: filepath.Join(dir, s.file), Start: start, End: end})
				}
			}
			if len(out) == 0 {
				return nil, ErrNotReady
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(listPoll):
		}
	}
}

// supervise runs ffmpeg while wanted, restarting it with a backoff.
func (b *Buffer) supervise() {
	defer close(b.done)
	backoff := time.Second
	for {
		b.mu.Lock()
		closed := b.closed
		run := b.wanted || (!b.stopAt.IsZero() && b.cfg.Now().Before(b.stopAt))
		wait := time.Duration(0)
		if !b.wanted && !b.stopAt.IsZero() {
			wait = b.stopAt.Sub(b.cfg.Now())
		}
		b.mu.Unlock()
		if closed {
			b.cleanup()
			return
		}
		if !run || b.cfg.Source == "" {
			b.cleanup()
			if wait > 0 {
				select {
				case <-b.wake:
				case <-time.After(wait):
				}
			} else {
				<-b.wake
			}
			continue
		}
		started := time.Now()
		err := b.runOnce()
		if err != nil {
			log.Printf("clips: replay buffer stopped: %v", err)
		}
		b.mu.Lock()
		b.lastErr = err
		b.running = false
		b.mu.Unlock()
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-b.wake:
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (b *Buffer) cleanup() {
	b.mu.Lock()
	dir := b.dir
	b.dir = ""
	b.segs = nil
	b.anchor = time.Time{}
	b.mu.Unlock()
	if dir != "" && b.cfg.Dir == "" {
		_ = os.RemoveAll(dir)
	}
}

// shouldStop reports whether the running ffmpeg should end.
func (b *Buffer) shouldStop() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed || (!b.wanted && (b.stopAt.IsZero() || !b.cfg.Now().Before(b.stopAt)))
}

func (b *Buffer) runOnce() error {
	helper, err := b.cfg.Helper()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	enc, ok := b.cfg.Encoder(helper)
	if !ok {
		return fmt.Errorf("%w: no working encoder", ErrUnavailable)
	}
	dir := b.cfg.Dir
	if dir == "" {
		dir, err = os.MkdirTemp("", "yui-replay-")
		if err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		old, _ := filepath.Glob(filepath.Join(dir, "seg*.ts"))
		for _, f := range old {
			_ = os.Remove(f)
		}
		_ = os.Remove(filepath.Join(dir, "list.csv"))
	}
	b.mu.Lock()
	b.gen++
	b.dir = dir
	b.segs = nil
	b.anchor = time.Time{}
	b.running = true
	b.lastErr = nil
	b.mu.Unlock()

	cmd := b.cfg.Command(helper, replayArgs(b.cfg.Source, enc, dir)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var stderr tailBuffer
	cmd.Stderr = &stderr
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("clips: replay buffer recording (pid %d)", cmd.Process.Pid)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.pumpAudio(ctx, stdin)
	}()

	ticker := time.NewTicker(listPoll)
	defer ticker.Stop()
	var listOff int64
	var runErr error
loop:
	for {
		select {
		case err := <-exited:
			runErr = fmt.Errorf("ffmpeg exited: %v: %s", err, stderr.String())
			exited = nil
			break loop
		case <-b.wake:
		case <-ticker.C:
		}
		listOff = b.readList(dir, listOff)
		if b.shouldStop() {
			break loop
		}
	}
	cancel()
	_ = stdin.Close() // ffmpeg finishes the current segment and exits
	wg.Wait()
	if exited != nil {
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	}
	return runErr
}

// readList picks up the segments ffmpeg finished since off.
func (b *Buffer) readList(dir string, off int64) int64 {
	f, err := os.Open(filepath.Join(dir, "list.csv"))
	if err != nil {
		return off
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return off
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return off
	}
	// Only whole lines; a half-written one is read next time.
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return off
	}
	now := b.cfg.Now()
	var got []listed
	sc := bufio.NewScanner(bytes.NewReader(data[:end+1]))
	for sc.Scan() {
		if s, ok := parseListLine(sc.Text()); ok {
			s.seen = now
			got = append(got, s)
		}
	}
	if len(got) > 0 {
		b.mu.Lock()
		for _, s := range got {
			// The stream started at most (seen - end) ago; the earliest
			// such estimate has the least muxing delay in it.
			a := s.seen.Add(-time.Duration(s.end * float64(time.Second)))
			if b.anchor.IsZero() || a.Before(b.anchor) {
				b.anchor = a
			}
			b.segs = append(b.segs, s)
		}
		if len(b.segs) > segWrap {
			b.segs = append([]listed(nil), b.segs[len(b.segs)-segWrap:]...)
		}
		b.mu.Unlock()
	}
	return off + int64(end+1)
}

// parseListLine reads one segment-list CSV line: "file,start,end".
func parseListLine(line string) (listed, bool) {
	parts := strings.Split(strings.TrimSpace(line), ",")
	if len(parts) < 3 {
		return listed{}, false
	}
	n := len(parts)
	start, err1 := strconv.ParseFloat(parts[n-2], 64)
	end, err2 := strconv.ParseFloat(parts[n-1], 64)
	file := strings.Trim(strings.Join(parts[:n-2], ","), `"`)
	if err1 != nil || err2 != nil || file == "" || end <= start || strings.ContainsAny(file, `/\`) {
		return listed{}, false
	}
	return listed{file: file, start: start, end: end}, true
}

// replayArgs is the screen-and-sound recorder's command line. Pure; tested.
func replayArgs(source string, enc func(kbps, fps, gop int) []string, dir string) []string {
	args := []string{"-hide_banner", "-loglevel", "warning", "-nostats",
		"-f", "lavfi", "-i", source,
		"-f", "f32le", "-ar", strconv.Itoa(audio.SampleRate), "-ac", strconv.Itoa(audio.Channels), "-i", "pipe:0",
		"-map", "0:v", "-map", "1:a",
		"-vf", fmt.Sprintf("fps=%d,scale='min(%d,iw)':-2:flags=bilinear,format=yuv420p", replayFPS, replayMaxW),
	}
	args = append(args, enc(replayKbps, replayFPS, replayFPS*segSecs)...)
	args = append(args,
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segSecs),
		"-c:a", "aac", "-b:a", "128k",
		"-f", "segment", "-segment_time", strconv.Itoa(segSecs), "-segment_format", "mpegts",
		"-segment_wrap", strconv.Itoa(segWrap),
		"-segment_list", filepath.Join(dir, "list.csv"), "-segment_list_type", "csv", "-segment_list_size", "0",
		"-reset_timestamps", "0",
		filepath.Join(dir, "seg%03d.ts"))
	return args
}

// pumpAudio writes the game's sound to ffmpeg in real time: captured frames
// when there are any, silence otherwise, so the audio clock follows the wall
// clock and the muxer never waits for sound.
func (b *Buffer) pumpAudio(ctx context.Context, w io.Writer) {
	const frameSamples = audio.FrameLen / audio.Channels // per channel, 10 ms
	var frames <-chan *audio.Frame
	var srcCancel context.CancelFunc
	retry := time.NewTimer(0)
	defer func() {
		retry.Stop()
		if srcCancel != nil {
			srcCancel()
		}
	}()
	var queue []*audio.Frame
	maxQueue := 20 // 200 ms
	start := time.Now()
	var written int64 // samples per channel
	silence := make([]byte, audio.FrameLen*4)
	buf := make([]byte, audio.FrameLen*4)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
			if b.cfg.Audio != nil && frames == nil {
				sctx, cancel := context.WithCancel(ctx)
				ch, err := b.cfg.Audio(sctx)
				if err != nil {
					cancel()
					retry.Reset(3 * time.Second)
				} else {
					frames, srcCancel = ch, cancel
				}
			}
		case f, ok := <-frames:
			if !ok {
				frames = nil
				if srcCancel != nil {
					srcCancel()
					srcCancel = nil
				}
				retry.Reset(time.Second)
				continue
			}
			if len(queue) >= maxQueue {
				queue = queue[1:]
			}
			queue = append(queue, f)
		case <-tick.C:
			due := int64(time.Since(start).Seconds()*audio.SampleRate) - written
			for due >= frameSamples && len(queue) > 0 {
				f := queue[0]
				queue = queue[1:]
				for i, v := range f {
					binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
				}
				if _, err := w.Write(buf); err != nil {
					return
				}
				written += frameSamples
				due -= frameSamples
			}
			// More than 60 ms behind with nothing captured: fill with
			// silence, keeping a little slack for a late frame.
			for due > 6*frameSamples {
				if _, err := w.Write(silence); err != nil {
					return
				}
				written += frameSamples
				due -= frameSamples
			}
		}
	}
}

// tailBuffer keeps the last 2 KB written to it (ffmpeg's stderr).
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2048 {
		t.buf = t.buf[len(t.buf)-2048:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}
