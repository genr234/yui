package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"kiosk/controller/internal/appsaves"
	"kiosk/controller/internal/assets"
	"kiosk/controller/internal/native"
	"kiosk/controller/internal/webapps"
)

// Web bundles, asset URLs and app saves (contracts §6). The overlay checks
// the app's permissions and attaches app_id.

const (
	saveFlushInterval = 60 * time.Second
	saveCallTimeout   = 2 * time.Minute
	// maxSaveParamBytes bounds a saves.put request: 8 MiB of base64 plus slack.
	maxSaveParamBytes = 12 << 20
)

func WebAppsCommands() []Command {
	return []Command{
		WebAppsOriginCommand{},
		WebAppsRuntimeCommand{},
		AssetsURLCommand{},
		SavesPutCommand{},
		SavesGetCommand{},
		SavesListCommand{},
		SavesDeleteCommand{},
	}
}

// webState is the registry's web app server and app saves service.
type webState struct {
	mu      sync.Mutex
	srv     *webapps.Server
	dev     fs.FS
	saves   *appsaves.Service
	flushCh chan struct{}
	// profileIDs remembers player id -> profile id for a player whose
	// account could not be asked (offline).
	profileIDs map[string]string
	// profileIDHook replaces the profile lookup in tests.
	profileIDHook func(ctx context.Context, appID, playerID string) (string, error)
}

// WebApps returns the web app server (created on first use; it only listens
// after StartWebApps).
func (r *Registry) WebApps() *webapps.Server {
	r.web.mu.Lock()
	defer r.web.mu.Unlock()
	return r.webAppsLocked()
}

func (r *Registry) webAppsLocked() *webapps.Server {
	if r.web.srv != nil {
		return r.web.srv
	}
	cfg := r.snapshotConfig()
	r.web.srv = webapps.New(webapps.Config{
		Port:         cfg.WebPort,
		DevBundles:   r.web.dev,
		InstalledDir: filepath.Join(cfg.ConfigDir, installedWebDir),
		Assets: func(id string) (string, bool) {
			return r.AssetSyncer().LocalPath(id)
		},
	})
	return r.web.srv
}

// StartWebApps listens on the web app port until ctx ends. devBundles holds
// the embedded dev bundles at app-web/<app id>/ (may be nil). A bind failure
// is logged, not fatal: apps without a web page keep working.
func (r *Registry) StartWebApps(ctx context.Context, devBundles fs.FS) {
	r.web.mu.Lock()
	if r.web.srv == nil {
		r.web.dev = devBundles
	}
	srv := r.webAppsLocked()
	r.web.mu.Unlock()
	if r.snapshotConfig().WebPort <= 0 {
		return
	}
	ln, err := srv.Listen()
	if err != nil {
		log.Printf("web apps: cannot listen on port %d: %v", r.snapshotConfig().WebPort, err)
		return
	}
	log.Printf("web apps listening on %s", ln.Addr())
	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Printf("web apps stopped: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
}

func (r *Registry) closeWebApps() {
	r.web.mu.Lock()
	srv := r.web.srv
	r.web.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

type webAppParams struct {
	AppID   string `json:"app_id"`
	Name    string `json:"name"`
	AssetID string `json:"asset_id"`
}

type WebAppsOriginCommand struct{}

func (WebAppsOriginCommand) Name() string { return "webapps.origin" }
func (WebAppsOriginCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p webAppParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	origin, has, err := r.WebApps().Register(p.AppID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"origin": origin, "has_bundle": has}, nil
}

// errRuntimeUnavailable is the runtime counterpart of native.ErrAssetNotReady.
var errRuntimeUnavailable = errors.New("runtime is not available on this kiosk")

type WebAppsRuntimeCommand struct{}

func (WebAppsRuntimeCommand) Name() string { return "webapps.runtime" }
func (WebAppsRuntimeCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p webAppParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	srv := r.WebApps()
	// Register first: an app that never asked for its origin has no mount.
	if _, _, err := srv.Register(p.AppID); err != nil {
		return nil, err
	}
	item, ok := r.newestRuntime(p.Name)
	if !ok {
		return nil, errRuntimeUnavailable
	}
	info, ok := registryAssets{r: r}.Asset(item.ID)
	if !ok {
		return nil, errRuntimeUnavailable
	}
	dir, err := r.Native().Runtimes().Dir(info)
	if err != nil {
		return nil, err
	}
	url, err := srv.Mount(p.AppID, p.Name, dir)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(item.Metadata, &meta)
	return map[string]string{"url": url, "version": meta.Version}, nil
}

// newestRuntime finds the ready runtime asset whose metadata.runtime is name.
// "Newest" is the highest asset id (ids grow with upload order; non-numeric
// ids compare as text).
func (r *Registry) newestRuntime(name string) (assets.Item, bool) {
	items, err := r.AssetSyncer().List(native.AssetKindRuntime)
	if err != nil {
		return assets.Item{}, false
	}
	var best assets.Item
	found := false
	for _, item := range items {
		if item.State != assets.StateReady || item.Kind != native.AssetKindRuntime {
			continue
		}
		var meta struct {
			Runtime string `json:"runtime"`
		}
		if json.Unmarshal(item.Metadata, &meta) != nil || meta.Runtime != name {
			continue
		}
		if !found || idNewer(item.ID, best.ID) {
			best, found = item, true
		}
	}
	return best, found
}

func idNewer(a, b string) bool {
	na, errA := strconv.ParseInt(a, 10, 64)
	nb, errB := strconv.ParseInt(b, 10, 64)
	if errA == nil && errB == nil {
		return na > nb
	}
	return a > b
}

type AssetsURLCommand struct{}

func (AssetsURLCommand) Name() string { return "assets.url" }
func (AssetsURLCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p webAppParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if _, ok := (registryAssets{r: r}).Asset(p.AssetID); !ok {
		return nil, native.ErrAssetNotReady
	}
	url, exp, err := r.WebApps().AssetURL(p.AppID, p.AssetID)
	if err != nil {
		return nil, err
	}
	return map[string]string{"url": url, "expires_at": exp.UTC().Format(time.RFC3339)}, nil
}

// --- app saves ---

// AppSaves returns the app saves service.
func (r *Registry) AppSaves() *appsaves.Service {
	r.web.mu.Lock()
	defer r.web.mu.Unlock()
	if r.web.saves != nil {
		return r.web.saves
	}
	r.AssetSyncer() // creates the shared endpoint cache
	r.assetsMu.Lock()
	cache := r.assetEndpoint
	r.assetsMu.Unlock()
	r.web.saves = &appsaves.Service{
		Root:    filepath.Join(r.snapshotConfig().ConfigDir, "app-saves"),
		Remote:  &appsaves.HTTPRemote{Endpoint: r.assetEndpointFunc(cache)},
		Account: r.saveAccountID,
	}
	return r.web.saves
}

// saveAccountID is the active account's id when the kiosk is connected.
func (r *Registry) saveAccountID() string {
	scope := r.currentScope()
	if !scope.connected() {
		return ""
	}
	return scope.account.ID
}

// kickSaveFlush asks the flush loop to run now (an account just connected).
func (r *Registry) kickSaveFlush() {
	r.web.mu.Lock()
	ch := r.web.flushCh
	r.web.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// StartSaveSync uploads queued app saves on a timer and whenever an account
// connects, until ctx ends.
func (r *Registry) StartSaveSync(ctx context.Context) {
	r.web.mu.Lock()
	if r.web.flushCh == nil {
		r.web.flushCh = make(chan struct{}, 1)
	}
	ch := r.web.flushCh
	r.web.mu.Unlock()
	go func() {
		ticker := time.NewTicker(saveFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-ch:
			}
			r.flushSaves(ctx)
		}
	}()
}

func (r *Registry) flushSaves(ctx context.Context) {
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if n, err := r.AppSaves().Flush(flushCtx); err != nil {
		log.Printf("app saves: upload paused: %v", err)
	} else if n > 0 {
		log.Printf("app saves: uploaded %d queued save(s)", n)
	}
}

type saveParams struct {
	AppID    string `json:"app_id"`
	Key      string `json:"key"`
	Data     string `json:"data"`
	PlayerID string `json:"player_id"`
}

// saveScope builds the scope of a call. For a player it asks Rails which
// account the player is (this also checks the plan 10 grant).
func (r *Registry) saveScope(ctx context.Context, p saveParams) (appsaves.Scope, error) {
	sc := appsaves.Scope{AppID: p.AppID, PlayerID: p.PlayerID}
	if p.PlayerID == "" {
		return sc, nil
	}
	id, err := r.profileIDFor(ctx, p.AppID, p.PlayerID)
	if err != nil {
		return sc, err
	}
	sc.ProfileID = id
	return sc, nil
}

func (r *Registry) profileIDFor(ctx context.Context, appID, playerID string) (string, error) {
	lookup := r.web.profileIDHook
	if lookup == nil {
		lookup = func(ctx context.Context, appID, playerID string) (string, error) {
			mgr, err := r.activePad()
			if err != nil {
				return "", appsaves.ErrNotLinked
			}
			profile, err := mgr.Profile(ctx, playerID, appID)
			if err != nil {
				return "", err
			}
			return profile.ProfileID, nil
		}
	}
	cacheKey := appID + "\x00" + playerID
	id, err := lookup(ctx, appID, playerID)
	if err == nil && id != "" {
		r.web.mu.Lock()
		if r.web.profileIDs == nil {
			r.web.profileIDs = map[string]string{}
		}
		r.web.profileIDs[cacheKey] = id
		r.web.mu.Unlock()
		return id, nil
	}
	if err == nil {
		return "", appsaves.ErrNotLinked
	}
	var saveErr *appsaves.Error
	if errors.As(err, &saveErr) {
		return "", err
	}
	if mapped := profileError(err); mapped.Error() != err.Error() {
		return "", mapped // not_linked / not_granted / too_large with codes
	}
	// Rails could not be reached: use the profile this player had before.
	r.web.mu.Lock()
	cached := r.web.profileIDs[cacheKey]
	r.web.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	return "", appsaves.ErrOffline
}

func saveContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), saveCallTimeout)
}

type SavesPutCommand struct{}

func (SavesPutCommand) Name() string { return "saves.put" }
func (SavesPutCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	if len(params) > maxSaveParamBytes {
		return nil, appsaves.ErrTooLarge
	}
	var p saveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return nil, errors.New("data must be base64")
	}
	ctx, cancel := saveContext()
	defer cancel()
	sc, err := r.saveScope(ctx, p)
	if err != nil {
		return nil, err
	}
	return r.AppSaves().Put(ctx, sc, p.Key, data)
}

type SavesGetCommand struct{}

func (SavesGetCommand) Name() string { return "saves.get" }
func (SavesGetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p saveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	ctx, cancel := saveContext()
	defer cancel()
	sc, err := r.saveScope(ctx, p)
	if err != nil {
		return nil, err
	}
	blob, err := r.AppSaves().Get(ctx, sc, p.Key)
	if err != nil || blob == nil {
		return nil, err
	}
	return map[string]any{
		"key": blob.Key, "data": base64.StdEncoding.EncodeToString(blob.Data),
		"size": len(blob.Data), "updated_at": blob.UpdatedAt,
	}, nil
}

type SavesListCommand struct{}

func (SavesListCommand) Name() string { return "saves.list" }
func (SavesListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p saveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	ctx, cancel := saveContext()
	defer cancel()
	sc, err := r.saveScope(ctx, p)
	if err != nil {
		return nil, err
	}
	metas, err := r.AppSaves().List(ctx, sc)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(metas))
	for _, m := range metas {
		out = append(out, map[string]any{"key": m.Key, "size": m.Size, "updated_at": m.UpdatedAt})
	}
	return out, nil
}

type SavesDeleteCommand struct{}

func (SavesDeleteCommand) Name() string { return "saves.delete" }
func (SavesDeleteCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p saveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	ctx, cancel := saveContext()
	defer cancel()
	sc, err := r.saveScope(ctx, p)
	if err != nil {
		return nil, err
	}
	return nil, r.AppSaves().Delete(ctx, sc, p.Key)
}
