package session

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"kiosk/controller/internal/audio"
	"kiosk/controller/internal/probe"
)

// Capture routes (contracts §4 audio.route). Auto captures a native runtime
// when one has registered its PID (AudioSource.SetCapturePID), otherwise the
// browser.
const (
	RouteAuto   = "auto"
	RouteApp    = "app"
	RouteNative = "native"
)

// AudioSource is how native-session code sends a runtime's sound to the
// speaker phone. SetCapturePID registers the runtime's process (its whole
// process tree is captured with WASAPI process loopback); ResetCapture goes
// back to the browser when the runtime exits. With the PCM-socket backend
// (D7 fallback) the PID is ignored: whatever writes to the socket is heard.
type AudioSource interface {
	SetCapturePID(pid int)
	ResetCapture()
}

// AudioConfig builds an Audio. Every field is optional.
type AudioConfig struct {
	// Probe returns the kiosk report that picks the capture backend
	// (audio.Choose). Called once, on the first capture. Default probe.Collect.
	Probe func(ctx context.Context) probe.Report
	// BrowserPID resolves Chrome's browser process when a capture starts
	// (web apps and the shell play their sound there).
	BrowserPID func(ctx context.Context) (int, error)
	// SocketAddr is the PCM socket address; empty = audio.DefaultPCMSocketAddr.
	SocketAddr string
	// NewSource replaces the AutoSource (tests).
	NewSource func(pid int) audio.Source
	Track     audio.TrackOptions
	// MaxBacklog bounds queued 10 ms frames (audio.StartCapture).
	MaxBacklog int
	// RetryDelay is the first wait before restarting a capture that ended
	// on its own; it doubles up to 5 s. Default 250 ms.
	RetryDelay time.Duration
}

// AudioStatus is the audio.status / audio.route result.
type AudioStatus struct {
	// Source is the requested route: auto, app or native.
	Source string `json:"source"`
	// Process is what is captured now: "browser" or "native" ("" when idle).
	Process   string `json:"process"`
	PID       int    `json:"pid"`
	Capturing bool   `json:"capturing"`
	// Backend is process-loopback or pcm-socket once a capture started.
	Backend string  `json:"backend"`
	Reason  string  `json:"reason,omitempty"`
	Volume  float64 `json:"volume"`
	Error   string  `json:"error,omitempty"`
}

// Audio sends the kiosk's sound to the speaker phone (plan 06). One Audio
// lives as long as the controller and serves each play session in turn.
//
// A play session gives every peer its own Opus send track before the offer
// and calls SetTarget with the speaker's track. The single audio.Track
// encodes each frame once into the current target, so moving the speaker
// needs no renegotiation. Capture runs only while there is a target, i.e. a
// session is active and its speaker is connected: idle costs no CPU.
type Audio struct {
	cfg AudioConfig

	// recMu serializes reconcile, which waits for a stopping capture
	// without holding mu.
	recMu sync.Mutex

	mu        sync.Mutex
	track     *audio.Track
	target    audio.SampleWriter
	pairL     audio.SampleWriter // stereo pair (SetPair); target is nil then
	pairR     audio.SampleWriter
	route     string
	nativePID int
	volume    float64
	run       *captureRun
	closed    bool
	backend   audio.Decision
	pid       int
	lastErr   error

	probeOnce sync.Once
	report    probe.Report
}

type captureRun struct {
	cancel    context.CancelFunc
	done      chan struct{}
	process   string
	nativePID int
}

// NewAudio does not start anything; the first SetTarget does.
func NewAudio(cfg AudioConfig) *Audio {
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 250 * time.Millisecond
	}
	return &Audio{cfg: cfg, route: RouteAuto, volume: 1}
}

// SetTarget points the encoder at w (the speaker peer's track) or at nobody
// (nil), and starts or stops the capture to match.
func (a *Audio) SetTarget(w audio.SampleWriter) { a.setTargets(w, nil, nil) }

// SetPair sends the left channel to l and the right channel to r, as two
// mono Opus streams (a stereo speaker pair). With either nil it behaves
// like SetTarget(nil). SetTarget returns to the single stream.
func (a *Audio) SetPair(l, r audio.SampleWriter) {
	if l == nil || r == nil {
		a.setTargets(nil, nil, nil)
		return
	}
	a.setTargets(nil, l, r)
}

func (a *Audio) setTargets(w, l, r audio.SampleWriter) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	if (w != nil || l != nil) && a.track == nil {
		tr, err := audio.NewTrack(a.cfg.Track)
		if err != nil {
			a.lastErr = err
			a.mu.Unlock()
			log.Printf("pad audio: opus encoder: %v", err)
			return
		}
		tr.SetGain(a.volume)
		a.track = tr
	}
	a.target, a.pairL, a.pairR = w, l, r
	if a.track != nil {
		if l != nil {
			if err := a.track.SwitchPair(l, r); err != nil {
				// No stereo encoders: stop rather than play a half mix.
				a.lastErr = err
				a.pairL, a.pairR = nil, nil
				a.track.SwitchTarget(nil)
				log.Printf("pad audio: stereo pair encoders: %v", err)
			}
		} else {
			a.track.SwitchTarget(w)
		}
	}
	a.mu.Unlock()
	a.reconcile()
}

// SetCapturePID captures a native runtime's process tree (AudioSource).
func (a *Audio) SetCapturePID(pid int) {
	if pid < 0 {
		pid = 0
	}
	a.mu.Lock()
	a.nativePID = pid
	a.mu.Unlock()
	a.reconcile()
}

// ResetCapture goes back to capturing the browser (AudioSource).
func (a *Audio) ResetCapture() { a.SetCapturePID(0) }

// SetRoute picks what ctx.audio sends: auto, app (the browser) or native.
func (a *Audio) SetRoute(route string) error {
	switch route {
	case "":
		route = RouteAuto
	case RouteAuto, RouteApp, RouteNative:
	default:
		return fmt.Errorf("audio route must be %s, %s or %s", RouteAuto, RouteApp, RouteNative)
	}
	a.mu.Lock()
	a.route = route
	a.mu.Unlock()
	a.reconcile()
	return nil
}

// SetVolume sets the hub gain from the speaker phone's slider: linear, 0–1.
func (a *Audio) SetVolume(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	a.mu.Lock()
	a.volume = v
	if a.track != nil {
		a.track.SetGain(v)
	}
	a.mu.Unlock()
	return v
}

// Volume is the current hub gain (0–1).
func (a *Audio) Volume() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.volume
}

// Status reports the route and the running capture.
func (a *Audio) Status() AudioStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := AudioStatus{Source: a.route, Volume: a.volume}
	if a.run != nil {
		st.Capturing = true
		st.Process = a.run.process
		st.PID = a.pid
		st.Backend = string(a.backend.Backend)
		st.Reason = a.backend.Reason
	}
	if a.lastErr != nil {
		st.Error = a.lastErr.Error()
	}
	return st
}

// Stats are the encoder counters (zero before the first capture).
func (a *Audio) Stats() audio.Stats {
	a.mu.Lock()
	tr := a.track
	a.mu.Unlock()
	if tr == nil {
		return audio.Stats{}
	}
	return tr.Stats()
}

// Close stops the capture and frees the encoder.
func (a *Audio) Close() error {
	a.mu.Lock()
	a.closed = true
	a.target, a.pairL, a.pairR = nil, nil, nil
	a.mu.Unlock()
	a.reconcile()
	a.mu.Lock()
	tr := a.track
	a.track = nil
	a.mu.Unlock()
	if tr != nil {
		return tr.Close()
	}
	return nil
}

// wantedLocked is the process to capture: "browser" or "native" + its PID.
func (a *Audio) wantedLocked() (string, int) {
	switch {
	case a.route == RouteApp:
		return "browser", 0
	case a.nativePID > 0:
		return "native", a.nativePID
	default:
		// auto with no native runtime, or native requested before one
		// registered: the browser is the only thing making sound.
		return "browser", 0
	}
}

func (a *Audio) reconcile() {
	a.recMu.Lock()
	defer a.recMu.Unlock()
	a.mu.Lock()
	want := (a.target != nil || a.pairL != nil) && a.track != nil && !a.closed
	process, nativePID := a.wantedLocked()
	if run := a.run; run != nil && (!want || run.process != process || run.nativePID != nativePID) {
		a.run = nil
		a.mu.Unlock()
		run.cancel()
		<-run.done
		a.mu.Lock()
	}
	if want && a.run == nil {
		ctx, cancel := context.WithCancel(context.Background())
		run := &captureRun{cancel: cancel, done: make(chan struct{}), process: process, nativePID: nativePID}
		a.run = run
		a.pid = 0
		a.backend = audio.Decision{}
		go a.capture(ctx, run, a.track)
	}
	a.mu.Unlock()
}

// capture keeps one capture running until ctx ends, restarting it with a
// backoff when the source stops on its own (Chrome restarted, socket error).
func (a *Audio) capture(ctx context.Context, run *captureRun, track *audio.Track) {
	defer close(run.done)
	delay := a.cfg.RetryDelay
	for {
		err := a.captureOnce(ctx, run, track)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("source ended")
		}
		a.mu.Lock()
		a.lastErr = err
		a.mu.Unlock()
		log.Printf("pad audio: capture stopped: %v (retry in %s)", err, delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay *= 2; delay > 5*time.Second {
			delay = 5 * time.Second
		}
	}
}

func (a *Audio) captureOnce(ctx context.Context, run *captureRun, track *audio.Track) error {
	pid := run.nativePID
	if run.process == "browser" && a.cfg.BrowserPID != nil {
		pidCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		resolved, err := a.cfg.BrowserPID(pidCtx)
		cancel()
		if err != nil {
			log.Printf("pad audio: browser pid: %v", err)
		}
		pid = resolved
	}
	var src audio.Source
	if a.cfg.NewSource != nil {
		src = a.cfg.NewSource(pid)
	} else {
		src = &audio.AutoSource{Probe: a.probe(), PID: uint32(pid), SocketAddr: a.cfg.SocketAddr}
	}
	capt, err := audio.StartCapture(ctx, src, a.cfg.MaxBacklog)
	if err != nil {
		return err
	}
	decision := audio.Decision{Backend: audio.Backend(src.Name())}
	if auto, ok := src.(*audio.AutoSource); ok {
		decision = auto.Decision()
	}
	a.mu.Lock()
	if a.run == run {
		a.pid = pid
		a.backend = decision
		a.lastErr = nil
	}
	a.mu.Unlock()
	log.Printf("pad audio: capturing %s pid %d via %s (%s)", run.process, pid, decision.Backend, decision.Reason)
	return track.Run(ctx, capt)
}

func (a *Audio) probe() probe.Report {
	a.probeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if a.cfg.Probe != nil {
			a.report = a.cfg.Probe(ctx)
		} else {
			a.report = probe.Collect(ctx)
		}
	})
	return a.report
}
