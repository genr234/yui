package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"kiosk/controller/internal/assets"
	"kiosk/controller/internal/config"
	"kiosk/controller/internal/store"
)

type Command interface {
	Name() string
	Handle(r *Registry, params json.RawMessage) (any, error)
}

type Registry struct {
	cfg        config.Config
	processMu  sync.Mutex
	processes  map[int]*exec.Cmd
	commands   map[string]Command
	storeMu    sync.Mutex
	store      *store.DB
	migrated   bool
	storeEpoch uint64
	plugins    *PluginManager
	authMu     sync.Mutex
	authState  AuthState
	authToken  string
	pluginCtx  context.Context
	syncMu     sync.Mutex
	syncState  accountSyncState
	assetsMu   sync.Mutex
	assetSync  *assets.Syncer
	emitMu     sync.RWMutex
	emitter    Emitter
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
	result, err := r.Dispatch(method, params)
	if err == nil {
		r.recordMutation(method, params, result)
	}
	return result, err
}

func (r *Registry) DispatchRemote(method string, params json.RawMessage) (any, error) {
	if !remoteCommandAllowed(method) {
		return nil, fmt.Errorf("remote command %q is not allowed", method)
	}
	result, err := r.Dispatch(method, params)
	if err == nil {
		r.recordMutation(method, params, result)
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
	legacyPath := filepath.Join(r.cfg.ConfigDir, "storage.json")
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
