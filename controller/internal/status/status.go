package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kiosk/controller/internal/telemetry"
)

type State struct {
	ControllerStartedAt string `json:"controller_started_at"`
	UpdatedAt           string `json:"updated_at"`
	Version             string `json:"version"`
	Commit              string `json:"commit"`
	Event               string `json:"event"`
	ConfigPath          string `json:"config_path"`
	ChromePath          string `json:"chrome_path"`
	ChromePID           int    `json:"chrome_pid,omitempty"`
	RestartCount        int    `json:"restart_count"`
	NextRestartAt       string `json:"next_restart_at,omitempty"`
	LastError           string `json:"last_error,omitempty"`
}

type Writer struct {
	path string
	base State
}

func New(path string, version string, commit string, configPath string, chromePath string) *Writer {
	now := time.Now().Format(time.RFC3339)
	return &Writer{
		path: path,
		base: State{
			ControllerStartedAt: now,
			Version:             version,
			Commit:              commit,
			ConfigPath:          configPath,
			ChromePath:          chromePath,
		},
	}
}

func (w *Writer) Write(update State) {
	recordTelemetry(update)
	if w == nil || w.path == "" {
		return
	}

	state := w.base
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	state.Event = update.Event
	state.ChromePath = first(update.ChromePath, state.ChromePath)
	state.ChromePID = update.ChromePID
	state.RestartCount = update.RestartCount
	state.NextRestartAt = update.NextRestartAt
	state.LastError = update.LastError

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		return
	}
	_ = os.WriteFile(w.path, data, 0644)
	_ = os.WriteFile(diagnosticsPath(w.path), []byte(formatDiagnostics(state)), 0644)
}

// chromeStates maps status events to the "chrome" field of the telemetry snapshot.
var chromeStates = map[string]string{
	"starting_chrome": "starting", "chrome_started": "running", "chrome_detached": "running",
	"chrome_exited": "restarting", "chrome_start_failed": "start_failed", "chrome_hung": "restarting",
	"chrome_restart_requested": "restarting", "controller_stopped": "stopped",
}

func recordTelemetry(update State) {
	if update.Event == "" {
		return
	}
	data := map[string]any{}
	if update.ChromePID != 0 {
		data["pid"] = update.ChromePID
	}
	if update.RestartCount != 0 {
		data["restart_count"] = update.RestartCount
	}
	if update.LastError != "" {
		data["error"] = update.LastError
	}
	if update.NextRestartAt != "" {
		data["next_restart_at"] = update.NextRestartAt
	}
	telemetry.Record(update.Event, data)
	if chrome, ok := chromeStates[update.Event]; ok {
		telemetry.Set("chrome", chrome)
		telemetry.Set("chrome_since", time.Now().UTC().Format(time.RFC3339))
		telemetry.Set("chrome_restarts", update.RestartCount)
	}
}

func first(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func diagnosticsPath(statusPath string) string {
	return filepath.Join(filepath.Dir(statusPath), "diagnostics.txt")
}

func formatDiagnostics(state State) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Yui Kiosk Controller\n")
	fmt.Fprintf(&b, "Version: %s\n", state.Version)
	fmt.Fprintf(&b, "Commit: %s\n", state.Commit)
	fmt.Fprintf(&b, "Controller started: %s\n", state.ControllerStartedAt)
	fmt.Fprintf(&b, "Updated: %s\n", state.UpdatedAt)
	fmt.Fprintf(&b, "Event: %s\n", state.Event)
	fmt.Fprintf(&b, "Config: %s\n", state.ConfigPath)
	fmt.Fprintf(&b, "Chrome: %s\n", state.ChromePath)
	if state.ChromePID != 0 {
		fmt.Fprintf(&b, "Chrome PID: %d\n", state.ChromePID)
	}
	fmt.Fprintf(&b, "Restart count: %d\n", state.RestartCount)
	if state.NextRestartAt != "" {
		fmt.Fprintf(&b, "Next restart: %s\n", state.NextRestartAt)
	}
	if state.LastError != "" {
		fmt.Fprintf(&b, "Last error: %s\n", state.LastError)
	}

	return b.String()
}
