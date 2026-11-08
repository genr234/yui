package handlers

import (
	"context"
	"encoding/json"
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
		Focus:       native.SystemFocus{FallbackPID: r.focusFallbackPID},
		OnStatus: func(st native.Status) {
			r.Emit("native.status", st)
		},
	})
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
