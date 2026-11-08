package native

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as the fake native program: Command re-executes
// it with YUI_NATIVE_HELPER set, and TestMain runs the helper instead of the
// tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("YUI_NATIVE_HELPER"); mode != "" {
		os.Exit(runHelper(mode))
	}
	os.Exit(m.Run())
}

func runHelper(mode string) int {
	switch mode {
	case "exit0":
		return 0
	case "crash":
		return 3
	case "wait":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, os.Interrupt)
		select {
		case <-ch:
			return 0
		case <-time.After(30 * time.Second):
			return 9
		}
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(30 * time.Second)
		return 0
	}
	return 2
}

type fakeAssets map[string]AssetInfo

func (f fakeAssets) Asset(id string) (AssetInfo, bool) {
	a, ok := f[id]
	return a, ok
}

type recorder struct {
	mu       sync.Mutex
	statuses []Status
	events   []string
	exe      string
	args     []string
}

func (r *recorder) SessionStarted(info Info) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "started:"+info.AppID)
}

func (r *recorder) SessionEnded(info Info, st Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "ended:"+info.AppID+":"+st.State)
}

func (r *recorder) onStatus(st Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, st)
}

func (r *recorder) waitState(t *testing.T, state string) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, st := range r.statuses {
			if st.State == state {
				r.mu.Unlock()
				return st
			}
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %q status; got %+v", state, r.statuses)
	return Status{}
}

func (r *recorder) eventList() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

type testRig struct {
	mgr  *Manager
	rec  *recorder
	root string
}

func newRig(t *testing.T, mode string) *testRig {
	t.Helper()
	root := t.TempDir()
	archive := filepath.Join(root, "rt.zip")
	writeZip(t, archive, runtimeFixture)
	game := filepath.Join(root, "game.rvz")
	if err := os.WriteFile(game, []byte("disc"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	mgr := NewManager(Config{
		RuntimesDir: filepath.Join(root, "runtimes"),
		DataDir:     filepath.Join(root, "data"),
		LogDir:      filepath.Join(root, "logs"),
		Assets: fakeAssets{
			"rt1":   {ID: "rt1", Kind: AssetKindRuntime, Path: archive, Metadata: []byte(`{"runtime":"emu","version":"1"}`)},
			"game1": {ID: "game1", Kind: "game", Path: game},
		},
		OnStatus:  rec.onStatus,
		StopGrace: 300 * time.Millisecond,
		Command: func(exe string, args []string) *exec.Cmd {
			rec.mu.Lock()
			rec.exe, rec.args = exe, args
			rec.mu.Unlock()
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "YUI_NATIVE_HELPER="+mode)
			return cmd
		},
	})
	mgr.AddObserver(rec)
	t.Cleanup(func() { mgr.Stop("", true) })
	return &testRig{mgr: mgr, rec: rec, root: root}
}

func baseSpec() Spec {
	return Spec{
		AppID:      "dev.example.arcade",
		Scope:      "account-1",
		Runtime:    "rt1",
		Executable: "bin/game.exe",
		Args:       []string{"-u", "{{dataDir}}/user", "-e", "{{asset:game1}}"},
		Files: []ConfigFile{
			{Path: "user/Config/main.ini", Content: "[General]\nISOPath0 = {{asset:game1}}\nUser = {{dataDir}}\n"},
		},
	}
}

func TestLaunchTemplatesConfigAndArgs(t *testing.T) {
	rig := newRig(t, "wait")
	st, err := rig.mgr.Launch(baseSpec())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateRunning || st.AppID != "dev.example.arcade" || st.Audio != AudioNone {
		t.Fatalf("status = %+v", st)
	}
	dataDir := filepath.Join(rig.root, "data", "account-1", "dev.example.arcade")
	game := filepath.Join(rig.root, "game.rvz")
	got, err := os.ReadFile(filepath.Join(dataDir, "user", "Config", "main.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "[General]\nISOPath0 = " + game + "\nUser = " + dataDir + "\n"; string(got) != want {
		t.Fatalf("config =\n%s\nwant\n%s", got, want)
	}
	rig.rec.mu.Lock()
	exe, args := rig.rec.exe, rig.rec.args
	rig.rec.mu.Unlock()
	if exe != filepath.Join(rig.root, "runtimes", "emu", "1", "bin", "game.exe") {
		t.Fatalf("exe = %q", exe)
	}
	if strings.Join(args, "|") != "-u|"+dataDir+"/user|-e|"+game {
		t.Fatalf("args = %q", args)
	}
	if !rig.mgr.InUse("rt1") || !rig.mgr.InUse("game1") || rig.mgr.InUse("other") {
		t.Fatal("InUse does not report the session's assets")
	}
	if _, err := rig.mgr.Launch(baseSpec()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Launch = %v, want ErrBusy", err)
	}
	if _, err := rig.mgr.Stop("dev.other.app", false); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("Stop by another app = %v, want ErrNotOwner", err)
	}

	st, err = rig.mgr.Stop("dev.example.arcade", false)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateExited {
		t.Fatalf("after Stop: %+v", st)
	}
	if rig.mgr.InUse("rt1") {
		t.Fatal("assets still in use after the session ended")
	}
	if ev := rig.rec.eventList(); strings.Join(ev, ",") != "started:dev.example.arcade,ended:dev.example.arcade:exited" {
		t.Fatalf("observer events = %v", ev)
	}
	rig.rec.waitState(t, StatePreparing)
	rig.rec.waitState(t, StateStopping)
}

func TestWatchdogReportsCrash(t *testing.T) {
	rig := newRig(t, "crash")
	if _, err := rig.mgr.Launch(baseSpec()); err != nil {
		t.Fatal(err)
	}
	st := rig.rec.waitState(t, StateCrashed)
	if st.ExitCode == nil || *st.ExitCode != 3 || st.EndedAt == "" {
		t.Fatalf("crash status = %+v", st)
	}
	deadline := time.Now().Add(5 * time.Second)
	for rig.mgr.InUse("rt1") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// The slot frees up: a new session can start after a crash.
	rig2 := baseSpec()
	if _, err := rig.mgr.Launch(rig2); err != nil {
		t.Fatalf("relaunch after crash: %v", err)
	}
}

func TestWatchdogReportsCleanExit(t *testing.T) {
	rig := newRig(t, "exit0")
	if _, err := rig.mgr.Launch(baseSpec()); err != nil {
		t.Fatal(err)
	}
	st := rig.rec.waitState(t, StateExited)
	if st.ExitCode == nil || *st.ExitCode != 0 || st.Error != "" {
		t.Fatalf("exit status = %+v", st)
	}
}

func TestStopKillsAfterGrace(t *testing.T) {
	rig := newRig(t, "ignore-term")
	if _, err := rig.mgr.Launch(baseSpec()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let the helper ignore SIGTERM
	start := time.Now()
	st, err := rig.mgr.Stop("", false)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateExited {
		t.Fatalf("after kill: %+v", st)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("killed after %v, before the grace period", elapsed)
	}
}

func TestLaunchFailuresAreReportedWithoutPaths(t *testing.T) {
	rig := newRig(t, "wait")
	cases := map[string]func(*Spec){
		"virtual pads not attached":  func(s *Spec) { s.Args = []string{"{{virtualPad:0}}"} },
		"not found in the runtime":   func(s *Spec) { s.Executable = "bin/missing.exe" },
		"runtime asset is not":       func(s *Spec) { s.Runtime = "nope" },
		"asset is not a runtime":     func(s *Spec) { s.Runtime = "game1" },
		"relative path":              func(s *Spec) { s.Executable = "../../bin/sh" },
		"relative to the data":       func(s *Spec) { s.Files = []ConfigFile{{Path: "../x.ini", Content: "x"}} },
		"invalid app id":             func(s *Spec) { s.AppID = "Bad/App" },
		"audio must be":              func(s *Spec) { s.Audio = "hdmi" },
		"unknown placeholder":        func(s *Spec) { s.Files = []ConfigFile{{Path: "a.ini", Content: "{{home}}"}} },
		"not available on this kios": func(s *Spec) { s.Args = []string{"{{asset:ghost}}"} },
	}
	for want, mutate := range cases {
		spec := baseSpec()
		mutate(&spec)
		_, err := rig.mgr.Launch(spec)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: Launch = %v", want, err)
			continue
		}
		if strings.Contains(err.Error(), rig.root) {
			t.Errorf("%s: error leaks a path: %v", want, err)
		}
		if st := rig.mgr.Status(); strings.Contains(st.Error, rig.root) {
			t.Errorf("%s: status leaks a path: %+v", want, st)
		}
	}
	if ev := rig.rec.eventList(); len(ev) != 0 {
		t.Fatalf("observers told about sessions that never started: %v", ev)
	}
}

type padResolver struct{}

func (padResolver) VirtualPad(appID string, n int) (string, error) {
	return appID + "#" + string(rune('0'+n)), nil
}

func TestVirtualPadResolverIsPluggable(t *testing.T) {
	rig := newRig(t, "wait")
	rig.mgr.SetVirtualPads(padResolver{})
	spec := baseSpec()
	spec.Args = []string{"{{virtualPad:2}}"}
	spec.VirtualPads = []int{0, 1, 1}
	spec.Audio = AudioSpeaker
	st, err := rig.mgr.Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.VirtualPads) != 2 || st.Audio != AudioSpeaker {
		t.Fatalf("status = %+v", st)
	}
	rig.rec.mu.Lock()
	args := rig.rec.args
	rig.rec.mu.Unlock()
	if len(args) != 1 || args[0] != "dev.example.arcade#2" {
		t.Fatalf("args = %q", args)
	}
	rig.mgr.SetVirtualPads(nil)
	rig.mgr.Stop("", false)
	if _, err := rig.mgr.Launch(spec); !errors.Is(err, ErrVirtualPadsNotAttached) && (err == nil || !strings.Contains(err.Error(), "virtual pads not attached")) {
		t.Fatalf("Launch after detaching = %v", err)
	}
}
