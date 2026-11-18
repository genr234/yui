// Package video streams a native program's secondary window to one phone.
//
// A helper process (ffmpeg) captures and encodes the window to RTP on a
// loopback UDP port; the Service reads that RTP and forwards it to exactly one
// player's track, starting at a keyframe. The package knows nothing about
// sessions or peers: callers supply track writers and callbacks through Config.
package video

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
)

const (
	defaultWidth  = 854
	defaultHeight = 480

	idleStop    = 10 * time.Second
	backoffMin  = 250 * time.Millisecond
	backoffMax  = 5 * time.Second
	healthyRun  = 10 * time.Second
	udpBufBytes = 1 << 20
)

// windowPoll is how often the window of a surface is looked for and checked.
var windowPoll = 500 * time.Millisecond

type SurfaceSpec struct {
	ID     string
	Title  string // RE2 pattern for the window title
	Hidden bool
	Width  int // 0 = 854
	Height int // 0 = 480
}

type State string

const (
	StateWaiting   State = "waiting"
	StateReady     State = "ready"
	StateStreaming State = "streaming"
	StateFailed    State = "failed"
	StateClosed    State = "closed"
)

type Status struct {
	ID       string  `json:"id"`
	State    State   `json:"state"`
	PlayerID *string `json:"player_id"` // JSON null when shown to nobody
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	FPS      int     `json:"fps"`
	Error    string  `json:"error,omitempty"`
}

type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"` // "unsupported" | "no_encoder"
}

type RTPWriter interface {
	WriteRTP(p *rtp.Packet) error
}

// Window is an opaque top-level window handle (an HWND on Windows).
type Window uintptr

type WindowSystem interface {
	// Find returns a visible top-level window of pid or its descendants whose title matches.
	Find(pid int, title *regexp.Regexp) (Window, bool)
	// Hide keeps the window rendering but off the kiosk's display.
	Hide(w Window) error
	Alive(w Window) bool
}

type Config struct {
	// Helper returns the encoder executable (ffmpeg); an error means "no_encoder".
	Helper func() (path string, err error)
	// Writer returns a player's video track, false when they have no video link.
	Writer func(playerID string) (RTPWriter, bool)
	// CanShow reports whether the phone said caps.video.
	CanShow func(playerID string) bool
	// Relayed picks the bitrate tier.
	Relayed func(playerID string) bool
	// Notify sends the `surface` control message to a phone.
	Notify func(playerID, surfaceID string, on bool, width, height int)
	// OnStatus is called with an app's surfaces after every change.
	OnStatus func(appID string, surfaces []Status)
	Windows  WindowSystem                                // nil = the platform's
	Command  func(name string, args ...string) *exec.Cmd // tests; default exec.Command
	Now      func() time.Time
}

var (
	ErrNotReady       = errors.New("not_ready")
	ErrNoVideo        = errors.New("no_video")
	ErrNoVideoLink    = errors.New("no_video_link")
	ErrUnknownSurface = errors.New("unknown_surface")
)

type Service struct {
	cfg       Config
	supported bool

	mu       sync.Mutex
	sessions map[string]*session
	closed   bool

	probeMu sync.Mutex
	probes  map[string]probeResult

	// Replaced by tests.
	afterFunc func(d time.Duration, f func()) (stop func() bool)
	sleep     func(d time.Duration) <-chan time.Time
}

type session struct {
	appID    string
	pid      int
	surfaces []*surface
	stop     chan struct{}
}

type surface struct {
	spec   SurfaceSpec
	re     *regexp.Regexp
	state  State
	err    string
	win    Window
	width  int
	height int
	fps    int
	want   string // player the app asked for; "" for nobody
	linked bool   // want is currently receiving packets
	st     *stream
	app    *session
}

// stream is one helper process and its loopback socket.
type stream struct {
	conn    *net.UDPConn
	port    int
	tgt     atomic.Pointer[target]
	stop    chan struct{}
	kick    chan struct{}
	relayed bool // tier the helper is (to be) running with
	idle    func() bool
	stopped bool
}

func New(cfg Config) *Service {
	s := &Service{cfg: cfg, sessions: map[string]*session{}, probes: map[string]probeResult{}}
	s.supported = cfg.Windows != nil || platformSupported()
	if s.cfg.Windows == nil {
		s.cfg.Windows = platformWindows()
	}
	if s.cfg.Command == nil {
		s.cfg.Command = exec.Command
	}
	if s.cfg.Now == nil {
		s.cfg.Now = time.Now
	}
	s.afterFunc = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	s.sleep = time.After
	return s
}

// deferred collects callbacks to run after the mutex is released.
type deferred []func()

func (d *deferred) add(f func()) { *d = append(*d, f) }
func (d deferred) run() {
	for _, f := range d {
		f()
	}
}

func (s *Service) Available() Availability {
	if !s.supported {
		return Availability{Reason: "unsupported"}
	}
	if s.cfg.Helper == nil {
		return Availability{Reason: "no_encoder"}
	}
	path, err := s.cfg.Helper()
	if err != nil || path == "" || s.probedNone(path) {
		return Availability{Reason: "no_encoder"}
	}
	return Availability{Available: true}
}

// SessionStarted registers an app's surfaces and starts looking for their windows.
func (s *Service) SessionStarted(appID string, pid int, specs []SurfaceSpec) {
	s.SessionEnded(appID)
	se := &session{appID: appID, pid: pid, stop: make(chan struct{})}
	for _, sp := range specs {
		sf := &surface{spec: sp, state: StateWaiting, width: sp.Width, height: sp.Height, app: se}
		if sf.width == 0 {
			sf.width = defaultWidth
		}
		if sf.height == 0 {
			sf.height = defaultHeight
		}
		re, err := regexp.Compile(sp.Title)
		if err != nil {
			sf.state, sf.err = StateFailed, "bad window title pattern: "+err.Error()
		}
		sf.re = re
		se.surfaces = append(se.surfaces, sf)
	}
	var d deferred
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.sessions[appID] = se
	s.notifyStatus(se, &d)
	s.mu.Unlock()
	d.run()
	go s.watch(se)
}

func (s *Service) SessionEnded(appID string) {
	var d deferred
	s.mu.Lock()
	se := s.sessions[appID]
	if se != nil {
		delete(s.sessions, appID)
		close(se.stop)
		for _, sf := range se.surfaces {
			s.release(sf, StateClosed, &d)
		}
	}
	s.mu.Unlock()
	d.run()
}

// release clears a surface's target, stops its stream and sets its final state.
// The lock is held.
func (s *Service) release(sf *surface, state State, d *deferred) {
	s.unlink(sf, d)
	s.stopStream(sf)
	sf.state = state
	sf.fps = 0
}

// unlink stops forwarding to the current target and tells its phone. Lock held.
func (s *Service) unlink(sf *surface, d *deferred) {
	if sf.want != "" && sf.linked && s.cfg.Notify != nil {
		p, id := sf.want, sf.spec.ID
		d.add(func() { s.cfg.Notify(p, id, false, 0, 0) })
	}
	sf.want, sf.linked = "", false
	if sf.st != nil {
		sf.st.tgt.Store(nil)
		s.armIdle(sf)
	}
	if sf.state == StateStreaming {
		sf.state = StateReady
	}
}

// armIdle stops the stream after idleStop without a target. Lock held.
func (s *Service) armIdle(sf *surface) {
	st := sf.st
	if st.idle != nil {
		st.idle()
	}
	st.idle = s.afterFunc(idleStop, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if sf.st == st && st.tgt.Load() == nil {
			s.stopStream(sf)
			sf.fps = 0
		}
	})
}

// stopStream kills the helper and closes the socket. Lock held.
func (s *Service) stopStream(sf *surface) {
	st := sf.st
	if st == nil {
		return
	}
	sf.st = nil
	st.stopped = true
	if st.idle != nil {
		st.idle()
	}
	close(st.stop)
	_ = st.conn.Close()
}

func (s *Service) statuses(se *session) []Status {
	out := make([]Status, 0, len(se.surfaces))
	for _, sf := range se.surfaces {
		st := Status{ID: sf.spec.ID, State: sf.state, Width: sf.width, Height: sf.height, FPS: sf.fps, Error: sf.err}
		if sf.linked {
			p := sf.want
			st.PlayerID = &p
		}
		out = append(out, st)
	}
	return out
}

// notifyStatus queues an OnStatus call with a snapshot. Lock held.
func (s *Service) notifyStatus(se *session, d *deferred) {
	if s.cfg.OnStatus == nil {
		return
	}
	snap, app := s.statuses(se), se.appID
	d.add(func() { s.cfg.OnStatus(app, snap) })
}

func (s *Service) find(appID, id string) *surface {
	se := s.sessions[appID]
	if se == nil {
		return nil
	}
	for _, sf := range se.surfaces {
		if sf.spec.ID == id {
			return sf
		}
	}
	return nil
}

// Show moves a surface to playerID ("" = nobody). Errors: ErrNotReady, ErrNoVideo,
// ErrNoVideoLink, ErrUnknownSurface.
func (s *Service) Show(appID, surfaceID, playerID string) (Status, error) {
	var d deferred
	s.mu.Lock()
	st, err := s.show(appID, surfaceID, playerID, &d)
	s.mu.Unlock()
	d.run()
	return st, err
}

func (s *Service) show(appID, surfaceID, playerID string, d *deferred) (Status, error) {
	sf := s.find(appID, surfaceID)
	if sf == nil || s.closed {
		return Status{}, ErrUnknownSurface
	}
	snapshot := func() Status {
		for _, x := range s.statuses(sf.app) {
			if x.ID == surfaceID {
				return x
			}
		}
		return Status{}
	}
	if playerID == "" {
		if sf.want != "" {
			s.unlink(sf, d)
			s.notifyStatus(sf.app, d)
		}
		return snapshot(), nil
	}
	if sf.state != StateReady && sf.state != StateStreaming {
		return Status{}, ErrNotReady
	}
	if s.cfg.CanShow == nil || !s.cfg.CanShow(playerID) {
		return Status{}, ErrNoVideo
	}
	var w RTPWriter
	ok := false
	if s.cfg.Writer != nil {
		w, ok = s.cfg.Writer(playerID)
	}
	if !ok {
		return Status{}, ErrNoVideoLink
	}
	if sf.want == playerID && sf.linked {
		return snapshot(), nil
	}
	// A phone has one video track: it can show one surface at a time.
	for _, se := range s.sessions {
		for _, other := range se.surfaces {
			if other != sf && other.linked && other.want == playerID {
				s.unlink(other, d)
				s.notifyStatus(se, d)
			}
		}
	}
	if sf.linked {
		s.unlink(sf, d) // moving: the old phone is told "off"
	}
	s.link(sf, playerID, w, d)
	s.notifyStatus(sf.app, d)
	if sf.state == StateFailed {
		return Status{}, ErrNotReady
	}
	return snapshot(), nil
}

// link points the surface's stream at a player. Lock held.
func (s *Service) link(sf *surface, playerID string, w RTPWriter, d *deferred) {
	relayed := s.cfg.Relayed != nil && s.cfg.Relayed(playerID)
	if sf.st == nil {
		st, err := s.newStream(sf, relayed)
		if err != nil {
			sf.state, sf.err = StateFailed, "video socket: "+err.Error()
			return
		}
		sf.st = st
		go s.readLoop(st)
		go s.supervise(sf, st)
	} else if sf.st.relayed != relayed {
		sf.st.relayed = relayed
		select {
		case sf.st.kick <- struct{}{}:
		default:
		}
	}
	if sf.st.idle != nil {
		sf.st.idle()
		sf.st.idle = nil
	}
	sf.want, sf.linked, sf.state = playerID, true, StateStreaming
	sf.st.tgt.Store(&target{player: playerID, w: w})
	if n := s.cfg.Notify; n != nil {
		id, width, height := sf.spec.ID, sf.width, sf.height
		d.add(func() { n(playerID, id, true, width, height) })
	}
}

func (s *Service) newStream(sf *surface, relayed bool) (*stream, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	_ = conn.SetReadBuffer(udpBufBytes)
	return &stream{
		conn: conn, port: conn.LocalAddr().(*net.UDPAddr).Port,
		stop: make(chan struct{}), kick: make(chan struct{}, 1), relayed: relayed,
	}, nil
}

func (s *Service) List(appID string) []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	se := s.sessions[appID]
	if se == nil {
		return []Status{}
	}
	return s.statuses(se)
}

// SurfaceOf returns the surface a player is watching, for routing their touch.
func (s *Service) SurfaceOf(playerID string) (appID, surfaceID string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, se := range s.sessions {
		for _, sf := range se.surfaces {
			if sf.linked && sf.want == playerID {
				return se.appID, sf.spec.ID, true
			}
		}
	}
	return "", "", false
}

// PeerChanged: up=false stops forwarding to that player (their surface goes back to
// "ready" and the app is told); up=true after a renegotiation resumes at the next keyframe.
//
// The player stays the surface's wanted target while their link is down, so a
// returning peer resumes without the app calling Show again.
func (s *Service) PeerChanged(playerID string, up bool) {
	var d deferred
	s.mu.Lock()
	for _, se := range s.sessions {
		changed := false
		for _, sf := range se.surfaces {
			if sf.want != playerID {
				continue
			}
			switch {
			case !up && sf.linked:
				sf.linked = false
				sf.st.tgt.Store(nil)
				s.armIdle(sf)
				if sf.state == StateStreaming {
					sf.state = StateReady
				}
				changed = true
			case up && sf.state != StateFailed && sf.state != StateClosed:
				w, ok := (RTPWriter)(nil), false
				if s.cfg.Writer != nil && s.cfg.CanShow != nil && s.cfg.CanShow(playerID) {
					w, ok = s.cfg.Writer(playerID)
				}
				if ok {
					s.link(sf, playerID, w, &d)
					changed = true
				}
			}
		}
		if changed {
			s.notifyStatus(se, &d)
		}
	}
	s.mu.Unlock()
	d.run()
}

func (s *Service) Close() {
	var d deferred
	s.mu.Lock()
	s.closed = true
	for id, se := range s.sessions {
		delete(s.sessions, id)
		close(se.stop)
		for _, sf := range se.surfaces {
			s.release(sf, StateClosed, &d)
		}
	}
	s.mu.Unlock()
	d.run()
}

// fail marks a surface failed (helper missing, no encoder) and tells its phone.
func (s *Service) fail(sf *surface, st *stream, reason string) {
	var d deferred
	s.mu.Lock()
	if sf.st == st && !st.stopped {
		s.unlink(sf, &d)
		s.stopStream(sf)
		sf.state, sf.err, sf.fps = StateFailed, reason, 0
		s.notifyStatus(sf.app, &d)
	}
	s.mu.Unlock()
	d.run()
}

// readLoop forwards the helper's RTP to the current target.
func (s *Service) readLoop(st *stream) {
	buf := make([]byte, 2048)
	for {
		n, _, err := st.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var p rtp.Packet
		if p.Unmarshal(buf[:n]) != nil {
			continue
		}
		if t := st.tgt.Load(); t != nil {
			t.forward(&p)
		}
	}
}

// backoff doubles from backoffMin to backoffMax.
type backoff struct{ cur time.Duration }

func (b *backoff) next() time.Duration {
	if b.cur == 0 {
		b.cur = backoffMin
	}
	d := b.cur
	if b.cur *= 2; b.cur > backoffMax {
		b.cur = backoffMax
	}
	return d
}
func (b *backoff) reset() { b.cur = 0 }

// supervise runs the helper for a stream, restarting it with backoff.
func (s *Service) supervise(sf *surface, st *stream) {
	var bo backoff
	for {
		select {
		case <-st.stop:
			return
		default:
		}
		if s.cfg.Helper == nil {
			s.fail(sf, st, "no_encoder")
			return
		}
		path, err := s.cfg.Helper()
		if err != nil || path == "" {
			s.fail(sf, st, "no_encoder")
			return
		}
		enc, ok := s.probe(path)
		if !ok {
			s.fail(sf, st, "no_encoder")
			return
		}
		s.mu.Lock()
		if st.stopped {
			s.mu.Unlock()
			return
		}
		relayed, win, width, height := st.relayed, sf.win, sf.width, sf.height
		select {
		case <-st.kick:
		default:
		}
		s.mu.Unlock()

		kbps, fps := tierParams(enc, relayed)
		s.setFPS(sf, st, fps)
		args := helperArgs(helperOpts{Hwnd: uintptr(win), Width: width, Height: height,
			FPS: fps, Kbps: kbps, Encoder: enc, Port: st.port})
		log.Printf("video: %s: %s at %d fps, %d kbit/s", sf.spec.ID, enc.Name, fps, kbps)

		cmd := s.cfg.Command(path, args...)
		cmd.Stderr = &logWriter{prefix: "video: " + sf.spec.ID + ": "}
		started := s.cfg.Now()
		if err := cmd.Start(); err != nil {
			log.Printf("video: %s: start failed: %v", sf.spec.ID, err)
		} else {
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-st.stop:
				_ = cmd.Process.Kill()
				<-done
				return
			case <-st.kick:
				_ = cmd.Process.Kill()
				<-done
				bo.reset()
				continue
			}
		}
		if s.cfg.Now().Sub(started) >= healthyRun {
			bo.reset()
		}
		select {
		case <-s.sleep(bo.next()):
		case <-st.kick:
			bo.reset()
		case <-st.stop:
			return
		}
	}
}

func (s *Service) setFPS(sf *surface, st *stream, fps int) {
	var d deferred
	s.mu.Lock()
	if sf.st == st && sf.fps != fps {
		sf.fps = fps
		s.notifyStatus(sf.app, &d)
	}
	s.mu.Unlock()
	d.run()
}

// watch finds each surface's window, hides it, and notices when it closes.
func (s *Service) watch(se *session) {
	t := time.NewTicker(windowPoll)
	defer t.Stop()
	for {
		s.poll(se)
		select {
		case <-se.stop:
			return
		case <-t.C:
		}
	}
}

func (s *Service) poll(se *session) {
	type job struct {
		sf    *surface
		state State
		win   Window
	}
	var jobs []job
	s.mu.Lock()
	for _, sf := range se.surfaces {
		if sf.state == StateWaiting || sf.state == StateReady || sf.state == StateStreaming {
			jobs = append(jobs, job{sf, sf.state, sf.win})
		}
	}
	s.mu.Unlock()

	type result struct {
		job
		found   Window
		hideErr error
		alive   bool
	}
	var results []result
	for _, j := range jobs {
		r := result{job: j}
		if j.state == StateWaiting {
			if w, ok := s.cfg.Windows.Find(se.pid, j.sf.re); ok {
				r.found = w
				if j.sf.spec.Hidden {
					r.hideErr = s.cfg.Windows.Hide(w)
				}
			}
		} else {
			r.alive = s.cfg.Windows.Alive(j.win)
		}
		results = append(results, r)
	}

	var d deferred
	s.mu.Lock()
	for _, r := range results {
		sf := r.sf
		if s.sessions[se.appID] != se || sf.state != r.state {
			continue
		}
		switch {
		case r.state == StateWaiting && r.found != 0:
			sf.win = r.found
			if r.hideErr != nil {
				sf.state, sf.err = StateFailed, fmt.Sprintf("hide window: %v", r.hideErr)
			} else {
				sf.state = StateReady
			}
			s.notifyStatus(se, &d)
		case r.state != StateWaiting && !r.alive:
			s.release(sf, StateClosed, &d)
			s.notifyStatus(se, &d)
		}
	}
	s.mu.Unlock()
	d.run()
}

// logWriter sends helper stderr to the controller log, at most five lines a second.
type logWriter struct {
	prefix string
	mu     sync.Mutex
	buf    []byte
	last   time.Time
	drops  int
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := -1
		for k, c := range w.buf {
			if c == '\n' {
				i = k
				break
			}
		}
		if i < 0 {
			if len(w.buf) > 4096 {
				w.buf = nil
			}
			return len(p), nil
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if now := time.Now(); now.Sub(w.last) >= 200*time.Millisecond {
			if w.drops > 0 {
				log.Printf("%s(%d lines skipped)", w.prefix, w.drops)
				w.drops = 0
			}
			log.Printf("%s%s", w.prefix, line)
			w.last = now
		} else {
			w.drops++
		}
	}
}
