package handlers

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

func ProcessCommands() []Command {
	return []Command{
		ProcessLaunchCommand{},
		ProcessKillCommand{},
	}
}

type ProcessLaunchCommand struct{}

func (ProcessLaunchCommand) Name() string { return "process.launch" }

func (ProcessLaunchCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Exe  string   `json:"exe"`
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	cmd := exec.Command(p.Exe, p.Args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r.processMu.Lock()
	r.processes[cmd.Process.Pid] = cmd
	r.processMu.Unlock()
	return map[string]int{"pid": cmd.Process.Pid}, nil
}

type ProcessKillCommand struct{}

func (ProcessKillCommand) Name() string { return "process.kill" }

func (ProcessKillCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	r.processMu.Lock()
	cmd := r.processes[p.PID]
	delete(r.processes, p.PID)
	r.processMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil, fmt.Errorf("unknown pid: %d", p.PID)
	}
	return nil, cmd.Process.Kill()
}
