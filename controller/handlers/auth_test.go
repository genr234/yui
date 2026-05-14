package handlers

import (
	"path/filepath"
	"testing"

	"kiosk/controller/internal/config"
)

func TestAdminPINProtectsPrivilegedBridgeCommands(t *testing.T) {
	registry := NewRegistry(config.Config{
		ConfigPath: filepath.Join(t.TempDir(), "controller.json"),
		StorePath:  filepath.Join(t.TempDir(), "store.db"),
	})

	result, err := registry.DispatchAuthenticated("auth.setPin", mustJSON(t, map[string]any{
		"pin": "129684",
	}))
	if err != nil {
		t.Fatalf("set pin: %v", err)
	}
	token := result.(authResult).Token
	if token == "" {
		t.Fatal("expected auth token")
	}

	if _, err := registry.DispatchAuthenticated("config.update", mustJSON(t, map[string]any{
		"url": "https://example.com",
	})); err == nil {
		t.Fatal("expected protected command without token to fail")
	}

	if _, err := registry.DispatchAuthenticated("config.update", mustJSON(t, map[string]any{
		"_auth_token": token,
		"url":         "https://example.com",
	})); err != nil {
		t.Fatalf("expected protected command with token to pass: %v", err)
	}
}

func TestAdminPINLocksAfterRepeatedFailures(t *testing.T) {
	registry := NewRegistry(config.Config{
		ConfigPath: filepath.Join(t.TempDir(), "controller.json"),
		StorePath:  filepath.Join(t.TempDir(), "store.db"),
	})

	if _, err := registry.DispatchAuthenticated("auth.setPin", mustJSON(t, map[string]any{
		"pin": "129684",
	})); err != nil {
		t.Fatalf("set pin: %v", err)
	}

	for range pinMaxFailed {
		_, _ = registry.DispatchAuthenticated("auth.verifyPin", mustJSON(t, map[string]any{
			"pin": "111111",
		}))
	}

	status, err := registry.DispatchAuthenticated("auth.status", nil)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if !status.(authStatus).Locked {
		t.Fatal("expected PIN entry to lock after repeated failures")
	}
}
