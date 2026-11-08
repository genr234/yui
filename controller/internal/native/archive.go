package native

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
)

// Archive formats recognised by DetectFormat.
const (
	FormatZip      = "zip"
	FormatSevenZip = "7z"
)

var (
	// ErrUnsafePath means an archive entry would land outside the destination.
	ErrUnsafePath = errors.New("archive entry escapes the destination")
	// ErrTooLarge means the archive expands past Limits.MaxBytes.
	ErrTooLarge = errors.New("archive expands beyond the size limit")
	// ErrTooManyFiles means the archive has more entries than Limits.MaxFiles.
	ErrTooManyFiles = errors.New("archive has too many entries")
	// ErrCorruptArchive means the archive reader failed on malformed data.
	ErrCorruptArchive = errors.New("runtime archive is corrupt")
	// ErrUnknownFormat means the file is neither a zip nor a 7z archive.
	ErrUnknownFormat = errors.New("runtime archive is not a .zip or .7z file")
)

// Limits bound what an extraction may write. Sizes are counted from the
// bytes actually decompressed, not from the archive's headers.
type Limits struct {
	MaxBytes int64 // total bytes written; default 8 GiB
	MaxFiles int   // entries (files and directories); default 200 000
}

const (
	defaultMaxBytes = int64(8) << 30
	defaultMaxFiles = 200_000
)

func (l Limits) withDefaults() Limits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = defaultMaxBytes
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = defaultMaxFiles
	}
	return l
}

var (
	zipMagic      = []byte("PK\x03\x04")
	zipEmptyMagic = []byte("PK\x05\x06")
	sevenZipMagic = []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}
)

// DetectFormat reads the archive's magic bytes.
func DetectFormat(archivePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 8)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, zipMagic), bytes.HasPrefix(head, zipEmptyMagic):
		return FormatZip, nil
	case bytes.HasPrefix(head, sevenZipMagic):
		return FormatSevenZip, nil
	}
	return "", ErrUnknownFormat
}

type archiveEntry struct {
	name string
	mode fs.FileMode
	open func() (io.ReadCloser, error)
}

// Extract unpacks a .zip or .7z archive into dest. It extracts into a
// sibling temporary directory and renames it into place, so dest either
// does not exist or holds a complete extraction. dest must not exist yet.
func Extract(archivePath, dest string, limits Limits) (err error) {
	// A malformed archive must not take the controller down with it; the
	// 7z reader can panic on corrupt headers.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrCorruptArchive, r)
		}
	}()
	limits = limits.withDefaults()
	format, err := DetectFormat(archivePath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("extract: destination already exists")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".partial-")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tmp)
		}
	}()

	switch format {
	case FormatZip:
		zr, err := zip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer zr.Close()
		entries := make([]archiveEntry, 0, len(zr.File))
		for _, f := range zr.File {
			entries = append(entries, archiveEntry{name: f.Name, mode: f.Mode(), open: f.Open})
		}
		err = extractEntries(entries, tmp, limits)
		if err != nil {
			return err
		}
	case FormatSevenZip:
		sr, err := sevenzip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer sr.Close()
		entries := make([]archiveEntry, 0, len(sr.File))
		for _, f := range sr.File {
			entries = append(entries, archiveEntry{name: f.Name, mode: f.Mode(), open: f.Open})
		}
		err = extractEntries(entries, tmp, limits)
		if err != nil {
			return err
		}
	}

	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	ok = true
	return nil
}

func extractEntries(entries []archiveEntry, dest string, limits Limits) error {
	if len(entries) > limits.MaxFiles {
		return ErrTooManyFiles
	}
	remaining := limits.MaxBytes
	for _, e := range entries {
		rel, err := safeRelPath(e.name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, rel)
		switch {
		case e.mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case e.mode.IsRegular():
			written, err := extractFile(e, target, remaining)
			if err != nil {
				return err
			}
			remaining -= written
		default:
			// Symlinks, devices, and other special entries are skipped: a
			// symlink could point a later entry outside the destination.
			continue
		}
	}
	return nil
}

func extractFile(e archiveEntry, target string, remaining int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	rc, err := e.open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	perm := fs.FileMode(0o644)
	if e.mode.Perm()&0o111 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return 0, err
	}
	// Read one byte past the budget so an over-limit entry is detected
	// without trusting the size the archive claims.
	written, copyErr := io.Copy(out, io.LimitReader(rc, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	if written > remaining {
		return written, ErrTooLarge
	}
	return written, nil
}

// safeRelPath turns an archive entry name into a relative OS path inside the
// destination. It returns "" for the root entry and ErrUnsafePath for names
// that are absolute, carry a drive or stream (":"), or climb out with "..".
func safeRelPath(name string) (string, error) {
	if strings.ContainsRune(name, 0) {
		return "", ErrUnsafePath
	}
	slashed := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(slashed, "/") || strings.Contains(slashed, ":") {
		return "", ErrUnsafePath
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", ErrUnsafePath
		}
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == "" {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", ErrUnsafePath
	}
	rel := filepath.FromSlash(clean)
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || !filepath.IsLocal(rel) {
		return "", ErrUnsafePath
	}
	return rel, nil
}
