package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Private installs: a program that keeps its settings beside its executable
// runs from <dataDir>/runtime/, a copy of the shared extracted runtime. The
// files are hard-linked (or copied when linking fails) so a 150 MB runtime
// costs no extra disk. Everything else the program or the app writes under
// runtime/ belongs to the account and survives launches and runtime updates.

// privateDirName is the folder under the data directory that holds a private
// install.
const privateDirName = "runtime"

// installManifest records what an install placed, so a later install of
// another runtime version removes only those files.
type installManifest struct {
	AssetID string   `json:"asset_id"`
	Files   []string `json:"files"` // slash paths relative to runtime/
}

// linkFile is os.Link; tests replace it to force the copy path.
var linkFile = os.Link

// installPrivate places the files of the extracted runtime in srcDir into
// <dataDir>/runtime/ and returns that directory. assetID names the runtime
// asset; with an unchanged asset, files already in place are kept as they
// are, so program edits to a copied file are not overwritten.
func installPrivate(srcDir, dataDir, assetID string) (string, error) {
	dst := filepath.Join(dataDir, privateDirName)
	if info, err := os.Lstat(dst); err == nil && !info.IsDir() {
		return "", errors.New("runtime install path is not a directory")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	prev, _ := readInstallManifest(dst)
	sameAsset := prev.AssetID == assetID

	var files []string
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case slash == runtimeMarker || slash == installMarker:
			return nil
		case !d.Type().IsRegular():
			return nil // links and devices are never placed
		}
		files = append(files, slash)
		if sameAsset {
			if _, err := os.Lstat(target); err == nil {
				return nil
			}
		}
		return placeFile(path, target)
	})
	if err != nil {
		return "", err
	}

	// Files an older version placed that this one does not have.
	keep := make(map[string]bool, len(files))
	for _, f := range files {
		keep[f] = true
	}
	for _, old := range prev.Files {
		if keep[old] {
			continue
		}
		if rel, err := safeRelPath(old); err == nil && rel != "" {
			_ = os.Remove(filepath.Join(dst, filepath.FromSlash(rel)))
		}
	}

	sort.Strings(files)
	data, err := json.Marshal(installManifest{AssetID: assetID, Files: files})
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Join(dst, installMarker), data); err != nil {
		return "", err
	}
	return dst, nil
}

// installMarker is the manifest's file name inside runtime/ (C7).
const installMarker = ".yui-runtime.json"

func readInstallManifest(dst string) (installManifest, error) {
	var m installManifest
	data, err := os.ReadFile(filepath.Join(dst, installMarker))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(data, &m)
	return m, err
}

// placeFile puts src at target, replacing what is there: a hard link, or a
// copy when linking is not possible (another volume, a file system without
// links).
func placeFile(src, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil {
		if info.IsDir() {
			return fmt.Errorf("a folder is in the way of a runtime file")
		}
		if err := os.Remove(target); err != nil {
			return err
		}
	}
	if err := linkFile(src, target); err == nil {
		return nil
	}
	return copyFile(src, target)
}

func copyFile(src, target string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".yui-copy-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, info.Mode().Perm()); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
