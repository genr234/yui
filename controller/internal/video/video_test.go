package video

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// TestMain doubles as the fake helper process.
func TestMain(m *testing.M) {
	windowPoll = 5 * time.Millisecond
	if mode := os.Getenv("VIDEO_FAKE_HELPER"); mode != "" {
		switch mode {
		case "sleep":
			time.Sleep(time.Minute)
		case "exit":
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeCommand runs this test binary as the helper. Probes succeed only for the
// encoders listed in ok; any other run behaves as mode.
func fakeCommand(mode string, ok map[string]bool, runs *cmdLog) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		m := mode
		if strings.Contains(joined, "testsrc2") {
			m = "exit"
			for enc := range ok {
				if strings.Contains(joined, "-c:v "+enc+" ") {
					m = "ok"
				}
			}
			runs.add("probe", joined)
		} else {
			runs.add("run", joined)
		}
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "VIDEO_FAKE_HELPER="+m)
		return cmd
	}
}

type cmdLog struct {
	mu   sync.Mutex
	list []string
}

func (c *cmdLog) add(kind, args string) {
	c.mu.Lock()
	c.list = append(c.list, kind+" "+args)
	c.mu.Unlock()
}
func (c *cmdLog) count(kind string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, l := range c.list {
		if strings.HasPrefix(l, kind+" ") {
			n++
		}
	}
	return n
}

type fakeWindows struct {
	mu     sync.Mutex
	found  bool
	alive  bool
	hidden int
}

func (f *fakeWindows) Find(pid int, re *regexp.Regexp) (Window, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.found && re.MatchString("Pad View") {
		return 77, true
	}
	return 0, false
}
func (f *fakeWindows) Hide(Window) error { f.mu.Lock(); f.hidden++; f.mu.Unlock(); return nil }
func (f *fakeWindows) Alive(Window) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.alive }
func (f *fakeWindows) set(found, alive bool) {
	f.mu.Lock()
	f.found, f.alive = found, alive
	f.mu.Unlock()
}

type safeSink struct {
	mu  sync.Mutex
	got []rtp.Packet
}

func (s *safeSink) WriteRTP(p *rtp.Packet) error {
	s.mu.Lock()
	c := *p
	c.Payload = append([]byte(nil), p.Payload...)
	s.got = append(s.got, c)
	s.mu.Unlock()
	return nil
}
func (s *safeSink) n() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.got) }

type note struct {
	player, surface string
	on              bool
	w, h            int
}

type rig struct {
	t       *testing.T
	svc     *Service
	win     *fakeWindows
	runs    *cmdLog
	writers map[string]*safeSink
	relayed map[string]bool
	noVideo map[string]bool
	noLink  map[string]bool

	mu       sync.Mutex
	notes    []note
	statuses [][]Status

	timers []*fakeTimer
	slept  []time.Duration
}

type fakeTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

func newRig(t *testing.T, mode string, okEncoders ...string) *rig {
	t.Helper()
	r := &rig{t: t, win: &fakeWindows{}, runs: &cmdLog{},
		writers: map[string]*safeSink{"a": {}, "b": {}}, relayed: map[string]bool{},
		noVideo: map[string]bool{}, noLink: map[string]bool{}}
	ok := map[string]bool{}
	for _, e := range okEncoders {
		ok[e] = true
	}
	if len(ok) == 0 {
		ok["libx264"] = true
	}
	r.svc = New(Config{
		Helper:  func() (string, error) { return "ffmpeg-fake", nil },
		Windows: r.win,
		Command: fakeCommand(mode, ok, r.runs),
		Writer: func(p string) (RTPWriter, bool) {
			w, has := r.writers[p]
			return w, has && !r.noLink[p]
		},
		CanShow: func(p string) bool { _, has := r.writers[p]; return has && !r.noVideo[p] },
		Relayed: func(p string) bool { return r.relayed[p] },
		Notify: func(p, s string, on bool, w, h int) {
			r.mu.Lock()
			r.notes = append(r.notes, note{p, s, on, w, h})
			r.mu.Unlock()
		},
		OnStatus: func(app string, st []Status) {
			r.mu.Lock()
			r.statuses = append(r.statuses, st)
			r.mu.Unlock()
		},
	})
	r.svc.afterFunc = func(d time.Duration, f func()) func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		ft := &fakeTimer{d: d, f: f}
		r.timers = append(r.timers, ft)
		return func() bool { r.mu.Lock(); defer r.mu.Unlock(); was := !ft.stopped; ft.stopped = true; return was }
	}
	r.svc.sleep = func(d time.Duration) <-chan time.Time {
		r.mu.Lock()
		r.slept = append(r.slept, d)
		r.mu.Unlock()
		return time.After(2 * time.Millisecond)
	}
	t.Cleanup(r.svc.Close)
	return r
}

func (r *rig) start() {
	r.t.Helper()
	r.win.set(true, true)
	r.svc.SessionStarted("app", 1, []SurfaceSpec{{ID: "screen", Title: "(?i)pad view", Hidden: true}})
	r.waitState("screen", StateReady)
}

func (r *rig) waitState(id string, want State) Status {
	r.t.Helper()
	var last []Status
	for i := 0; i < 400; i++ {
		last = r.svc.List("app")
		for _, s := range last {
			if s.ID == id && s.State == want {
				return s
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("surface %s never reached %s: %+v", id, want, last)
	return Status{}
}

func (r *rig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	for i := 0; i < 400; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("timed out waiting for %s", what)
}

func (r *rig) notesCopy() []note {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]note(nil), r.notes...)
}

// port returns the loopback port of the surface's stream.
func (r *rig) port() int {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	return r.svc.find("app", "screen").st.port
}

func (r *rig) send(seq uint16, payload ...byte) {
	r.t.Helper()
	raw, _ := (&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: seq, Timestamp: 1}, Payload: payload}).Marshal()
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: r.port()})
	if err != nil {
		r.t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write(raw)
}

func TestWindowFoundHiddenAndReady(t *testing.T) {
	r := newRig(t, "sleep")
	r.svc.SessionStarted("app", 1, []SurfaceSpec{{ID: "screen", Title: "pad view", Hidden: true}})
	if s := r.svc.List("app"); len(s) != 1 || s[0].State != StateWaiting || s[0].Width != 854 || s[0].Height != 480 {
		t.Fatalf("%+v", s)
	}
	// "pad view" is case sensitive, the fake title is "Pad View".
	time.Sleep(30 * time.Millisecond)
	if r.svc.List("app")[0].State != StateWaiting {
		t.Fatal("pattern matched wrongly")
	}
	r.svc.SessionStarted("app", 1, []SurfaceSpec{{ID: "screen", Title: "(?i)pad view", Hidden: true}})
	r.win.set(true, true)
	r.waitState("screen", StateReady)
	if r.win.hidden != 1 {
		t.Fatalf("hidden %d", r.win.hidden)
	}
}

func TestBadPatternFails(t *testing.T) {
	r := newRig(t, "sleep")
	r.svc.SessionStarted("app", 1, []SurfaceSpec{{ID: "screen", Title: "("}})
	if s := r.svc.List("app")[0]; s.State != StateFailed || s.Error == "" {
		t.Fatalf("%+v", s)
	}
}

func TestShowErrors(t *testing.T) {
	r := newRig(t, "sleep")
	if _, err := r.svc.Show("app", "screen", "a"); !errors.Is(err, ErrUnknownSurface) {
		t.Fatal(err)
	}
	r.svc.SessionStarted("app", 1, []SurfaceSpec{{ID: "screen", Title: "x"}})
	if _, err := r.svc.Show("app", "screen", "a"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("waiting: %v", err)
	}
	r.svc.SessionEnded("app")
	r.start()
	r.noVideo["a"] = true
	if _, err := r.svc.Show("app", "screen", "a"); !errors.Is(err, ErrNoVideo) {
		t.Fatalf("no caps: %v", err)
	}
	r.noLink["b"] = true
	if _, err := r.svc.Show("app", "screen", "b"); !errors.Is(err, ErrNoVideoLink) {
		t.Fatalf("no link: %v", err)
	}
	if _, err := r.svc.Show("app", "other", "b"); !errors.Is(err, ErrUnknownSurface) {
		t.Fatal(err)
	}
	if _, err := r.svc.Show("app", "screen", "ghost"); !errors.Is(err, ErrNoVideo) {
		t.Fatalf("unknown player: %v", err)
	}
}

func TestShowMoveAndStop(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	st, err := r.svc.Show("app", "screen", "a")
	if err != nil || st.State != StateStreaming || st.PlayerID == nil || *st.PlayerID != "a" {
		t.Fatalf("%+v %v", st, err)
	}
	if app, id, ok := r.svc.SurfaceOf("a"); !ok || app != "app" || id != "screen" {
		t.Fatal("SurfaceOf a")
	}
	if _, err := r.svc.Show("app", "screen", "b"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.svc.SurfaceOf("a"); ok {
		t.Fatal("a still watching")
	}
	if _, _, ok := r.svc.SurfaceOf("b"); !ok {
		t.Fatal("b not watching")
	}
	want := []note{{"a", "screen", true, 854, 480}, {"a", "screen", false, 0, 0}, {"b", "screen", true, 854, 480}}
	got := r.notesCopy()
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("note %d: %+v want %+v", i, got[i], want[i])
		}
	}
	st, err = r.svc.Show("app", "screen", "")
	if err != nil || st.State != StateReady || st.PlayerID != nil {
		t.Fatalf("%+v %v", st, err)
	}
	if n := r.notesCopy(); n[len(n)-1] != (note{"b", "screen", false, 0, 0}) {
		t.Fatalf("%+v", n)
	}
	r.mu.Lock()
	last := r.statuses[len(r.statuses)-1]
	r.mu.Unlock()
	if last[0].State != StateReady {
		t.Fatalf("%+v", last)
	}
}

func TestForwardingGateAndRenumber(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	if _, err := r.svc.Show("app", "screen", "a"); err != nil {
		t.Fatal(err)
	}
	r.send(500, 0x41, 1) // not a keyframe: held back
	r.send(501, 0x67, 1)
	r.send(502, 0x65, 2)
	w := r.writers["a"]
	r.waitFor("two packets", func() bool { return w.n() >= 2 })
	time.Sleep(20 * time.Millisecond)
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.got) != 2 || w.got[0].SequenceNumber != 0 || w.got[1].SequenceNumber != 1 || w.got[0].Payload[0] != 0x67 {
		t.Fatalf("%+v", w.got)
	}
}

func TestMoveRestartsGate(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.send(1, 0x67, 1)
	r.waitFor("a gets packet", func() bool { return r.writers["a"].n() == 1 })
	_, _ = r.svc.Show("app", "screen", "b")
	r.send(2, 0x41, 1) // mid-GOP: b must not get it
	r.send(3, 0x67, 1)
	r.waitFor("b gets packet", func() bool { return r.writers["b"].n() >= 1 })
	time.Sleep(20 * time.Millisecond)
	if r.writers["b"].n() != 1 || r.writers["a"].n() != 1 {
		t.Fatalf("a=%d b=%d", r.writers["a"].n(), r.writers["b"].n())
	}
	if r.writers["b"].got[0].SequenceNumber != 0 {
		t.Fatal("b not renumbered from 0")
	}
}

func TestPeerChanged(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.svc.PeerChanged("a", false)
	if s := r.svc.List("app")[0]; s.State != StateReady || s.PlayerID != nil {
		t.Fatalf("%+v", s)
	}
	if _, _, ok := r.svc.SurfaceOf("a"); ok {
		t.Fatal("still watching")
	}
	r.send(1, 0x67, 1)
	time.Sleep(20 * time.Millisecond)
	if r.writers["a"].n() != 0 {
		t.Fatal("forwarded while down")
	}
	r.svc.PeerChanged("a", true)
	if s := r.svc.List("app")[0]; s.State != StateStreaming || s.PlayerID == nil {
		t.Fatalf("%+v", s)
	}
	r.send(2, 0x65, 1)
	r.waitFor("resumed", func() bool { return r.writers["a"].n() == 1 })
}

func TestHelperStartsOncePerTierAndRestartsOnTierChange(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.waitFor("helper start", func() bool { return r.runs.count("run") == 1 })
	_, _ = r.svc.Show("app", "screen", "b")
	time.Sleep(30 * time.Millisecond)
	if r.runs.count("run") != 1 {
		t.Fatalf("restarted on same tier: %d", r.runs.count("run"))
	}
	r.relayed["a"] = true
	_, _ = r.svc.Show("app", "screen", "a")
	r.waitFor("tier restart", func() bool { return r.runs.count("run") == 2 })
	r.runs.mu.Lock()
	last := r.runs.list[len(r.runs.list)-1]
	r.runs.mu.Unlock()
	if !strings.Contains(last, "max_framerate=30") || !strings.Contains(last, "1200k") {
		t.Fatalf("relayed tier args: %s", last)
	}
}

func TestEncoderProbedOnceAndLadderOrder(t *testing.T) {
	r := newRig(t, "sleep", "h264_amf", "libx264")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.waitFor("helper", func() bool { return r.runs.count("run") == 1 })
	r.runs.mu.Lock()
	defer r.runs.mu.Unlock()
	probes := 0
	for _, l := range r.runs.list {
		if strings.HasPrefix(l, "probe ") {
			probes++
		}
	}
	if probes != 3 { // nvenc, qsv fail; amf passes
		t.Fatalf("probes %d", probes)
	}
	if !strings.Contains(r.runs.list[len(r.runs.list)-1], "h264_amf") {
		t.Fatal("amf not used")
	}
}

func TestNoEncoderFails(t *testing.T) {
	r := newRig(t, "sleep")
	r.svc.cfg.Command = fakeCommand("sleep", map[string]bool{"nothing": true}, r.runs)
	r.start()
	if _, err := r.svc.Show("app", "screen", "a"); err != nil {
		t.Fatal(err)
	}
	s := r.waitState("screen", StateFailed)
	if s.Error != "no_encoder" || s.PlayerID != nil {
		t.Fatalf("%+v", s)
	}
	if a := r.svc.Available(); a.Available || a.Reason != "no_encoder" {
		t.Fatalf("%+v", a)
	}
	if n := r.notesCopy(); n[len(n)-1].on {
		t.Fatalf("phone not told off: %+v", n)
	}
}

func TestHelperRestartBackoff(t *testing.T) {
	r := newRig(t, "exit")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.waitFor("restarts", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.slept) >= 4 })
	r.mu.Lock()
	defer r.mu.Unlock()
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}
	for i, w := range want {
		if r.slept[i] != w {
			t.Errorf("sleep %d: %v want %v", i, r.slept[i], w)
		}
	}
}

func TestIdleStop(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.waitFor("helper", func() bool { return r.runs.count("run") == 1 })
	_, _ = r.svc.Show("app", "screen", "")
	r.mu.Lock()
	var idle *fakeTimer
	for _, ft := range r.timers {
		if !ft.stopped {
			idle = ft
		}
	}
	r.mu.Unlock()
	if idle == nil || idle.d != 10*time.Second {
		t.Fatalf("no idle timer: %+v", idle)
	}
	idle.f()
	r.svc.mu.Lock()
	has := r.svc.find("app", "screen").st != nil
	r.svc.mu.Unlock()
	if has {
		t.Fatal("stream still up after idle")
	}
	// Showing again brings the stream back.
	if _, err := r.svc.Show("app", "screen", "a"); err != nil {
		t.Fatal(err)
	}
	r.waitFor("second helper", func() bool { return r.runs.count("run") == 2 })
}

func TestIdleTimerCancelledByShow(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	_, _ = r.svc.Show("app", "screen", "")
	_, _ = r.svc.Show("app", "screen", "b")
	r.mu.Lock()
	for _, ft := range r.timers[:len(r.timers)] {
		if !ft.stopped {
			r.mu.Unlock()
			t.Fatal("idle timer still armed while streaming")
		}
	}
	r.mu.Unlock()
}

func TestWindowClosed(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.win.set(true, false)
	s := r.waitState("screen", StateClosed)
	if s.PlayerID != nil {
		t.Fatalf("%+v", s)
	}
	if _, err := r.svc.Show("app", "screen", "a"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("%v", err)
	}
	if n := r.notesCopy(); n[len(n)-1].on {
		t.Fatal("phone not told off")
	}
}

func TestSessionEndedAndClose(t *testing.T) {
	r := newRig(t, "sleep")
	r.start()
	_, _ = r.svc.Show("app", "screen", "a")
	r.svc.SessionEnded("app")
	if len(r.svc.List("app")) != 0 {
		t.Fatal("still listed")
	}
	if _, _, ok := r.svc.SurfaceOf("a"); ok {
		t.Fatal("still watching")
	}
	r.svc.Close()
	r.svc.SessionStarted("app", 1, []SurfaceSpec{{ID: "screen", Title: "x"}})
	if len(r.svc.List("app")) != 0 {
		t.Fatal("started after close")
	}
}

func TestOnePhoneOneSurface(t *testing.T) {
	r := newRig(t, "sleep")
	r.win.set(true, true)
	r.svc.SessionStarted("app", 1, []SurfaceSpec{
		{ID: "screen", Title: "(?i)pad view"}, {ID: "map", Title: "(?i)pad view"}})
	r.waitState("screen", StateReady)
	r.waitState("map", StateReady)
	_, _ = r.svc.Show("app", "screen", "a")
	_, _ = r.svc.Show("app", "map", "a")
	_, id, _ := r.svc.SurfaceOf("a")
	if id != "map" {
		t.Fatalf("a watches %s", id)
	}
	if s := r.svc.List("app")[0]; s.State != StateReady {
		t.Fatalf("%+v", s)
	}
}

func TestAvailable(t *testing.T) {
	s := New(Config{})
	if runtimeSupported := platformSupported(); !runtimeSupported {
		if a := s.Available(); a.Available || a.Reason != "unsupported" {
			t.Fatalf("%+v", a)
		}
	}
	s = New(Config{Windows: &fakeWindows{}})
	if a := s.Available(); a.Available || a.Reason != "no_encoder" {
		t.Fatalf("nil helper: %+v", a)
	}
	s.cfg.Helper = func() (string, error) { return "", errors.New("missing") }
	if a := s.Available(); a.Reason != "no_encoder" {
		t.Fatalf("missing helper: %+v", a)
	}
	s.cfg.Helper = func() (string, error) { return "ffmpeg", nil }
	if a := s.Available(); !a.Available {
		t.Fatalf("%+v", a)
	}
}
