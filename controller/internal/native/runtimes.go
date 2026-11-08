package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// AssetKindRuntime is the asset kind a Spec.Runtime must have.
const AssetKindRuntime = "runtime"

// AssetInfo is what the session needs to know about one synced asset.
// Path is Go-internal and never leaves this package.
type AssetInfo struct {
	ID       string
	Kind     string
	Path     string
	Metadata json.RawMessage
}

// AssetSource looks up ready local assets. The handlers adapt the asset
// syncer to it.
type AssetSource interface {
	Asset(id string) (AssetInfo, bool)
}

// ErrAssetNotReady means the asset id is unknown or not downloaded yet.
var ErrAssetNotReady = errors.New("asset is not available on this kiosk")

var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

const runtimeMarker = ".yui-runtime.json"

type runtimeMarkerFile struct {
	AssetID string `json:"asset_id"`
}

// Runtimes extracts runtime assets once and remembers where.
type Runtimes struct {
	Root   string // <ConfigDir>/runtimes
	Limits Limits

	mu sync.Mutex
}

// Dir returns the extracted directory for a runtime asset, extracting it
// first if needed. An existing extraction is reused only when its marker
// names the same asset id; a re-uploaded runtime with the same name and
// version replaces it.
func (r *Runtimes) Dir(asset AssetInfo) (string, error) {
	if asset.Kind != AssetKindRuntime {
		return "", errors.New("asset is not a runtime")
	}
	name, version := runtimeNameVersion(asset)
	dir := filepath.Join(r.Root, name, version)

	r.mu.Lock()
	defer r.mu.Unlock()

	if marker, err := readRuntimeMarker(dir); err == nil && marker.AssetID == asset.ID {
		return dir, nil
	}
	if _, err := os.Lstat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("replace runtime: %w", err)
		}
	}
	if err := Extract(asset.Path, dir, r.Limits); err != nil {
		return "", err
	}
	data, err := json.Marshal(runtimeMarkerFile{AssetID: asset.ID})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, runtimeMarker), data, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func readRuntimeMarker(dir string) (runtimeMarkerFile, error) {
	var marker runtimeMarkerFile
	data, err := os.ReadFile(filepath.Join(dir, runtimeMarker))
	if err != nil {
		return marker, err
	}
	err = json.Unmarshal(data, &marker)
	return marker, err
}

// runtimeNameVersion reads `runtime` and `version` from the asset metadata
// (contracts §1). Values that are not safe path segments fall back to the
// asset id, so metadata can never steer extraction outside Root.
func runtimeNameVersion(asset AssetInfo) (string, string) {
	var meta struct {
		Runtime string `json:"runtime"`
		Version string `json:"version"`
	}
	if len(asset.Metadata) > 0 {
		_ = json.Unmarshal(asset.Metadata, &meta)
	}
	name := meta.Runtime
	if !segmentPattern.MatchString(name) || name == "." || name == ".." {
		name = "asset-" + asset.ID
	}
	version := meta.Version
	if !segmentPattern.MatchString(version) || version == "." || version == ".." {
		version = "asset-" + asset.ID
	}
	return name, version
}
