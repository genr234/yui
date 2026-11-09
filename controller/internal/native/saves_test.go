package native

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeSaveFile(t *testing.T, dir, rel, body string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func readSaveFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var saveEpoch = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func TestNormalizeSaves(t *testing.T) {
	got, err := normalizeSaves([]string{"cards", "profile/slots", "profile/slots/00010000", "cards", `Dir\Sub`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "Dir/Sub,cards,profile/slots" {
		t.Fatalf("normalized = %v", got)
	}
	for _, bad := range []string{"../x", "a/../../x", "/abs", "C:/x", "", ".", "a:b"} {
		if _, err := normalizeSaves([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := normalizeSaves(make([]string, maxSavePaths+1)); err == nil {
		t.Error("too many paths accepted")
	}
}

func TestPackAndRestoreRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeSaveFile(t, src, "profile/slots/a.sav", "alpha", saveEpoch)
	writeSaveFile(t, src, "profile/slots/sub/b.sav", "beta", saveEpoch.Add(time.Hour))
	writeSaveFile(t, src, "cards/card.raw", "gamma", saveEpoch.Add(-time.Hour))
	writeSaveFile(t, src, "Config/not-a-save.ini", "nope", saveEpoch)
	saves := []string{"cards", "profile/slots", "Missing/path"}

	var buf bytes.Buffer
	savedAt, files, err := packSaves(src, saves, &buf, SaveLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if files != 3 || !savedAt.Equal(saveEpoch.Add(time.Hour)) {
		t.Fatalf("files=%d savedAt=%v", files, savedAt)
	}
	var again bytes.Buffer
	if _, _, err := packSaves(src, saves, &again, SaveLimits{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), again.Bytes()) {
		t.Fatal("packing unchanged files is not deterministic")
	}

	archive := filepath.Join(t.TempDir(), "saves.zip")
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	writeSaveFile(t, dst, "profile/slots/stale.sav", "stale", saveEpoch)
	writeSaveFile(t, dst, "Config/keep.ini", "keep", saveEpoch)
	if err := restoreSaves(archive, dst, filepath.Join(t.TempDir(), "stage"), saves, SaveLimits{}); err != nil {
		t.Fatal(err)
	}
	if readSaveFile(t, dst, "profile/slots/a.sav") != "alpha" || readSaveFile(t, dst, "profile/slots/sub/b.sav") != "beta" || readSaveFile(t, dst, "cards/card.raw") != "gamma" {
		t.Fatal("restored files differ")
	}
	if _, err := os.Stat(filepath.Join(dst, "profile/slots/stale.sav")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stale file survived the restore: %v", err)
	}
	if readSaveFile(t, dst, "Config/keep.ini") != "keep" {
		t.Fatal("restore touched a path outside the saves")
	}
	if latest, _ := latestSaveTime(dst, saves); !latest.Equal(savedAt) {
		t.Fatalf("restored mtime = %v, want %v", latest, savedAt)
	}
}

type zipMember struct {
	name string
	body string
	mode fs.FileMode
}

func makeZip(t *testing.T, members ...zipMember) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range members {
		h := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
		if m.mode != 0 {
			h.SetMode(m.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestoreRejectsUnsafeArchives(t *testing.T) {
	saves := []string{"saves"}
	cases := map[string][]zipMember{
		"traversal":    {{name: "saves/ok.sav", body: "ok"}, {name: "../escape.txt", body: "x"}},
		"nested climb": {{name: "saves/../../escape.txt", body: "x"}},
		"absolute":     {{name: "/etc/escape.txt", body: "x"}},
		"backslash":    {{name: `saves\..\..\escape.txt`, body: "x"}},
		"symlink":      {{name: "saves/link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777}},
	}
	for name, members := range cases {
		t.Run(name, func(t *testing.T) {
			dst := t.TempDir()
			writeSaveFile(t, dst, "saves/old.sav", "old", saveEpoch)
			parent := filepath.Dir(dst)
			err := restoreSaves(makeZip(t, members...), dst, filepath.Join(t.TempDir(), "stage"), saves, SaveLimits{})
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("err = %v, want ErrUnsafePath", err)
			}
			if readSaveFile(t, dst, "saves/old.sav") != "old" {
				t.Fatal("local data changed by a rejected restore")
			}
			if _, err := os.Stat(filepath.Join(dst, "saves/ok.sav")); err == nil {
				t.Fatal("part of a rejected archive was written")
			}
			if _, err := os.Stat(filepath.Join(parent, "escape.txt")); err == nil {
				t.Fatal("entry escaped the data directory")
			}
		})
	}
}

func TestRestoreIgnoresEntriesOutsideDeclaredPaths(t *testing.T) {
	dst := t.TempDir()
	archive := makeZip(t, zipMember{name: "saves/a.sav", body: "a"}, zipMember{name: "Config/x.ini", body: "x"})
	if err := restoreSaves(archive, dst, filepath.Join(t.TempDir(), "stage"), []string{"saves"}, SaveLimits{}); err != nil {
		t.Fatal(err)
	}
	if readSaveFile(t, dst, "saves/a.sav") != "a" {
		t.Fatal("declared entry missing")
	}
	if _, err := os.Stat(filepath.Join(dst, "Config/x.ini")); err == nil {
		t.Fatal("undeclared entry was extracted")
	}
}

func TestRestoreEnforcesCaps(t *testing.T) {
	saves := []string{"saves"}
	big := strings.Repeat("A", 4096) // compresses to almost nothing
	err := restoreSaves(makeZip(t, zipMember{name: "saves/big.sav", body: big}), t.TempDir(), t.TempDir(), saves, SaveLimits{MaxBytes: 1024})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size cap: err = %v", err)
	}
	members := []zipMember{}
	for i := 0; i < 5; i++ {
		members = append(members, zipMember{name: "saves/f" + string(rune('a'+i)), body: "x"})
	}
	err = restoreSaves(makeZip(t, members...), t.TempDir(), t.TempDir(), saves, SaveLimits{MaxFiles: 3})
	if !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("file cap: err = %v", err)
	}
}

func TestPackSkipsSymlinksAndEnforcesCap(t *testing.T) {
	src := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSaveFile(t, src, "saves/a.sav", "a", saveEpoch)
	if err := os.Symlink(outside, filepath.Join(src, "saves", "link.sav")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(src, "linked")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_, files, err := packSaves(src, []string{"saves", "linked"}, &buf, SaveLimits{})
	if err != nil || files != 1 {
		t.Fatalf("files=%d err=%v", files, err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	for _, f := range zr.File {
		if strings.Contains(f.Name, "link") || strings.Contains(f.Name, "secret") {
			t.Fatalf("symlink target packed: %s", f.Name)
		}
	}

	writeSaveFile(t, src, "saves/huge.sav", strings.Repeat("B", 2048), saveEpoch)
	if _, _, err := packSaves(src, []string{"saves"}, io.Discard, SaveLimits{MaxBytes: 1024}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("pack cap: err = %v", err)
	}
	if _, _, err := packSaves(src, []string{"nothing"}, io.Discard, SaveLimits{}); !errors.Is(err, errNoSaves) {
		t.Fatalf("empty pack: err = %v", err)
	}
}

// fakeSaves is an in-memory SaveRemote.
type fakeSaves struct {
	mu       sync.Mutex
	blob     []byte
	info     *SaveInfo
	offline  bool
	infoErr  error
	uploads  int
	lastSave time.Time
}

func (f *fakeSaves) SaveInfo(appID, runtime string) (SaveInfo, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offline {
		return SaveInfo{}, false, ErrSavesOffline
	}
	if f.infoErr != nil {
		return SaveInfo{}, false, f.infoErr
	}
	if f.info == nil {
		return SaveInfo{}, false, nil
	}
	return *f.info, true, nil
}

func (f *fakeSaves) SaveDownload(appID, runtime string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return io.NopCloser(bytes.NewReader(f.blob)), nil
}

func (f *fakeSaves) SaveUpload(appID, runtime string, up SaveUpload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offline {
		return ErrSavesOffline
	}
	data, err := io.ReadAll(up.Body)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != up.SHA256 || int64(len(data)) != up.Size {
		return errors.New("upload does not match its declared hash")
	}
	if f.info != nil && f.info.SavedAt.After(up.SavedAt) {
		return ErrSaveRemoteNewer
	}
	f.blob = data
	f.info = &SaveInfo{SHA256: up.SHA256, Size: up.Size, SavedAt: up.SavedAt}
	f.uploads++
	f.lastSave = up.SavedAt
	return nil
}

func (f *fakeSaves) seed(t *testing.T, files map[string]string, mtime time.Time) {
	t.Helper()
	src := t.TempDir()
	for rel, body := range files {
		writeSaveFile(t, src, rel, body, mtime)
	}
	var buf bytes.Buffer
	savedAt, _, err := packSaves(src, []string{"saves"}, &buf, SaveLimits{})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	f.blob = buf.Bytes()
	f.info = &SaveInfo{SHA256: hex.EncodeToString(sum[:]), Size: int64(buf.Len()), SavedAt: savedAt}
}

func savesRig(t *testing.T, mode string, remote *fakeSaves) (*testRig, Spec) {
	t.Helper()
	rig := newRig(t, mode)
	rig.mgr.cfg.SaveRemote = remote
	spec := baseSpec()
	spec.Saves = []string{"saves"}
	return rig, spec
}

func (r *testRig) dataDir() string {
	return filepath.Join(r.root, "data", "account-1", "dev.example.arcade")
}

func waitSavesState(t *testing.T, m *Manager, want string) SaveStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := m.Status(); st.Saves != nil && st.Saves.State == want {
			return *st.Saves
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("saves never reached %q; status = %+v", want, m.Status().Saves)
	return SaveStatus{}
}

func TestLaunchRestoresNewerBackup(t *testing.T) {
	remote := &fakeSaves{}
	remote.seed(t, map[string]string{"saves/slot1.sav": "from-cloud"}, saveEpoch)
	rig, spec := savesRig(t, "wait", remote)
	writeSaveFile(t, rig.dataDir(), "saves/slot1.sav", "old-local", saveEpoch.Add(-24*time.Hour))

	st, err := rig.mgr.Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateRunning {
		t.Fatalf("state = %s", st.State)
	}
	if got := readSaveFile(t, rig.dataDir(), "saves/slot1.sav"); got != "from-cloud" {
		t.Fatalf("save = %q, want the restored backup", got)
	}
	if s := rig.mgr.Status().Saves; s == nil || s.State != SavesRestored || s.SavedAt == "" {
		t.Fatalf("saves status = %+v", s)
	}
	if _, err := rig.mgr.Stop("", true); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchKeepsNewerLocalData(t *testing.T) {
	remote := &fakeSaves{}
	remote.seed(t, map[string]string{"saves/slot1.sav": "from-cloud"}, saveEpoch)
	rig, spec := savesRig(t, "wait", remote)
	writeSaveFile(t, rig.dataDir(), "saves/slot1.sav", "fresh-local", saveEpoch.Add(24*time.Hour))

	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	if got := readSaveFile(t, rig.dataDir(), "saves/slot1.sav"); got != "fresh-local" {
		t.Fatalf("save = %q, local newer data was overwritten", got)
	}
	if s := rig.mgr.Status().Saves; s == nil || s.State != SavesKeptLocal {
		t.Fatalf("saves status = %+v", s)
	}
	_, _ = rig.mgr.Stop("", true)
}

func TestLaunchRestoresIntoEmptyDataDirOnce(t *testing.T) {
	remote := &fakeSaves{}
	remote.seed(t, map[string]string{"saves/slot1.sav": "from-cloud"}, saveEpoch)
	rig, spec := savesRig(t, "exit0", remote)

	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	rig.rec.waitState(t, StateExited)
	waitSavesState(t, rig.mgr, SavesSaved)
	if readSaveFile(t, rig.dataDir(), "saves/slot1.sav") != "from-cloud" {
		t.Fatal("not restored into an empty data directory")
	}
	if remote.uploads != 0 {
		t.Fatalf("unchanged restored data was re-uploaded (%d uploads)", remote.uploads)
	}
	// Second launch: the marker says local matches the backup.
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	if s := rig.mgr.Status().Saves; s == nil || (s.State != SavesUpToDate && s.State != SavesSaved) {
		t.Fatalf("saves status = %+v", s)
	}
}

func TestSessionEndBacksUpSaves(t *testing.T) {
	remote := &fakeSaves{}
	rig, spec := savesRig(t, "exit0", remote)
	writeSaveFile(t, rig.dataDir(), "saves/slot1.sav", "progress", saveEpoch)
	writeSaveFile(t, rig.dataDir(), "user/Config/main.ini", "not saved", saveEpoch)

	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	rig.rec.waitState(t, StateExited)
	s := waitSavesState(t, rig.mgr, SavesSaved)
	if s.SavedAt != saveEpoch.Format(time.RFC3339) {
		t.Fatalf("saved_at = %q", s.SavedAt)
	}
	remote.mu.Lock()
	zr, err := zip.NewReader(bytes.NewReader(remote.blob), int64(len(remote.blob)))
	remote.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "saves/,saves/slot1.sav" {
		t.Fatalf("backup entries = %v", names)
	}

	// Same content again: nothing is uploaded twice.
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	waitSavesState(t, rig.mgr, SavesSaved)
	rig.mgr.waitSaves(5 * time.Second)
	if remote.uploads != 1 {
		t.Fatalf("uploads = %d, want 1", remote.uploads)
	}
}

func TestExplicitBackupWhileRunning(t *testing.T) {
	remote := &fakeSaves{}
	rig, spec := savesRig(t, "wait", remote)
	writeSaveFile(t, rig.dataDir(), "saves/slot1.sav", "progress", saveEpoch)
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.mgr.BackupSaves("someone.else"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("foreign backup: err = %v", err)
	}
	st, err := rig.mgr.BackupSaves("dev.example.arcade")
	if err != nil {
		t.Fatal(err)
	}
	if st.Saves == nil || st.Saves.State != SavesSaved || remote.uploads != 1 {
		t.Fatalf("status = %+v uploads=%d", st.Saves, remote.uploads)
	}
	_, _ = rig.mgr.Stop("", true)
}

func TestBackupWithoutSavesIsAnError(t *testing.T) {
	rig := newRig(t, "exit0")
	if _, err := rig.mgr.BackupSaves(""); !errors.Is(err, ErrNoSaveTarget) {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveErrorsNeverBlockLaunchOrStop(t *testing.T) {
	remote := &fakeSaves{infoErr: errors.New("boom: dial tcp 10.0.0.1")}
	rig, spec := savesRig(t, "wait", remote)
	writeSaveFile(t, rig.dataDir(), "saves/slot1.sav", "progress", saveEpoch)

	st, err := rig.mgr.Launch(spec)
	if err != nil || st.State != StateRunning {
		t.Fatalf("launch: %+v err=%v", st, err)
	}
	s := rig.mgr.Status().Saves
	if s == nil || s.State != SavesError || s.Error == "" {
		t.Fatalf("saves status = %+v", s)
	}
	remote.mu.Lock()
	remote.infoErr = nil
	remote.offline = true
	remote.mu.Unlock()
	if st, err := rig.mgr.Stop("", false); err != nil || st.State != StateExited {
		t.Fatalf("stop: %+v err=%v", st, err)
	}
	waitSavesState(t, rig.mgr, SavesOffline)
}

func TestSavesRemoteNewerIsNotAnError(t *testing.T) {
	remote := &fakeSaves{}
	rig, spec := savesRig(t, "exit0", remote)
	writeSaveFile(t, rig.dataDir(), "saves/slot1.sav", "progress", saveEpoch)
	remote.mu.Lock()
	remote.info = &SaveInfo{SHA256: "x", SavedAt: saveEpoch.Add(time.Hour)}
	remote.mu.Unlock()
	// Local is older than the (unreadable) remote backup, so launch tries to
	// restore and fails on the checksum; the end-of-session upload is refused
	// as older. Neither blocks anything.
	if _, err := rig.mgr.Launch(spec); err != nil {
		t.Fatal(err)
	}
	rig.rec.waitState(t, StateExited)
	s := waitSavesState(t, rig.mgr, SavesRemoteNewr)
	if s.Error != "" {
		t.Fatalf("remote-newer carried an error: %+v", s)
	}
}

func TestInvalidSavePathsRejectLaunch(t *testing.T) {
	rig := newRig(t, "exit0")
	spec := baseSpec()
	spec.Saves = []string{"../outside"}
	if _, err := rig.mgr.Launch(spec); err == nil {
		t.Fatal("launch with an unsafe save path succeeded")
	}
}
