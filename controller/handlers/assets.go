package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"kiosk/controller/internal/assets"
)

const (
	assetEndpointTTL    = 5 * time.Second
	assetChangeDebounce = 500 * time.Millisecond
)

func AssetsCommands() []Command {
	return []Command{
		AssetsListCommand{},
		AssetsStatusCommand{},
		AssetsSyncNowCommand{},
		AssetsReconcileCommand{},
	}
}

// AssetSyncer returns the asset syncer, creating it on first use. Its remote
// follows the active account, and it idles when there is none.
func (r *Registry) AssetSyncer() *assets.Syncer {
	r.assetsMu.Lock()
	defer r.assetsMu.Unlock()
	if r.assetSync != nil {
		return r.assetSync
	}
	var (
		epMu    sync.Mutex
		epAt    time.Time
		epURL   string
		epToken string
		epOK    bool
	)
	endpoint := func() (string, string, bool) {
		epMu.Lock()
		defer epMu.Unlock()
		if time.Since(epAt) > assetEndpointTTL {
			cfg, account, ok := r.refreshedAccount()
			epURL, epToken = cfg.ServerURL, account.DeviceToken
			epOK = ok && epURL != "" && epToken != ""
			epAt = time.Now()
		}
		return epURL, epToken, epOK
	}
	var (
		changeMu    sync.Mutex
		changeTimer *time.Timer
	)
	remote := &assets.HTTPRemote{
		Endpoint: endpoint,
		Control:  accountHTTPClient,
		Transfer: &http.Client{},
	}
	r.assetSync = assets.NewSyncer(assets.Config{
		DataDir: filepath.Join(r.cfg.ConfigDir, "assets"),
		Remote:  remote,
		Store:   r.Store,
		// Progress can change many times a second; coalesce into at most one
		// "assets.changed" per assetChangeDebounce, carrying the latest status.
		// The emit runs on its own goroutine, so it never holds syncer locks.
		OnChange: func() {
			changeMu.Lock()
			defer changeMu.Unlock()
			if changeTimer != nil {
				return
			}
			changeTimer = time.AfterFunc(assetChangeDebounce, func() {
				changeMu.Lock()
				changeTimer = nil
				changeMu.Unlock()
				r.Emit("assets.changed", r.AssetSyncer().Status())
			})
		},
	})
	return r.assetSync
}

// StartAssetSync runs the asset syncer until ctx ends. Called from StartAccountSync.
func (r *Registry) StartAssetSync(ctx context.Context) {
	syncer := r.AssetSyncer()
	go syncer.Run(ctx)
}

type AssetsListCommand struct{}

func (AssetsListCommand) Name() string { return "assets.list" }
func (AssetsListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Kind string `json:"kind"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	return r.AssetSyncer().List(p.Kind)
}

type AssetsStatusCommand struct{}

func (AssetsStatusCommand) Name() string { return "assets.status" }
func (AssetsStatusCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return r.AssetSyncer().Status(), nil
}

type AssetsSyncNowCommand struct{}

func (AssetsSyncNowCommand) Name() string { return "assets.syncNow" }
func (AssetsSyncNowCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	r.AssetSyncer().TriggerReconcile()
	return map[string]bool{"ok": true}, nil
}

// AssetsReconcileCommand is the server-sent kiosk command (remote-only in practice).
type AssetsReconcileCommand struct{}

func (AssetsReconcileCommand) Name() string { return "assets.reconcile" }
func (AssetsReconcileCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	r.AssetSyncer().TriggerReconcile()
	return map[string]bool{"ok": true}, nil
}
