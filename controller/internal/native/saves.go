package native

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Save-file backup (plan 08 item 5). An app declares which paths inside its
// data directory are "saves"; the platform zips them when the session ends,
// uploads the zip per account, and restores a newer backup before the next
// launch. Nothing here knows what the program is.

// Save states reported in Status.Saves.State.
const (
	SavesRestoring  = "restoring"
	SavesRestored   = "restored"     // a newer backup was restored before launch
	SavesUpToDate   = "up_to_date"   // local and backup already match
	SavesKeptLocal  = "kept_local"   // local data is newer than the backup, left alone
	SavesBackingUp  = "backing_up"   // uploading
	SavesSaved      = "saved"        // the backup is current
	SavesRemoteNewr = "remote_newer" // the account has a newer backup than this upload
	SavesNothing    = "nothing"      // nothing to back up or restore
	SavesOffline    = "offline"      // no account, nothing to sync with
	SavesError      = "error"
)

// SaveStatus is the `saves` field of Status. It never carries a path.
type SaveStatus struct {
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
	SavedAt string `json:"saved_at,omitempty"`
}

var (
	// ErrSavesOffline means there is no account to back up to.
	ErrSavesOffline = errors.New("saves: no active account")
	// ErrSaveRemoteNewer means the account holds a newer backup than the upload.
	ErrSaveRemoteNewer = errors.New("saves: the account has a newer backup")
	errNoSaves         = errors.New("no save data")
	// ErrNoSaveTarget means native.saves.backup had no session with saves.
	ErrNoSaveTarget = errors.New("no native session with saves to back up")
)

// SaveInfo describes the backup the account holds.
type SaveInfo struct {
	SHA256    string
	Size      int64
	SavedAt   time.Time
	KioskName string
}

// SaveUpload is one backup being sent.
type SaveUpload struct {
	Body    io.Reader
	Size    int64
	SHA256  string
	SavedAt time.Time
}

// SaveRemote is the server side of save backups. Implementations return
// ErrSavesOffline when there is no account and ErrSaveRemoteNewer when the
// server refuses an upload as older than what it holds.
type SaveRemote interface {
	SaveInfo(appID, runtime string) (SaveInfo, bool, error)
	SaveDownload(appID, runtime string) (io.ReadCloser, error)
	SaveUpload(appID, runtime string, up SaveUpload) error
}

// SaveLimits bound packing and restoring. Sizes are counted from the bytes
// actually read or written, never from archive headers.
type SaveLimits struct {
	MaxBytes        int64 // uncompressed bytes in a backup; default 256 MiB
	MaxFiles        int   // entries; default 20 000
	MaxArchiveBytes int64 // compressed zip; default 64 MiB (the server's cap)
}

func (l SaveLimits) withDefaults() SaveLimits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = 256 << 20
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = 20_000
	}
	if l.MaxArchiveBytes <= 0 {
		l.MaxArchiveBytes = 64 << 20
	}
	return l
}

const maxSavePaths = 32

// normalizeSaves validates declared save paths: relative to the data
// directory, forward slashes, no "..". Paths inside another declared path
// are dropped; the result is sorted.
func normalizeSaves(paths []string) ([]string, error) {
	if len(paths) > maxSavePaths {
		return nil, userErrorf("too many save paths")
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		rel, err := safeRelPath(p)
		if err != nil || rel == "" {
			return nil, userErrorf("save path %q must be relative to the data directory", p)
		}
		slashed := filepath.ToSlash(rel)
		if seen[slashed] {
			continue
		}
		seen[slashed] = true
		out = append(out, slashed)
	}
	sort.Strings(out)
	var kept []string
	for _, p := range out {
		nested := false
		for _, q := range kept {
			if strings.HasPrefix(p, q+"/") {
				nested = true
				break
			}
		}
		if !nested {
			kept = append(kept, p)
		}
	}
	return kept, nil
}

func underSaves(rel string, saves []string) bool {
	for _, p := range saves {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// symlinkInPath reports whether a component of rel below root (excluding the
// last) is a symlink.
func symlinkInPath(root, rel string) bool {
	parts := strings.Split(rel, "/")
	cur := root
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		if info, err := os.Lstat(cur); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// walkSaves visits the regular files and directories under the declared
// paths. Missing paths and symlinks are skipped.
func walkSaves(dataDir string, saves []string, fn func(rel, abs string, info fs.FileInfo) error) error {
	for _, p := range saves {
		if symlinkInPath(dataDir, p) {
			continue
		}
		root := filepath.Join(dataDir, filepath.FromSlash(p))
		info, err := os.Lstat(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				if err := fn(p, root, info); err != nil {
					return err
				}
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 || !(d.IsDir() || d.Type().IsRegular()) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dataDir, path)
			if err != nil {
				return err
			}
			return fn(filepath.ToSlash(rel), path, info)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// latestSaveTime is the newest modification time among the save files, to
// the second.
func latestSaveTime(dataDir string, saves []string) (time.Time, bool) {
	var latest time.Time
	found := false
	_ = walkSaves(dataDir, saves, func(_, _ string, info fs.FileInfo) error {
		if info.Mode().IsRegular() {
			found = true
			if t := info.ModTime(); t.After(latest) {
				latest = t
			}
		}
		return nil
	})
	return latest.UTC().Truncate(time.Second), found
}

// packSaves writes a zip of the declared paths to dst. The zip is
// deterministic for unchanged files, so its hash tells whether anything
// changed. savedAt is the newest file's modification time.
func packSaves(dataDir string, saves []string, dst io.Writer, limits SaveLimits) (savedAt time.Time, files int, err error) {
	limits = limits.withDefaults()
	zw := zip.NewWriter(dst)
	var total int64
	entries := 0
	err = walkSaves(dataDir, saves, func(rel, abs string, info fs.FileInfo) error {
		entries++
		if entries > limits.MaxFiles {
			return ErrTooManyFiles
		}
		header := &zip.FileHeader{Name: rel, Modified: info.ModTime().UTC().Truncate(time.Second)}
		if info.IsDir() {
			header.Name += "/"
			header.SetMode(fs.ModeDir | 0o755)
			_, err := zw.CreateHeader(header)
			return err
		}
		files++
		header.Method = zip.Deflate
		header.SetMode(0o644)
		if t := header.Modified; t.After(savedAt) {
			savedAt = t
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(abs)
		if err != nil {
			return err
		}
		defer f.Close()
		n, err := io.Copy(w, io.LimitReader(f, limits.MaxBytes-total+1))
		total += n
		if total > limits.MaxBytes {
			return ErrTooLarge
		}
		return err
	})
	if err != nil {
		return time.Time{}, 0, err
	}
	if files == 0 {
		return time.Time{}, 0, errNoSaves
	}
	if err := zw.Close(); err != nil {
		return time.Time{}, 0, err
	}
	return savedAt, files, nil
}

// restoreSaves unpacks zipPath over the declared paths of dataDir. Every
// entry is checked before anything is written; entries outside the declared
// paths are ignored. The files are extracted to a staging directory under
// stageRoot (same volume as dataDir) and swapped in per declared path.
func restoreSaves(zipPath, dataDir, stageRoot string, saves []string, limits SaveLimits) error {
	limits = limits.withDefaults()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptArchive, err)
	}
	defer zr.Close()
	if len(zr.File) > limits.MaxFiles {
		return ErrTooManyFiles
	}

	type entry struct {
		f   *zip.File
		rel string
	}
	var plan []entry
	for _, f := range zr.File {
		rel, err := safeRelPath(strings.TrimSuffix(f.Name, "/"))
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		mode := f.Mode()
		if mode&fs.ModeSymlink != 0 || !(mode.IsDir() || mode.IsRegular()) {
			return fmt.Errorf("%w: links and special files are not allowed", ErrUnsafePath)
		}
		if !underSaves(filepath.ToSlash(rel), saves) {
			continue
		}
		plan = append(plan, entry{f: f, rel: rel})
	}

	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(stageRoot, "restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	var total int64
	for _, e := range plan {
		target := filepath.Join(stage, e.rel)
		if e.f.Mode().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		n, err := extractSaveFile(e.f, target, limits.MaxBytes-total)
		total += n
		if err != nil {
			return err
		}
		_ = os.Chtimes(target, e.f.Modified, e.f.Modified)
	}

	for _, p := range saves {
		src := filepath.Join(stage, filepath.FromSlash(p))
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		if symlinkInPath(dataDir, p) {
			return fmt.Errorf("%w: save path crosses a link", ErrUnsafePath)
		}
		if err := swapIn(src, filepath.Join(dataDir, filepath.FromSlash(p))); err != nil {
			return err
		}
	}
	return nil
}

func extractSaveFile(f *zip.File, target string, remaining int64) (int64, error) {
	if remaining < 0 {
		remaining = 0
	}
	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrCorruptArchive, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, remaining+1))
	closeErr := out.Close()
	if n > remaining {
		return n, ErrTooLarge
	}
	if err != nil {
		return n, fmt.Errorf("%w: %v", ErrCorruptArchive, err)
	}
	return n, closeErr
}

// swapIn replaces dst with src, keeping the old one until the new is in place.
func swapIn(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	old := dst + ".yui-old"
	_ = os.RemoveAll(old)
	hadOld := false
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return err
		}
		hadOld = true
	}
	if err := os.Rename(src, dst); err != nil {
		if hadOld {
			_ = os.Rename(old, dst)
		}
		return err
	}
	if hadOld {
		_ = os.RemoveAll(old)
	}
	return nil
}

// saveMarker remembers which backup the local copy last matched, so an
// unchanged copy is neither re-uploaded nor re-downloaded.
type saveMarker struct {
	SHA256  string `json:"sha256"`
	SavedAt string `json:"saved_at"`
}

func markerPath(scopeDir, appID, runtime string) string {
	return filepath.Join(scopeDir, ".saves", appID+"-"+runtime+".json")
}

func readMarker(path string) saveMarker {
	var m saveMarker
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func writeMarker(path string, m saveMarker) error {
	data, _ := json.Marshal(m)
	return writeFileAtomic(path, data)
}

func hashFile(f *os.File) (string, int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func formatSaveTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
