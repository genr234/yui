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
	if r.assetEndpoint == nil {
		r.assetEndpoint = &assetEndpointCache{}
	}
	endpointCache := r.assetEndpoint
	endpoint := r.assetEndpointFunc(endpointCache)
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
		DataDir: filepath.Join(r.snapshotConfig().ConfigDir, "assets"),
		Remote:  remote,
		Store:   r.Store,
		InUse:   r.nativeAssetInUse,
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

// assetEndpointFunc returns the active account's server URL and device token,
// cached in cache. The asset syncer and save-file backup share it.
func (r *Registry) assetEndpointFunc(cache *assetEndpointCache) func() (string, string, bool) {
	return func() (string, string, bool) {
		return cache.current(func() (string, string, bool) {
			cfg, account, ok := r.refreshedAccount()
			return cfg.ServerURL, account.DeviceToken, ok && cfg.ServerURL != "" && account.DeviceToken != ""
		})
	}
}

func (r *Registry) assetSyncerIfStarted() *assets.Syncer {
	r.assetsMu.Lock()
	defer r.assetsMu.Unlock()
	return r.assetSync
}

func (r *Registry) invalidateAssetEndpoint() {
	r.assetsMu.Lock()
	endpoint := r.assetEndpoint
	r.assetsMu.Unlock()
	if endpoint != nil {
		endpoint.invalidate()
	}
}

func (r *Registry) triggerAssetReconcile() {
	if syncer := r.assetSyncerIfStarted(); syncer != nil {
		syncer.TriggerReconcile()
	}
}

// assetEndpointCache remembers the active account's asset credentials. Account
// transitions drop it so a reconcile cannot keep using the previous token for
// assetEndpointTTL after the switch.
type assetEndpointCache struct {
	mu    sync.Mutex
	at    time.Time
	url   string
	token string
	ok    bool
}

func (c *assetEndpointCache) current(load func() (string, string, bool)) (string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() || time.Since(c.at) > assetEndpointTTL {
		c.url, c.token, c.ok = load()
		c.at = time.Now()
	}
	return c.url, c.token, c.ok
}

func (c *assetEndpointCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Time{}
	c.url = ""
	c.token = ""
	c.ok = false
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
