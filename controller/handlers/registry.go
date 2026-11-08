package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"kiosk/controller/internal/assets"
	"kiosk/controller/internal/config"
	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/apps"
	"kiosk/controller/internal/pad/router"
	"kiosk/controller/internal/pad/session"
	"kiosk/controller/internal/store"
)

type Command interface {
	Name() string
	Handle(r *Registry, params json.RawMessage) (any, error)
}

type Registry struct {
	// configMu serializes read-modify-save-publish of cfg. Never acquire it
	// while storeMu, cfgMu, or the asset reconcile lock is held.
	configMu sync.Mutex
	// switchMu serializes account transitions, including the wait for an
	// in-flight reconcile to leave the old account.
	switchMu      sync.Mutex
	cfgMu         sync.RWMutex
	cfg           config.Config
	processMu     sync.Mutex
	processes     map[int]*exec.Cmd
	commands      map[string]Command
	storeMu       sync.Mutex
	store         *store.DB
	migrated      bool
	storeEpoch    uint64
	plugins       *PluginManager
	authMu        sync.Mutex
	authState     AuthState
	authToken     string
	pluginCtx     context.Context
	syncMu        sync.Mutex
	syncState     accountSyncState
	assetsMu      sync.Mutex
	assetSync     *assets.Syncer
	assetEndpoint *assetEndpointCache
	emitMu        sync.RWMutex
	emitter       Emitter
	// padMu guards the play-session handle. Do not take config, store, or
	// asset locks while holding it.
	padMu sync.Mutex
	// padStartMu serializes pad.session.start so two taps cannot each open
	// a manager. It is taken before padMu, never while holding it.
	padStartMu   sync.Mutex
	padRoot      context.Context
	padMgr       *session.Manager
	padCancel    context.CancelFunc
	padDone      chan struct{}
	padSensitive bool
	// padRouter is the input hub (focus stack + adapters), created by the
	// first play session and kept for the controller's lifetime.
	padRouter *router.Router
	// padDSUAddrs overrides the DSU listen addresses (players, system pad).
	padDSUAddrs [2]string
	// mutationMu: a command that records an outbox entry (local or remote)
	// holds the read side from its write until that entry is recorded. An
	// account transition holds the write side while it publishes, so the
	// write and its entry always land in the same account's store.
	mutationMu sync.RWMutex
	// cmdMu guards cmdCancel, which closes the command socket of the
	// account that opened it.
	cmdMu     sync.Mutex
	cmdCancel context.CancelFunc
	// nativeMu guards the native session manager handle. The manager has its
	// own locks; do not hold nativeMu while calling into it.
	nativeMu  sync.Mutex
	nativeMgr *native.Manager
	// ctrlMu guards the app/native layer controller (controllers.go), built
	// on the input hub the first time an app or native session needs it.
	// Never held while calling into the hub or the controller.
	ctrlMu     sync.Mutex
	ctrl       *apps.Controller
	ctrlRouter *router.Router
}

// errStoreScopeChanged means the active account store moved to a new epoch
// after the caller captured its scope. The write was not made.
var errStoreScopeChanged = errors.New("store scope changed")

// storeScope is the published config and store epoch read together. Sync,
// remote commands, and outbox records capture one at the start and write
// only while the epoch is unchanged.
type storeScope struct {
	cfg     config.Config
	account config.AccountConfig
	epoch   uint64
}

// connected reports whether the scope has an account that can talk to the
// server.
func (s storeScope) connected() bool {
	return s.cfg.ServerURL != "" && s.account.ID != "" && s.account.DeviceToken != ""
}

// currentScope reads the config and epoch under storeMu. Account publication
// changes both under that lock, so they always belong to the same account.
func (r *Registry) currentScope() storeScope {
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	cfg := r.snapshotConfig()
	scope := storeScope{cfg: cfg, epoch: r.storeEpoch}
	if account, ok := findAccount(cfg.Accounts, cfg.ActiveAccountID); ok {
		scope.account = *account
	}
	return scope
}

// refreshedScope reloads the account fields from disk and captures a scope.
func (r *Registry) refreshedScope() storeScope {
	r.reloadAccountConfig()
	return r.currentScope()
}

func NewRegistry(cfg config.Config) *Registry {
	r := &Registry{
		cfg:       cfg,
		processes: make(map[int]*exec.Cmd),
		commands:  make(map[string]Command),
	}
	r.Register(StatusCommands()...)
	r.Register(ConfigCommands()...)
	r.Register(AuthCommands()...)
	r.Register(AccountCommands()...)
	r.Register(StorageCommands()...)
	r.Register(FSCommands()...)
	r.Register(ProcessCommands()...)
	r.Register(NetworkCommands()...)
	r.Register(AppsCommands()...)
	r.Register(AssetsCommands()...)
	r.plugins = NewPluginManager(r)
	r.Register(PluginCommands()...)
	r.Register(UpdateCommands()...)
	r.Register(EmbedBlockerCommands()...)
	r.Register(PadCommands()...)
	r.Register(NativeCommands()...)
	r.Register(LibraryCommands()...)
	r.Register(ControllersCommands()...)
	return r
}

func (r *Registry) Register(cmds ...Command) {
	for _, cmd := range cmds {
		r.commands[cmd.Name()] = cmd
	}
}

func (r *Registry) Dispatch(method string, params json.RawMessage) (any, error) {
	cmd, ok := r.commands[method]
	if !ok {
		return nil, fmt.Errorf("unknown method: %s", method)
	}
	return cmd.Handle(r, params)
}

func (r *Registry) DispatchAuthenticated(method string, params json.RawMessage) (any, error) {
	if err := r.Authorize(method, params); err != nil {
		return nil, err
	}
	if !recordsMutation(method) {
		return r.Dispatch(method, params)
	}
	r.mutationMu.RLock()
	defer r.mutationMu.RUnlock()
	epoch := r.currentScope().epoch
	result, err := r.Dispatch(method, params)
	if err == nil {
		r.recordMutation(epoch, method, params, result)
	}
	return result, err
}

func (r *Registry) DispatchRemote(method string, params json.RawMessage) (any, error) {
	r.mutationMu.RLock()
	defer r.mutationMu.RUnlock()
	return r.dispatchRemoteAt(r.currentScope().epoch, method, params)
}

// dispatchRemoteAt runs a server command for the store at epoch. The caller
// holds mutationMu for reading, so the epoch cannot move while it runs.
func (r *Registry) dispatchRemoteAt(epoch uint64, method string, params json.RawMessage) (any, error) {
	if !remoteCommandAllowed(method) {
		return nil, fmt.Errorf("remote command %q is not allowed", method)
	}
	result, err := r.Dispatch(method, params)
	if err == nil {
		r.recordMutation(epoch, method, params, result)
	}
	return result, err
}

func (r *Registry) Store() (*store.DB, error) {
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	if err := r.ensureStoreLocked(); err != nil {
		return nil, err
	}
	return r.store, nil
}

// withStore runs fn while the active database and its epoch stay paired.
// Account publication takes the same lock, so a read cannot observe one
// account's document with another account's epoch.
func (r *Registry) withStore(fn func(db *store.DB, epoch uint64) error) error {
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	if err := r.ensureStoreLocked(); err != nil {
		return err
	}
	return fn(r.store, r.storeEpoch)
}

// withStoreAt runs fn on the active database only while the store epoch
// still equals epoch. Otherwise it returns errStoreScopeChanged and writes
// nothing, so a sync or command that started on one account cannot write the
// next account's database.
func (r *Registry) withStoreAt(epoch uint64, fn func(db *store.DB) error) error {
	r.storeMu.Lock()
	defer r.storeMu.Unlock()
	if r.storeEpoch != epoch {
		return errStoreScopeChanged
	}
	if err := r.ensureStoreLocked(); err != nil {
		return err
	}
	return fn(r.store)
}

// ensureStoreLocked opens the active account database. The caller holds
// storeMu. Migration uses that database directly: calling Store or withStore
// from here would lock storeMu again and deadlock.
func (r *Registry) ensureStoreLocked() error {
	if r.store != nil {
		return nil
	}

	db, err := store.Open(r.activeStorePath())
	if err != nil {
		return err
	}
	r.store = db

	if !r.migrated {
		r.migrated = true
		if err := r.migrateLegacyStorage(); err != nil {
			log.Printf("legacy storage migration failed: %v", err)
		}
	}
	return nil
}

func (r *Registry) Close() error {
	r.closeNative()
	if r.plugins != nil {
		r.plugins.Stop()
	}
	r.storeMu.Lock()
	defer r.storeMu.Unlock()

	if r.store == nil {
		return nil
	}
	err := r.store.Close()
	r.store = nil
	return err
}

// resetStoreLocked drops the open database and bumps the epoch. The caller
// holds storeMu and must publish the config that selects the next database
// before releasing it. The epoch still bumps when nothing is open, because a
// switch can happen before the first store command.
func (r *Registry) resetStoreLocked() error {
	r.storeEpoch++
	r.migrated = false
	if r.store == nil {
		return nil
	}
	db := r.store
	r.store = nil
	return db.Close()
}

func (r *Registry) migrateLegacyStorage() error {
	legacyPath := filepath.Join(r.snapshotConfig().ConfigDir, "storage.json")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	values := map[string]string{}
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}

	collection := r.store.Collection("storage")
	for key, value := range values {
		if _, ok, err := collection.Get(key); err != nil {
			return err
		} else if ok {
			continue
		}
		if err := collection.Put(key, value); err != nil {
			return err
		}
	}

	migratedPath := legacyPath + ".migrated"
	if err := os.Rename(legacyPath, migratedPath); err != nil {
		log.Printf("legacy storage migrated but could not rename %s: %v", legacyPath, err)
	}
	return nil
}
