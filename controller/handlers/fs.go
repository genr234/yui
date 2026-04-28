package handlers

import (
	"encoding/json"
	"os"
)

func FSCommands() []Command {
	return []Command{
		FSReadCommand{},
		FSListCommand{},
	}
}

type FSReadCommand struct{}

func (FSReadCommand) Name() string { return "fs.read" }

func (FSReadCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	data, err := os.ReadFile(p.Path)
	return string(data), err
}

type FSListCommand struct{}

func (FSListCommand) Name() string { return "fs.list" }

func (FSListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p.Path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += string(os.PathSeparator)
		}
		names = append(names, name)
	}
	return names, nil
}
