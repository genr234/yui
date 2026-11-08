package handlers

import (
	"path/filepath"
	"testing"

	"kiosk/controller/internal/assets"
	"kiosk/controller/internal/config"
)

func TestAssetsCommands(t *testing.T) {
	dir := t.TempDir()
	registry := NewRegistry(config.Config{
		ConfigPath: filepath.Join(dir, "controller.json"),
		ConfigDir:  dir,
		StorePath:  filepath.Join(dir, "store.db"),
	})
	defer registry.Close()

	result, err := registry.DispatchAuthenticated("assets.list", mustJSON(t, map[string]any{"kind": "wii-game"}))
	if err != nil {
		t.Fatal(err)
	}
	if items := result.([]assets.Item); len(items) != 0 {
		t.Fatalf("expected empty list, got %v", items)
	}
	status, err := registry.DispatchAuthenticated("assets.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st := status.(assets.Status); st.State != "idle" || st.PausedReasons == nil {
		t.Fatalf("status %+v", st)
	}
	if _, err := registry.DispatchAuthenticated("assets.syncNow", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.DispatchRemote("assets.reconcile", nil); err != nil {
		t.Fatalf("assets.reconcile should be remote-allowed: %v", err)
	}
	if _, err := registry.DispatchRemote("assets.list", nil); err == nil {
		t.Fatal("assets.list must not be remote-allowed")
	}
}

func TestAssetsReadCommandsRequirePIN(t *testing.T) {
	dir := t.TempDir()
	registry := NewRegistry(config.Config{ConfigPath: filepath.Join(dir, "controller.json"), ConfigDir: dir, StorePath: filepath.Join(dir, "store.db")})
	defer registry.Close()
	res, err := registry.DispatchAuthenticated("auth.setPin", mustJSON(t, map[string]any{"pin": "129684"}))
	if err != nil {
		t.Fatal(err)
	}
	token := res.(authResult).Token
	if _, err := registry.DispatchAuthenticated("assets.list", nil); err == nil {
		t.Fatal("expected PIN gate, same as apps.installed.list")
	}
	if _, err := registry.DispatchAuthenticated("assets.list", mustJSON(t, map[string]any{"_auth_token": token})); err != nil {
		t.Fatal(err)
	}
}
