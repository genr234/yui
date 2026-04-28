package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func StorageCommands() []Command {
	return []Command{
		StorageGetCommand{},
		StorageSetCommand{},
		StorageDeleteCommand{},
	}
}

func storagePath(r *Registry) string {
	return filepath.Join(r.cfg.ConfigDir, "storage.json")
}

func loadStorage(r *Registry) map[string]string {
	data, err := os.ReadFile(storagePath(r))
	if err != nil {
		return map[string]string{}
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return map[string]string{}
	}
	return values
}

func saveStorage(r *Registry, values map[string]string) error {
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(storagePath(r)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(storagePath(r), append(data, '\n'), 0o644)
}

type StorageGetCommand struct{}

func (StorageGetCommand) Name() string { return "storage.get" }

func (StorageGetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	value, ok := loadStorage(r)[p.Key]
	if !ok {
		return nil, nil
	}
	return &value, nil
}

type StorageSetCommand struct{}

func (StorageSetCommand) Name() string { return "storage.set" }

func (StorageSetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	values := loadStorage(r)
	values[p.Key] = p.Value
	return nil, saveStorage(r, values)
}

type StorageDeleteCommand struct{}

func (StorageDeleteCommand) Name() string { return "storage.delete" }

func (StorageDeleteCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	values := loadStorage(r)
	delete(values, p.Key)
	return nil, saveStorage(r, values)
}
