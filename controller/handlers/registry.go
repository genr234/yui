package handlers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"

	"kiosk/controller/internal/config"
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
