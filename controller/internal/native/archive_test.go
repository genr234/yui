package native

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var runtimeFixture = []fixtureEntry{
	{name: "bin/", dir: true},
	{name: "bin/game.exe", body: "#!/bin/sh\necho game\n", mode: 0o755},
	{name: "share/data/readme.txt", body: "hello runtime"},
}

func checkRuntimeTree(t *testing.T, dir string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "share", "data", "readme.txt"))
	if err != nil || string(got) != "hello runtime" {
		t.Fatalf("readme = %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "bin", "game.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable bit lost: %v", info.Mode())
	}
	if info, err := os.Stat(filepath.Join(dir, "bin")); err != nil || !info.IsDir() {
		t.Fatalf("bin is not a directory: %v", err)
	}
}

func TestExtractZip(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "rt.zip")
	writeZip(t, archive, runtimeFixture)
	if f, err := DetectFormat(archive); err != nil || f != FormatZip {
		t.Fatalf("DetectFormat = %q, %v", f, err)
	}
	dest := filepath.Join(tmp, "out", "rt", "1")
	if err := Extract(archive, dest, Limits{}); err != nil {
		t.Fatal(err)
	}
	checkRuntimeTree(t, dest)
}

func TestExtract7z(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "rt.7z")
	write7z(t, archive, runtimeFixture)
	if f, err := DetectFormat(archive); err != nil || f != FormatSevenZip {
		t.Fatalf("DetectFormat = %q, %v", f, err)
	}
	dest := filepath.Join(tmp, "out", "rt", "1")
	if err := Extract(archive, dest, Limits{}); err != nil {
		t.Fatal(err)
	}
	checkRuntimeTree(t, dest)
}

func TestExtractRejectsZipSlip(t *testing.T) {
	for _, name := range []string{
		"../evil.txt",
		"bin/../../evil.txt",
		"/abs/evil.txt",
		"..\\evil.txt",
		"C:/evil.txt",
		"C:evil.txt",
		"bin/file.txt:stream",
	} {
		t.Run(name, func(t *testing.T) {
			tmp := t.TempDir()
			for _, write := range []func(*testing.T, string, []fixtureEntry){writeZip, write7z} {
				archive := filepath.Join(tmp, "evil.archive")
				write(t, archive, []fixtureEntry{
					{name: "ok.txt", body: "fine"},
					{name: name, body: "pwned"},
				})
				dest := filepath.Join(tmp, "nested", "dest")
				err := Extract(archive, dest, Limits{})
				if !errors.Is(err, ErrUnsafePath) {
					t.Fatalf("Extract(%q) = %v, want ErrUnsafePath", name, err)
				}
				if _, err := os.Stat(dest); !os.IsNotExist(err) {
					t.Fatalf("destination exists after a rejected archive: %v", err)
				}
				if _, err := os.Stat(filepath.Join(tmp, "evil.txt")); !os.IsNotExist(err) {
					t.Fatal("entry escaped the destination")
				}
				leftovers, _ := filepath.Glob(filepath.Join(tmp, "nested", ".dest.partial-*"))
				if len(leftovers) != 0 {
					t.Fatalf("partial extraction left behind: %v", leftovers)
				}
			}
		})
	}
}

func TestExtractStopsAtSizeLimit(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "bomb.zip")
	// Zeros deflate to almost nothing; the limit counts what is written.
	writeZip(t, archive, []fixtureEntry{
		{name: "a.bin", body: strings.Repeat("\x00", 600)},
		{name: "b.bin", body: strings.Repeat("\x00", 600)},
	})
	dest := filepath.Join(tmp, "dest")
	if err := Extract(archive, dest, Limits{MaxBytes: 1000}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Extract = %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("destination exists after an over-limit archive")
	}
	if err := Extract(archive, dest, Limits{MaxBytes: 1200}); err != nil {
		t.Fatalf("archive at exactly the limit: %v", err)
	}
}

func TestExtractStopsAtFileLimit(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "many.7z")
	write7z(t, archive, []fixtureEntry{{name: "a", body: "1"}, {name: "b", body: "2"}, {name: "c", body: "3"}})
	if err := Extract(archive, filepath.Join(tmp, "dest"), Limits{MaxFiles: 2}); !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("Extract = %v, want ErrTooManyFiles", err)
	}
}

func TestExtractCorrupt7zIsAnError(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "bad.7z")
	write7z(t, archive, runtimeFixture)
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the signature, scramble everything after it.
	for i := 12; i < len(data); i++ {
		data[i] ^= 0x5A
	}
	if err := os.WriteFile(archive, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmp, "dest")
	if err := Extract(archive, dest, Limits{}); err == nil {
		t.Fatal("corrupt archive extracted")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("destination exists after a corrupt archive")
	}
}

func TestExtractUnknownFormat(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "x.rar")
	if err := os.WriteFile(archive, []byte("Rar!\x1a\x07\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(archive, filepath.Join(tmp, "dest"), Limits{}); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("Extract = %v, want ErrUnknownFormat", err)
	}
}

func TestRuntimesExtractOnceAndReplaceOnNewAsset(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "dolphin.zip")
	writeZip(t, archive, runtimeFixture)
	rts := &Runtimes{Root: filepath.Join(tmp, "runtimes")}
	asset := AssetInfo{ID: "7", Kind: AssetKindRuntime, Path: archive, Metadata: []byte(`{"runtime":"emu","version":"2509","archive":"zip"}`)}

	dir, err := rts.Dir(asset)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(tmp, "runtimes", "emu", "2509"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	marker := filepath.Join(dir, "share", "data", "readme.txt")
	if err := os.WriteFile(marker, []byte("touched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := rts.Dir(asset); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(marker); string(got) != "touched" {
		t.Fatal("second Dir re-extracted the same asset")
	}

	asset.ID = "8"
	if _, err := rts.Dir(asset); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(marker); string(got) != "hello runtime" {
		t.Fatal("a new asset with the same name and version did not replace the extraction")
	}
}

func TestRuntimesIgnoreUnsafeMetadata(t *testing.T) {
	tmp := t.TempDir()
	archive := filepath.Join(tmp, "rt.zip")
	writeZip(t, archive, runtimeFixture)
	rts := &Runtimes{Root: filepath.Join(tmp, "runtimes")}
	dir, err := rts.Dir(AssetInfo{ID: "9", Kind: AssetKindRuntime, Path: archive, Metadata: []byte(`{"runtime":"../../x","version":".."}`)})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(tmp, "runtimes", "asset-9", "asset-9"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if _, err := rts.Dir(AssetInfo{ID: "10", Kind: "game", Path: archive}); err == nil {
		t.Fatal("a non-runtime asset was extracted")
	}
}

func TestSafeRelPath(t *testing.T) {
	ok := map[string]string{
		"a.txt":        "a.txt",
		"dir/":         "dir",
		"./dir/b.txt":  filepath.Join("dir", "b.txt"),
		"dir\\c.txt":   filepath.Join("dir", "c.txt"),
		".":            "",
		"dir/./d.txt":  filepath.Join("dir", "d.txt"),
		"Dolphin.exe":  "Dolphin.exe",
		"User/Config/": filepath.Join("User", "Config"),
	}
	for in, want := range ok {
		got, err := safeRelPath(in)
		if err != nil || got != want {
			t.Errorf("safeRelPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"..", "../a", "a/../../b", "a/../b", "/a", "\\a", "C:\\a", "a:b", "a\x00b"} {
		if _, err := safeRelPath(in); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("safeRelPath(%q) = %v, want ErrUnsafePath", in, err)
		}
	}
}
