package native

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// focusLog is a Focuser that records what it was asked to do.
type focusLog struct {
	mu    sync.Mutex
	calls []string
}

func (f *focusLog) Capture() FocusToken { return 42 }

func (f *focusLog) Give(pid int, _ []*regexp.Regexp, _ <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "game")
}

func (f *focusLog) Restore(t FocusToken) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t == 42 {
		f.calls = append(f.calls, "page")
	} else {
		f.calls = append(f.calls, "?")
	}
}

func (f *focusLog) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		got := strings.Join(f.calls, ",")
		f.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Fatalf("focus moves = %v, want %s", f.calls, want)
}

func overlayRig(t *testing.T) (*Manager, *focusLog) {
	t.Helper()
	root := t.TempDir()
	archive := filepath.Join(root, "rt.zip")
	writeZip(t, archive, runtimeFixture)
	focus := &focusLog{}
	mgr := NewManager(Config{
		RuntimesDir: filepath.Join(root, "runtimes"),
		DataDir:     filepath.Join(root, "data"),
		LogDir:      filepath.Join(root, "logs"),
		Assets:      fakeAssets{"rt1": {ID: "rt1", Kind: AssetKindRuntime, Path: archive, Metadata: []byte(`{"runtime":"emu","version":"1"}`)}},
		Focus:       focus,
		StopGrace:   300 * time.Millisecond,
		Command: func(string, []string) *exec.Cmd {
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "YUI_NATIVE_HELPER=wait")
			return cmd
		},
	})
	t.Cleanup(func() { mgr.Stop("", true) })
	return mgr, focus
}

func TestShowOverlayLiftsThePageOverTheGameAndBack(t *testing.T) {
	mgr, focus := overlayRig(t)
	spec := Spec{AppID: "dev.example.wiiu", Runtime: "rt1", Executable: "bin/game.exe"}
	if _, err := mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	focus.waitFor(t, "game") // the game takes the front when it starts
	mgr.ShowOverlay(true)
	focus.waitFor(t, "game,page")
	mgr.ShowOverlay(true) // already up: nothing more
	mgr.ShowOverlay(false)
	focus.waitFor(t, "game,page,game")
	mgr.ShowOverlay(false)
	time.Sleep(50 * time.Millisecond)
	focus.waitFor(t, "game,page,game")
}

func TestAGameStartingMidCalibrationGoesUnderThePage(t *testing.T) {
	mgr, focus := overlayRig(t)
	mgr.ShowOverlay(true) // nothing running: only remembered
	spec := Spec{AppID: "dev.example.wiiu", Runtime: "rt1", Executable: "bin/game.exe"}
	if _, err := mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	focus.waitFor(t, "game,page")
	if _, err := mgr.Stop("", false); err != nil {
		t.Fatal(err)
	}
	// The session ending hands focus back to the page, as always.
	focus.waitFor(t, "game,page,page")
}

// keeperLog is a focusLog that also keeps the game in front (FocusKeeper).
type keeperLog struct{ focusLog }

func (k *keeperLog) Hold(_ int, _ []*regexp.Regexp, stop, release <-chan struct{}) {
	k.mu.Lock()
	k.calls = append(k.calls, "hold")
	k.mu.Unlock()
	select {
	case <-stop:
	case <-release:
	}
	k.mu.Lock()
	k.calls = append(k.calls, "release")
	k.mu.Unlock()
}

func TestTheGameIsHeldInFrontExceptUnderThePage(t *testing.T) {
	mgr, _ := overlayRig(t)
	keeper := &keeperLog{}
	mgr.cfg.Focus = keeper
	spec := Spec{AppID: "dev.example.wiiu", Runtime: "rt1", Executable: "bin/game.exe"}
	if _, err := mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	keeper.waitFor(t, "game,hold")
	// Calibration lifts the page: the hold lets go first.
	mgr.ShowOverlay(true)
	keeper.waitFor(t, "game,hold,release,page")
	mgr.ShowOverlay(false)
	keeper.waitFor(t, "game,hold,release,page,game,hold")
	// The game ending releases the hold before focus goes back to the page.
	if _, err := mgr.Stop("", false); err != nil {
		t.Fatal(err)
	}
	keeper.waitFor(t, "game,hold,release,page,game,hold,release,page")
}
