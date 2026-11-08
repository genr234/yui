package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"kiosk/controller/internal/probe"
)

func StatusCommands() []Command {
	return []Command{
		StatusGetCommand{},
		DiagnosticsGetCommand{},
		SystemProbeCommand{},
	}
}

// SystemProbeCommand reports the kiosk's hardware (Windows build, audio
// endpoints, GPUs) for the audio strategy decision.
type SystemProbeCommand struct{}

func (SystemProbeCommand) Name() string { return "system.probe" }

func (SystemProbeCommand) Handle(_ *Registry, _ json.RawMessage) (any, error) {
	return probe.Collect(context.Background()), nil
}

type StatusGetCommand struct{}

func (StatusGetCommand) Name() string { return "status.get" }

func (StatusGetCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return readJSONFile(r.snapshotConfig().StatusPath)
}

type DiagnosticsGetCommand struct{}

func (DiagnosticsGetCommand) Name() string { return "diagnostics.get" }

func (DiagnosticsGetCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return readTextFile(diagnosticsPath(r.snapshotConfig().StatusPath))
}

func readJSONFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func readTextFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{"text": ""}, err
	}
	return map[string]string{"text": string(data)}, nil
}

func diagnosticsPath(statusPath string) string {
	return filepath.Join(filepath.Dir(statusPath), "diagnostics.txt")
}
