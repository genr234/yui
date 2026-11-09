package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"kiosk/controller/internal/assets"
	"kiosk/controller/internal/native"
)

// Native sessions (contracts §4, `native.*`). The overlay gates the
// `native` permission to admin-trusted apps; the controller only sees the
// app id the overlay's ctx attaches.

func NativeCommands() []Command {
	return []Command{
		NativeLaunchCommand{},
		NativeStopCommand{},
		NativeStatusCommand{},
		NativeSavesBackupCommand{},
	}
}

// Native returns the native session manager, creating it on first use.
// The input hub uses it to attach virtual pads (SetVirtualPads) and to
// follow sessions (AddObserver).
func (r *Registry) Native() *native.Manager {
	r.nativeMu.Lock()
	defer r.nativeMu.Unlock()
	if r.nativeMgr != nil {
		return r.nativeMgr
	}
	cfg := r.snapshotConfig()
	root := filepath.Join(cfg.ConfigDir, "native")
	r.nativeMgr = native.NewManager(native.Config{
		RuntimesDir: filepath.Join(cfg.ConfigDir, "runtimes"),
		DataDir:     filepath.Join(root, "data"),
		LogDir:      filepath.Join(root, "logs"),
		Assets:      registryAssets{r: r},
		SaveRemote:  registrySaves{r: r},
		VirtualPads: r.dsuResolver(),
		Focus:       native.SystemFocus{FallbackPID: r.focusFallbackPID},
		OnStatus: func(st native.Status) {
			r.Emit("native.status", st)
		},
	})
	// The input hub follows sessions: native layer, virtual pads, watchdog.
	r.nativeMgr.AddObserver(nativeHubObserver{r: r})
	return r.nativeMgr
}

func (r *Registry) nativeIfStarted() *native.Manager {
	r.nativeMu.Lock()
	defer r.nativeMu.Unlock()
	return r.nativeMgr
}

// nativeAssetInUse is the asset syncer's InUse hook (contracts §1 orphan
// grace): files the active native session uses are not deleted.
func (r *Registry) nativeAssetInUse(id string) bool {
	if mgr := r.nativeIfStarted(); mgr != nil {
		return mgr.InUse(id)
	}
	return false
}

func (r *Registry) closeNative() {
	if mgr := r.nativeIfStarted(); mgr != nil {
		mgr.Close()
	}
}

// focusFallbackPID is the fallback focus target when the window captured
// at launch is gone: the live Chrome browser process (chromePID in audio.go).
func (r *Registry) focusFallbackPID() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pid, err := r.chromePID(ctx)
	if err != nil {
		return 0
	}
	return pid
}

// registryAssets adapts the asset syncer to native.AssetSource.
type registryAssets struct{ r *Registry }

func (a registryAssets) Asset(id string) (native.AssetInfo, bool) {
	syncer := a.r.AssetSyncer()
	items, err := syncer.List("")
	if err != nil {
		return native.AssetInfo{}, false
	}
	for _, item := range items {
		if item.ID != id || item.State != assets.StateReady {
			continue
		}
		path, ok := syncer.LocalPath(id)
		if !ok {
			return native.AssetInfo{}, false
		}
		return native.AssetInfo{ID: item.ID, Kind: item.Kind, Path: path, Metadata: item.Metadata}, true
	}
	return native.AssetInfo{}, false
}

type nativeLaunchParams struct {
	AppID       string              `json:"app_id"`
	Runtime     string              `json:"runtime"`
	Executable  string              `json:"executable"`
	Args        []string            `json:"args"`
	Files       []native.ConfigFile `json:"files"`
	VirtualPads []int               `json:"virtual_pads"`
	Audio       string              `json:"audio"`
	Saves       []string            `json:"saves"`
}

type NativeLaunchCommand struct{}

func (NativeLaunchCommand) Name() string { return "native.launch" }
func (NativeLaunchCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p nativeLaunchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	scope := r.snapshotConfig().ActiveAccountID
	st, err := r.Native().Launch(native.Spec{
		AppID:       p.AppID,
		Scope:       nativeScope(scope),
		Runtime:     p.Runtime,
		Executable:  p.Executable,
		Args:        p.Args,
		Files:       p.Files,
		VirtualPads: p.VirtualPads,
		Audio:       p.Audio,
		Saves:       p.Saves,
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// nativeScope keeps each account's program data (saves, settings) apart.
func nativeScope(accountID string) string {
	if accountID == "" {
		return "local"
	}
	return "account-" + accountID
}

type NativeStopCommand struct{}

func (NativeStopCommand) Name() string { return "native.stop" }
func (NativeStopCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID string `json:"app_id"`
		Force bool   `json:"force"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	mgr := r.nativeIfStarted()
	if mgr == nil {
		return native.Status{State: native.StateIdle}, nil
	}
	st, err := mgr.Stop(p.AppID, p.Force)
	if err != nil {
		return nil, err
	}
	return st, nil
}

type NativeStatusCommand struct{}

func (NativeStatusCommand) Name() string { return "native.status" }
func (NativeStatusCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	mgr := r.nativeIfStarted()
	if mgr == nil {
		return native.Status{State: native.StateIdle}, nil
	}
	return mgr.Status(), nil
}

// registrySaves adapts the kiosk saves API to native.SaveRemote. Timeouts
// live in the HTTP client; the account endpoint is shared with the syncer.
type registrySaves struct{ r *Registry }

func (s registrySaves) remote() *assets.HTTPRemote {
	s.r.AssetSyncer() // creates the shared endpoint cache
	s.r.assetsMu.Lock()
	cache := s.r.assetEndpoint
	s.r.assetsMu.Unlock()
	return &assets.HTTPRemote{
		Endpoint: s.r.assetEndpointFunc(cache),
		Control:  accountHTTPClient,
		Transfer: &http.Client{},
	}
}

func mapSaveError(err error) error {
	switch {
	case errors.Is(err, assets.ErrNoAccount):
		return native.ErrSavesOffline
	case errors.Is(err, assets.ErrSaveNewer):
		return native.ErrSaveRemoteNewer
	case errors.Is(err, assets.ErrSaveTooLarge):
		return native.ErrTooLarge
	}
	return err
}

func (s registrySaves) SaveInfo(appID, runtime string) (native.SaveInfo, bool, error) {
	info, ok, err := s.remote().SaveInfo(context.Background(), appID, runtime)
	if err != nil || !ok {
		return native.SaveInfo{}, false, mapSaveError(err)
	}
	return native.SaveInfo{SHA256: info.SHA256, Size: info.Size, SavedAt: info.SavedAt, KioskName: info.KioskName}, true, nil
}

func (s registrySaves) SaveDownload(appID, runtime string) (io.ReadCloser, error) {
	body, err := s.remote().SaveDownload(context.Background(), appID, runtime)
	return body, mapSaveError(err)
}

func (s registrySaves) SaveUpload(appID, runtime string, up native.SaveUpload) error {
	return mapSaveError(s.remote().SaveUpload(context.Background(), appID, runtime, up.Body, up.Size, up.SHA256, up.SavedAt))
}

// NativeSavesBackupCommand backs up the declared saves of the app's current or
// latest session now, instead of waiting for the session to end.
type NativeSavesBackupCommand struct{}

func (NativeSavesBackupCommand) Name() string { return "native.saves.backup" }
func (NativeSavesBackupCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID string `json:"app_id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	mgr := r.nativeIfStarted()
	if mgr == nil {
		return nil, native.ErrNoSaveTarget
	}
	return mgr.BackupSaves(p.AppID)
}
