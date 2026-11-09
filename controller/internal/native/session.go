package native

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Session states reported in Status.State.
const (
	StateIdle      = "idle"      // no session has run since the controller started
	StatePreparing = "preparing" // extracting the runtime and writing config
	StateRunning   = "running"
	StateStopping  = "stopping"
	StateExited    = "exited"  // clean exit, or stopped on request
	StateCrashed   = "crashed" // exited on its own with a failure
	StateFailed    = "failed"  // could not be started
)

// Audio routes a Spec may ask for. Routing itself belongs to the audio
// service (plan 06); the route is carried on the session for it.
const (
	AudioNone    = "none"
	AudioSpeaker = "speaker"
)

var (
	// ErrBusy means another native session is still active.
	ErrBusy = errors.New("a native session is already running")
	// ErrNotOwner means the caller tried to stop another app's session.
	ErrNotOwner = errors.New("the native session belongs to another app")
)

// Status is the `native.status` result and event payload. It never carries
// a filesystem path.
type Status struct {
	State       string `json:"state"`
	SessionID   string `json:"session_id,omitempty"`
	AppID       string `json:"app_id,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	Audio       string `json:"audio,omitempty"`
	VirtualPads []int  `json:"virtual_pads,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	EndedAt     string `json:"ended_at,omitempty"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Active reports whether a session is preparing, running, or stopping.
func (s Status) Active() bool {
	return s.State == StatePreparing || s.State == StateRunning || s.State == StateStopping
}

// ConfigFile is written under the app's data directory before launch.
type ConfigFile struct {
	Path    string `json:"path"`    // relative to {{dataDir}}, forward slashes
	Content string `json:"content"` // template
}

// Spec describes one native session.
type Spec struct {
	AppID       string
	Scope       string // account the data directory belongs to; "" means "local"
	Runtime     string // runtime asset id
	Executable  string // path inside the extracted runtime, forward slashes
	Args        []string
	Files       []ConfigFile
	VirtualPads []int
	Audio       string
}

// Info describes a running session to observers.
type Info struct {
	ID          string
	AppID       string
	PID         int
	VirtualPads []int
	Audio       string
	// Assets are the asset ids the session uses (runtime and templates).
	Assets []string
}

// Observer is told when a session's process starts and after it ends. The
// input hub implements it to push and pop the session's focus-stack
// provider; the audio service can use it to capture the process. Calls are
// made without Manager locks held, in order: SessionStarted, then
// SessionEnded for the same session.
type Observer interface {
	SessionStarted(Info)
	SessionEnded(Info, Status)
}

// Config holds a Manager's dependencies.
type Config struct {
	RuntimesDir string // <ConfigDir>/runtimes
	DataDir     string // <ConfigDir>/native/data; sessions get <DataDir>/<scope>/<app>
	LogDir      string // <ConfigDir>/native/logs; stdout/stderr per app
	Assets      AssetSource
	VirtualPads VirtualPadResolver
	Focus       Focuser
	OnStatus    func(Status)
	Limits      Limits
	// StopGrace is how long Stop waits after the graceful request before
	// killing. Default 10 s.
	StopGrace time.Duration
	// Command builds the process. Tests replace it; default exec.Command.
	Command func(exe string, args []string) *exec.Cmd
	Now     func() time.Time
}

// Manager runs at most one native session at a time.
type Manager struct {
	cfg      Config
	runtimes *Runtimes

	mu        sync.Mutex
	status    Status
	cur       *session
	pads      VirtualPadResolver
	observers map[int]Observer
	nextObs   int

	emitMu sync.Mutex
}

type session struct {
	info          Info
	cmd           *exec.Cmd
	stopRequested bool
	done          chan struct{}
	focusStop     chan struct{}
	focus         FocusToken
	logFile       *os.File
}

// NewManager returns an idle Manager.
func NewManager(cfg Config) *Manager {
	if cfg.Focus == nil {
		cfg.Focus = noFocus{}
	}
	if cfg.StopGrace <= 0 {
		cfg.StopGrace = 10 * time.Second
	}
	if cfg.Command == nil {
		cfg.Command = func(exe string, args []string) *exec.Cmd { return exec.Command(exe, args...) }
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	pads := cfg.VirtualPads
	if pads == nil {
		pads = noVirtualPads{}
	}
	return &Manager{
		cfg:       cfg,
		runtimes:  &Runtimes{Root: cfg.RuntimesDir, Limits: cfg.Limits},
		status:    Status{State: StateIdle},
		pads:      pads,
		observers: map[int]Observer{},
	}
}

// SetVirtualPads installs the resolver for {{virtualPad:<n>}}. nil restores
// the default, which fails with ErrVirtualPadsNotAttached.
func (m *Manager) SetVirtualPads(r VirtualPadResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r == nil {
		r = noVirtualPads{}
	}
	m.pads = r
}

// AddObserver registers o and returns a function that removes it.
func (m *Manager) AddObserver(o Observer) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextObs
	m.nextObs++
	m.observers[id] = o
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.observers, id)
	}
}

// Status returns the current or most recent session's status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() Status {
	st := m.status
	st.VirtualPads = append([]int(nil), st.VirtualPads...)
	return st
}

// InUse reports whether the active session uses the asset. The asset
// syncer asks before deleting a file that left the manifest.
func (m *Manager) InUse(assetID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return false
	}
	for _, id := range m.cur.info.Assets {
		if id == assetID {
			return true
		}
	}
	return false
}

// publish sends the latest status. emitMu keeps events in order, and each
// event reads the status under it, so the last event is always current.
func (m *Manager) publish() {
	if m.cfg.OnStatus == nil {
		return
	}
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.cfg.OnStatus(m.Status())
}

func (m *Manager) observerList() []Observer {
	out := make([]Observer, 0, len(m.observers))
	for _, o := range m.observers {
		out = append(out, o)
	}
	return out
}

func (m *Manager) now() string {
	return m.cfg.Now().UTC().Format(time.RFC3339)
}

// Launch prepares and starts a session. It returns once the process has
// started (or failed to). The returned error is safe to show to apps.
func (m *Manager) Launch(spec Spec) (Status, error) {
	if err := normalizeSpec(&spec); err != nil {
		return m.Status(), err
	}

	m.mu.Lock()
	if m.cur != nil {
		st := m.statusLocked()
		m.mu.Unlock()
		return st, ErrBusy
	}
	s := &session{
		info: Info{
			ID:          newSessionID(),
			AppID:       spec.AppID,
			VirtualPads: spec.VirtualPads,
			Audio:       spec.Audio,
			Assets:      []string{spec.Runtime},
		},
		done:      make(chan struct{}),
		focusStop: make(chan struct{}),
	}
	m.cur = s
	m.status = Status{
		State:       StatePreparing,
		SessionID:   s.info.ID,
		AppID:       spec.AppID,
		Runtime:     spec.Runtime,
		Audio:       spec.Audio,
		VirtualPads: spec.VirtualPads,
	}
	pads := m.pads
	m.mu.Unlock()
	m.publish()

	cmd, logFile, assets, err := m.prepare(spec, pads)

	m.mu.Lock()
	if err == nil && s.stopRequested {
		m.cur = nil
		m.status.State = StateExited
		m.status.EndedAt = m.now()
		st := m.statusLocked()
		m.mu.Unlock()
		closeQuietly(logFile)
		close(s.done)
		m.publish()
		return st, nil
	}
	if err == nil {
		s.info.Assets = appendUnique(s.info.Assets, assets...)
		s.focus = m.cfg.Focus.Capture()
		err = cmd.Start()
	}
	if err != nil {
		log.Printf("native: launch %s failed: %v", spec.AppID, err)
		public := errors.New(publicError(err))
		m.cur = nil
		m.status.State = StateFailed
		m.status.Error = public.Error()
		m.status.EndedAt = m.now()
		st := m.statusLocked()
		m.mu.Unlock()
		closeQuietly(logFile)
		close(s.done)
		m.publish()
		return st, public
	}
	s.cmd = cmd
	s.logFile = logFile
	s.info.PID = cmd.Process.Pid
	m.status.State = StateRunning
	m.status.StartedAt = m.now()
	info := s.info
	info.Assets = append([]string(nil), s.info.Assets...)
	observers := m.observerList()
	st := m.statusLocked()
	m.mu.Unlock()

	for _, o := range observers {
		o.SessionStarted(info)
	}
	m.publish()
	go m.cfg.Focus.Give(info.PID, s.focusStop)
	go m.watch(s, info)
	return st, nil
}

// watch is the watchdog: it waits for the process and reports how it ended.
func (m *Manager) watch(s *session, info Info) {
	waitErr := s.cmd.Wait()
	close(s.focusStop)
	code := -1
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}

	m.mu.Lock()
	m.status.EndedAt = m.now()
	m.status.ExitCode = &code
	switch {
	case s.stopRequested || (waitErr == nil && code == 0):
		m.status.State = StateExited
	default:
		m.status.State = StateCrashed
		if code >= 0 {
			m.status.Error = fmt.Sprintf("exited with code %d", code)
		} else {
			m.status.Error = "terminated unexpectedly"
		}
	}
	final := m.statusLocked()
	observers := m.observerList()
	m.mu.Unlock()

	closeQuietly(s.logFile)
	m.cfg.Focus.Restore(s.focus)
	for _, o := range observers {
		o.SessionEnded(info, final)
	}

	m.mu.Lock()
	if m.cur == s {
		m.cur = nil
	}
	m.mu.Unlock()
	close(s.done)
	m.publish()
}

// Stop ends the active session: a graceful request (SIGTERM, or WM_CLOSE on
// Windows), then a kill after StopGrace. force kills immediately. appID, when
// set, must own the session. Stopping with nothing active is not an error.
func (m *Manager) Stop(appID string, force bool) (Status, error) {
	m.mu.Lock()
	s := m.cur
	if s == nil {
		st := m.statusLocked()
		m.mu.Unlock()
		return st, nil
	}
	if appID != "" && s.info.AppID != appID {
		st := m.statusLocked()
		m.mu.Unlock()
		return st, ErrNotOwner
	}
	first := !s.stopRequested
	s.stopRequested = true
	if m.status.State == StateRunning {
		m.status.State = StateStopping
	}
	cmd := s.cmd
	m.mu.Unlock()
	if first {
		m.publish()
	}

	if cmd == nil {
		// Still preparing: Launch sees stopRequested and does not start.
		select {
		case <-s.done:
		case <-time.After(m.cfg.StopGrace):
		}
		return m.Status(), nil
	}

	graceful := false
	if !force && first {
		graceful = terminate(cmd.Process)
	}
	if graceful {
		select {
		case <-s.done:
			return m.Status(), nil
		case <-time.After(m.cfg.StopGrace):
		}
	}
	_ = cmd.Process.Kill()
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		log.Printf("native: session %s did not exit after kill", s.info.ID)
	}
	return m.Status(), nil
}

// Close stops any session. Called on controller shutdown.
func (m *Manager) Close() {
	_, _ = m.Stop("", false)
}

func (m *Manager) prepare(spec Spec, pads VirtualPadResolver) (*exec.Cmd, *os.File, []string, error) {
	if m.cfg.Assets == nil {
		return nil, nil, nil, userErrorf("assets are not available")
	}
	runtimeAsset, ok := m.cfg.Assets.Asset(spec.Runtime)
	if !ok {
		return nil, nil, nil, userErrorf("runtime asset is not available on this kiosk")
	}
	dir, err := m.runtimes.Dir(runtimeAsset)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("prepare runtime: %w", err)
	}
	exeRel, _ := safeRelPath(spec.Executable)
	exe := filepath.Join(dir, exeRel)
	if info, err := os.Stat(exe); err != nil || !info.Mode().IsRegular() {
		return nil, nil, nil, userErrorf("executable %q not found in the runtime", spec.Executable)
	}

	dataDir := filepath.Join(m.cfg.DataDir, spec.Scope, spec.AppID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("data directory: %w", err)
	}
	var used []string
	env := TemplateEnv{
		DataDir: dataDir,
		Asset: func(id string) (string, error) {
			a, ok := m.cfg.Assets.Asset(id)
			if !ok {
				return "", ErrAssetNotReady
			}
			used = appendUnique(used, id)
			return a.Path, nil
		},
		VirtualPad: func(n int) (string, error) {
			return pads.VirtualPad(spec.AppID, n)
		},
		SystemPad: func() (string, error) {
			if sys, ok := pads.(SystemPadResolver); ok {
				return sys.SystemPad(spec.AppID)
			}
			return "", ErrVirtualPadsNotAttached
		},
	}
	args := make([]string, len(spec.Args))
	for i, arg := range spec.Args {
		if args[i], err = Render(arg, env); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, file := range spec.Files {
		content, err := Render(file.Content, env)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", file.Path, err)
		}
		rel, _ := safeRelPath(file.Path)
		if err := writeFileAtomic(filepath.Join(dataDir, rel), []byte(content)); err != nil {
			return nil, nil, nil, fmt.Errorf("write %s: %w", file.Path, err)
		}
	}

	var logFile *os.File
	if m.cfg.LogDir != "" {
		if err := os.MkdirAll(m.cfg.LogDir, 0o755); err == nil {
			logFile, _ = os.OpenFile(filepath.Join(m.cfg.LogDir, spec.AppID+".log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		}
	}
	cmd := m.cfg.Command(exe, args)
	if cmd.Dir == "" {
		cmd.Dir = filepath.Dir(exe)
	}
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	return cmd, logFile, used, nil
}

func writeFileAtomic(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".yui-config-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Spec limits.
const (
	maxArgs        = 256
	maxArgBytes    = 32 << 10
	maxFiles       = 64
	maxFileBytes   = 1 << 20
	maxVirtualPads = MaxVirtualPad + 1
)

var appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,126}[a-z0-9_-]$`)

func normalizeSpec(spec *Spec) error {
	if !appIDPattern.MatchString(spec.AppID) {
		return userErrorf("invalid app id")
	}
	if spec.Scope == "" {
		spec.Scope = "local"
	}
	if !segmentPattern.MatchString(spec.Scope) {
		return userErrorf("invalid data scope")
	}
	if !assetIDPattern.MatchString(spec.Runtime) {
		return userErrorf("runtime must be a runtime asset id")
	}
	if rel, err := safeRelPath(spec.Executable); err != nil || rel == "" {
		return userErrorf("executable must be a relative path inside the runtime")
	}
	if len(spec.Args) > maxArgs {
		return userErrorf("too many arguments")
	}
	for _, arg := range spec.Args {
		if len(arg) > maxArgBytes {
			return userErrorf("argument too long")
		}
	}
	if len(spec.Files) > maxFiles {
		return userErrorf("too many config files")
	}
	seen := map[string]bool{}
	for _, file := range spec.Files {
		rel, err := safeRelPath(file.Path)
		if err != nil || rel == "" {
			return userErrorf("config file path %q must be relative to the data directory", file.Path)
		}
		if seen[rel] {
			return userErrorf("config file %q listed twice", file.Path)
		}
		seen[rel] = true
		if len(file.Content) > maxFileBytes {
			return userErrorf("config file %q is too large", file.Path)
		}
	}
	if len(spec.VirtualPads) > maxVirtualPads {
		return userErrorf("too many virtual pads")
	}
	pads := make([]int, 0, len(spec.VirtualPads))
	for _, n := range spec.VirtualPads {
		if n < 0 || n > MaxVirtualPad {
			return userErrorf("virtual pad must be 0-%d", MaxVirtualPad)
		}
		pads = appendUniqueInt(pads, n)
	}
	spec.VirtualPads = pads
	switch spec.Audio {
	case "":
		spec.Audio = AudioNone
	case AudioNone, AudioSpeaker:
	default:
		return userErrorf("audio must be %q or %q", AudioNone, AudioSpeaker)
	}
	return nil
}

// userError messages are written for apps and contain no paths.
type userError struct{ msg string }

func (e *userError) Error() string { return e.msg }

func userErrorf(format string, args ...any) error {
	return &userError{msg: fmt.Sprintf(format, args...)}
}

// publicError strips filesystem paths from err for the bridge. The full
// error is logged by the caller.
func publicError(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Op + ": " + pathErr.Err.Error()
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return linkErr.Op + ": " + linkErr.Err.Error()
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return "could not start the executable: " + execErr.Err.Error()
	}
	return err.Error()
}

func newSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("s%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func appendUnique(list []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, existing := range list {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			list = append(list, item)
		}
	}
	return list
}

func appendUniqueInt(list []int, n int) []int {
	for _, existing := range list {
		if existing == n {
			return list
		}
	}
	return append(list, n)
}

func closeQuietly(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}
