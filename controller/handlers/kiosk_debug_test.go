package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kiosk/controller/internal/config"
)

func TestDebugReadStaysInItsFolders(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(config.Config{StorePath: filepath.Join(dir, "store.db"), ConfigDir: dir})
	logs := filepath.Join(dir, "native", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "dev.app.log"), []byte("one\nDSU two\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.json"), []byte("token"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := DebugReadCommand{}.Handle(r, json.RawMessage(`{"path":"native/logs/dev.app.log","grep":"dsu"}`))
	if err != nil {
		t.Fatal(err)
	}
	if lines := got.(map[string]any)["lines"].([]string); len(lines) != 1 || lines[0] != "DSU two" {
		t.Fatalf("lines = %v", lines)
	}
	for _, bad := range []string{`{"path":"secret.json"}`, `{"path":"native/../secret.json"}`, `{"path":"runtimes/../../etc"}`} {
		if _, err := (DebugReadCommand{}).Handle(r, json.RawMessage(bad)); err == nil {
			t.Errorf("%s: expected an error", bad)
		}
	}

	logsOut, err := DebugLogsCommand{}.Handle(r, json.RawMessage(`{"source":"native","app":"dev.app","lines":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if lines := logsOut.(map[string]any)["lines"].([]string); strings.Join(lines, ",") != "DSU two,three" {
		t.Fatalf("native tail = %v", lines)
	}
}
