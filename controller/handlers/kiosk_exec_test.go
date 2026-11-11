//go:build !windows

package handlers

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kiosk/controller/internal/config"
)

func TestDebugExecRunsOnlyFromTheServer(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(config.Config{StorePath: filepath.Join(dir, "store.db"), ConfigDir: dir})
	params := json.RawMessage(`{"script":"echo hi; echo oops >&2; exit 3"}`)

	if _, err := r.Dispatch("debug.exec", params); err == nil {
		t.Fatal("local Dispatch must not reach debug.exec")
	}
	if _, err := r.DispatchAuthenticated("debug.exec", params); err == nil {
		t.Fatal("the local socket must not reach debug.exec")
	}

	got, err := r.DispatchRemote("debug.exec", params)
	if err != nil {
		t.Fatal(err)
	}
	out := got.(map[string]any)
	if out["stdout"] != "hi\n" || out["stderr"] != "oops\n" || out["exit_code"] != 3 {
		t.Fatalf("out = %v", out)
	}
}

func TestDebugExecTimeoutAndCap(t *testing.T) {
	out, err := runScript("", "sleep 5", "", 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if out["timed_out"] != true {
		t.Fatalf("out = %v", out)
	}

	out, err = runScript("", "head -c 400000 /dev/zero | tr '\\0' a", "", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if s := out["stdout"].(string); len(s) != execMaxOutput || !strings.HasPrefix(s, "aaa") || out["truncated"] != true {
		t.Fatalf("len = %d truncated = %v", len(s), out["truncated"])
	}
}
