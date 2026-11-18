package native

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallPrivateLinksFiles(t *testing.T) {
	src, data := t.TempDir(), t.TempDir()
	writeTree(t, src, map[string]string{"app.exe": "v1", "lib/a.dll": "a", runtimeMarker: "{}"})
	if err := os.MkdirAll(filepath.Join(src, "portable"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst, err := installPrivate(src, data, "asset1")
	if err != nil {
		t.Fatal(err)
	}
	if dst != filepath.Join(data, "runtime") {
		t.Fatalf("dst = %q", dst)
	}
	if readFile(t, filepath.Join(dst, "app.exe")) != "v1" || readFile(t, filepath.Join(dst, "lib", "a.dll")) != "a" {
		t.Fatal("files not placed")
	}
	if info, err := os.Stat(filepath.Join(dst, "portable")); err != nil || !info.IsDir() {
		t.Fatalf("empty folder not created: %v", err)
	}
	a, _ := os.Stat(filepath.Join(src, "app.exe"))
	b, _ := os.Stat(filepath.Join(dst, "app.exe"))
	if !os.SameFile(a, b) {
		t.Fatal("expected a hard link")
	}
	m, err := readInstallManifest(dst)
	if err != nil || m.AssetID != "asset1" || strings.Join(m.Files, ",") != "app.exe,lib/a.dll" {
		t.Fatalf("manifest = %+v, %v", m, err)
	}
}

func TestInstallPrivateCopiesWhenLinkFails(t *testing.T) {
	old := linkFile
	linkFile = func(string, string) error { return errors.New("cross-device") }
	defer func() { linkFile = old }()

	src, data := t.TempDir(), t.TempDir()
	writeTree(t, src, map[string]string{"app.exe": "v1"})
	dst, err := installPrivate(src, data, "asset1")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := os.Stat(filepath.Join(src, "app.exe"))
	b, err := os.Stat(filepath.Join(dst, "app.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(a, b) {
		t.Fatal("expected a copy")
	}
	if readFile(t, filepath.Join(dst, "app.exe")) != "v1" {
		t.Fatal("copy has wrong content")
	}
	if b.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable bit lost: %v", b.Mode())
	}
}

func TestInstallPrivateVersionReplaceKeepsForeignFiles(t *testing.T) {
	data := t.TempDir()
	v1, v2 := t.TempDir(), t.TempDir()
	writeTree(t, v1, map[string]string{"app.exe": "v1", "old.dll": "old", "keep/shared.txt": "one"})
	writeTree(t, v2, map[string]string{"app.exe": "v2", "new.dll": "new", "keep/shared.txt": "two"})
	dst, err := installPrivate(v1, data, "asset1")
	if err != nil {
		t.Fatal(err)
	}
	// The program and the app add their own files.
	writeTree(t, dst, map[string]string{"portable/settings.xml": "mine", "user.txt": "u"})

	if _, err := installPrivate(v2, data, "asset2"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "app.exe")); got != "v2" {
		t.Fatalf("app.exe = %q, want v2", got)
	}
	if got := readFile(t, filepath.Join(dst, "keep", "shared.txt")); got != "two" {
		t.Fatalf("shared.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "old.dll")); !os.IsNotExist(err) {
		t.Fatalf("old.dll should be removed: %v", err)
	}
	if readFile(t, filepath.Join(dst, "new.dll")) != "new" {
		t.Fatal("new.dll missing")
	}
	if readFile(t, filepath.Join(dst, "portable", "settings.xml")) != "mine" || readFile(t, filepath.Join(dst, "user.txt")) != "u" {
		t.Fatal("foreign files were touched")
	}
	// v1's source must not have been altered by the replace.
	if readFile(t, filepath.Join(v1, "app.exe")) != "v1" {
		t.Fatal("replace wrote through a hard link")
	}
}

func TestInstallPrivateSameAssetKeepsPlacedFiles(t *testing.T) {
	old := linkFile
	linkFile = func(string, string) error { return errors.New("no links") }
	defer func() { linkFile = old }()

	src, data := t.TempDir(), t.TempDir()
	writeTree(t, src, map[string]string{"app.exe": "v1", "b.dll": "b"})
	dst, err := installPrivate(src, data, "asset1")
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, dst, map[string]string{"app.exe": "edited"})
	if err := os.Remove(filepath.Join(dst, "b.dll")); err != nil {
		t.Fatal(err)
	}
	if _, err := installPrivate(src, data, "asset1"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "app.exe")); got != "edited" {
		t.Fatalf("app.exe = %q; a same-asset install must not overwrite", got)
	}
	if readFile(t, filepath.Join(dst, "b.dll")) != "b" {
		t.Fatal("missing file was not restored")
	}
}

func TestLaunchPrivateInstallRunsFromDataDir(t *testing.T) {
	rig := newRig(t, "wait")
	spec := baseSpec()
	spec.Install = InstallPrivate
	spec.Executable = "runtime/bin/game.exe"
	spec.Files = []ConfigFile{{Path: "runtime/portable/settings.xml", Content: "<x/>"}}
	spec.Saves = []string{"runtime/portable/save"}
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(rig.root, "data", "account-1", "dev.example.arcade")
	rig.rec.mu.Lock()
	exe := rig.rec.exe
	rig.rec.mu.Unlock()
	if exe != filepath.Join(dataDir, "runtime", "bin", "game.exe") {
		t.Fatalf("exe = %q", exe)
	}
	if readFile(t, filepath.Join(dataDir, "runtime", "portable", "settings.xml")) != "<x/>" {
		t.Fatal("config not written under runtime/")
	}
	// The shared extraction was not written to.
	shared := filepath.Join(rig.root, "runtimes", "emu", "1")
	if _, err := os.Stat(filepath.Join(shared, "portable")); !os.IsNotExist(err) {
		t.Fatalf("shared runtime was modified: %v", err)
	}
	rig.mgr.Stop("", false)
}

func TestLaunchPrivateInstallAcceptsBareExecutable(t *testing.T) {
	rig := newRig(t, "wait")
	spec := baseSpec()
	spec.Install = InstallPrivate
	spec.Executable = "bin/game.exe"
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	rig.rec.mu.Lock()
	exe := rig.rec.exe
	rig.rec.mu.Unlock()
	if !strings.HasSuffix(filepath.ToSlash(exe), "/runtime/bin/game.exe") {
		t.Fatalf("exe = %q", exe)
	}
	rig.mgr.Stop("", false)
}

func TestNormalizeSpecInstallAndSurfaces(t *testing.T) {
	ok := func(mutate func(*Spec)) Spec {
		s := baseSpec()
		mutate(&s)
		if err := normalizeSpec(&s); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return s
	}
	if s := ok(func(*Spec) {}); s.Install != InstallShared {
		t.Fatalf("default install = %q", s.Install)
	}
	ok(func(s *Spec) { s.Install = "private" })
	ok(func(s *Spec) {
		s.Surfaces = []SurfaceSpec{
			{ID: "second-screen", Title: "(?i)pad", Hidden: true, Width: 854, Height: 480},
			{ID: "b", Title: "x"},
		}
	})

	long := strings.Repeat("a", 201)
	bad := map[string]func(*Spec){
		"install must be": func(s *Spec) { s.Install = "global" },
		"at most 2 surfaces": func(s *Spec) {
			s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x"}, {ID: "b", Title: "x"}, {ID: "c", Title: "x"}}
		},
		"invalid surface id":  func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "Bad_ID", Title: "x"}} },
		"empty id":            func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "", Title: "x"}} },
		"id too long":         func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: strings.Repeat("a", 33), Title: "x"}} },
		"listed twice":        func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x"}, {ID: "a", Title: "y"}} },
		"not a valid pattern": func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "("}} },
		"title must be":       func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: long}} },
		"empty title":         func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: ""}} },
		"odd width":           func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x", Width: 853, Height: 480}} },
		"width too small":     func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x", Width: 158, Height: 480}} },
		"width too large":     func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x", Width: 1922, Height: 480}} },
		"height too small":    func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x", Width: 854, Height: 118}} },
		"height too large":    func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x", Width: 854, Height: 1082}} },
		"odd height":          func(s *Spec) { s.Surfaces = []SurfaceSpec{{ID: "a", Title: "x", Width: 854, Height: 481}} },
	}
	for name, mutate := range bad {
		s := baseSpec()
		mutate(&s)
		var ue *userError
		if err := normalizeSpec(&s); !errors.As(err, &ue) {
			t.Errorf("%s: normalizeSpec = %v, want a user error", name, err)
		}
	}
}

func TestInfoCarriesSurfaces(t *testing.T) {
	rig := newRig(t, "wait")
	var got []SurfaceSpec
	rig.mgr.AddObserver(observerFunc(func(info Info) { got = info.Surfaces }))
	spec := baseSpec()
	spec.Surfaces = []SurfaceSpec{{ID: "screen", Title: "(?i)pad", Hidden: true}}
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "screen" {
		t.Fatalf("Info.Surfaces = %+v", got)
	}
	rig.mgr.Stop("", false)
}

type observerFunc func(Info)

func (f observerFunc) SessionStarted(i Info)   { f(i) }
func (observerFunc) SessionEnded(Info, Status) {}

type giveCall struct {
	pid  int
	skip []string
}

type giveRecorder struct{ calls chan giveCall }

func (giveRecorder) Capture() FocusToken { return 0 }
func (g giveRecorder) Give(pid int, skip []*regexp.Regexp, _ <-chan struct{}) {
	var s []string
	for _, re := range skip {
		s = append(s, re.String())
	}
	g.calls <- giveCall{pid, s}
}
func (giveRecorder) Restore(FocusToken) {}

func TestGivePassesSurfaceTitles(t *testing.T) {
	rig := newRig(t, "wait")
	g := giveRecorder{calls: make(chan giveCall, 1)}
	rig.mgr.cfg.Focus = g
	spec := baseSpec()
	spec.Surfaces = []SurfaceSpec{{ID: "screen", Title: "(?i)pad"}}
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	call := <-g.calls
	if call.pid == 0 || len(call.skip) != 1 || call.skip[0] != "(?i)pad" {
		t.Fatalf("Give = %+v", call)
	}
	rig.mgr.Stop("", false)
}

func TestPickWindowSkipsSurfaces(t *testing.T) {
	skip := []*regexp.Regexp{regexp.MustCompile(`(?i)second view`)}
	wins := []titledWindow{{1, "Second View"}, {2, "Program"}, {3, "Other"}}
	w, ok := pickWindow(wins, skip)
	if !ok || w.id != 2 {
		t.Fatalf("pickWindow = %+v, %v", w, ok)
	}
	if _, ok := pickWindow(wins[:1], skip); ok {
		t.Fatal("only a surface window left: nothing to pick")
	}
	if w, ok := pickWindow(wins, nil); !ok || w.id != 1 {
		t.Fatalf("no skip patterns: %+v %v", w, ok)
	}
}
