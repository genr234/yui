package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/store"
)

type Command interface {
	Name() string
	Handle(r *Registry, params json.RawMessage) (any, error)
}

type Registry struct {
	cfg       config.Config
	processMu sync.Mutex
	processes map[int]*exec.Cmd
	commands  map[string]Command
	storeMu   sync.Mutex
	store     *store.DB
	migrated  bool
	plugins   *PluginManager
}

func NewRegistry(cfg config.Config) *Registry {
	r := &Registry{
		cfg:       cfg,
		processes: make(map[int]*exec.Cmd),
		commands:  make(map[string]Command),
	}
	r.Register(StatusCommands()...)
	r.Register(ConfigCommands()...)
	r.Register(StorageCommands()...)
	r.Register(FSCommands()...)
	r.Register(ProcessCommands()...)
	r.Register(NetworkCommands()...)
	r.Register(AppsCommands()...)
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

func (r *Registry) Store() (*store.DB, error) {
	r.storeMu.Lock()
	defer r.storeMu.Unlock()

	if r.store != nil {
		return r.store, nil
	}

	db, err := store.Open(r.cfg.StorePath)
	if err != nil {
		return nil, err
	}
	r.store = db

	if !r.migrated {
		r.migrated = true
		if err := r.migrateLegacyStorage(); err != nil {
			log.Printf("legacy storage migration failed: %v", err)
		}
	}

	return r.store, nil
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
