package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"kiosk/controller/internal/config"
)

func TestAccountSwitchUsesSeparateStores(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "yui-store.db"),
		ServerURL:       "http://127.0.0.1:3000",
		ActiveAccountID: "one",
		Accounts: []config.AccountConfig{
			{ID: "one", Name: "One", KioskID: "kiosk-one", DeviceToken: "token-one"},
			{ID: "two", Name: "Two", KioskID: "kiosk-two", DeviceToken: "token-two"},
		},
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(cfg)
	t.Cleanup(func() {
		_ = registry.Close()
	})

	if _, err := (AppDevInstallCommand{}).Handle(registry, mustJSON(t, map[string]string{
		"entry":  "/workspace/apps/signed-test/app.yui.js",
		"source": signedTestAppSource,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := (StorageSetCommand{}).Handle(registry, mustJSON(t, map[string]string{
		"key":   "current",
		"value": "account-one",
	})); err != nil {
		t.Fatal(err)
	}

	if _, err := (AccountSwitchCommand{}).Handle(registry, mustJSON(t, map[string]string{"account_id": "two"})); err != nil {
		t.Fatal(err)
	}
	apps, err := (AppInstalledListCommand{}).Handle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps.([]installedAppRecord)) != 0 {
		t.Fatalf("expected account two to have no installed apps, got %+v", apps)
	}
	value, err := (StorageGetCommand{}).Handle(registry, mustJSON(t, map[string]string{"key": "current"}))
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatalf("expected account two storage to be empty, got %+v", value)
	}

	if _, err := (AccountSwitchCommand{}).Handle(registry, mustJSON(t, map[string]string{"account_id": "one"})); err != nil {
		t.Fatal(err)
	}
	apps, err = (AppInstalledListCommand{}).Handle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	installed := apps.([]installedAppRecord)
	if len(installed) != 1 || installed[0].ID != "signed.test" {
		t.Fatalf("expected account one installed app, got %+v", installed)
	}
	value, err = (StorageGetCommand{}).Handle(registry, mustJSON(t, map[string]string{"key": "current"}))
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || *(value.(*string)) != "account-one" {
		t.Fatalf("expected account one storage, got %+v", value)
	}
}

func TestMarkAccountNeedsPairingClearsStaleToken(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "yui-store.db"),
		ServerURL:       "http://127.0.0.1:3000",
		ActiveAccountID: "one",
		Accounts: []config.AccountConfig{
			{ID: "one", Name: "One", KioskID: "kiosk-one", DeviceToken: "token-one"},
		},
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(cfg)
	t.Cleanup(func() {
		_ = registry.Close()
	})

	if err := registry.markAccountNeedsPairing("one", errAccountUnauthorized.Error()); err != nil {
		t.Fatal(err)
	}

	status := registry.accountStatus()
	if status.Connected {
		t.Fatal("expected account to no longer be connected")
	}
	if !status.NeedsPairing {
		t.Fatal("expected account to need pairing")
	}
	if status.ActiveAccount == nil || status.ActiveAccount.ID != "one" {
		t.Fatalf("expected active account to remain selected, got %+v", status.ActiveAccount)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].DeviceToken != "" || cfg.Accounts[0].KioskID != "" {
		t.Fatalf("expected stale credentials to be cleared, got %+v", cfg.Accounts)
	}
}

func TestRequireOKClassifiesUnauthorized(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Status:     "401 Unauthorized",
		Body:       io.NopCloser(strings.NewReader(`{"error":"unauthorized"}`)),
	}
	err := requireOK(resp)
	if !errors.Is(err, errAccountUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestPushOperationsBatchesPendingOperations(t *testing.T) {
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/kiosk/sync/push" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token-one" {
			t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Operations []syncOperation `json:"operations"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		batchSizes = append(batchSizes, len(body.Operations))
		_, _ = w.Write([]byte(`{"accepted":[],"sync_cursor":0}`))
	}))
	defer server.Close()

	previousClient := accountHTTPClient
	accountHTTPClient = server.Client()
	t.Cleanup(func() {
		accountHTTPClient = previousClient
	})

	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "yui-store.db"),
		ServerURL:       server.URL,
		DeviceUID:       "device-one",
		ActiveAccountID: "one",
		Accounts: []config.AccountConfig{
			{ID: "one", Name: "One", KioskID: "kiosk-one", DeviceToken: "token-one"},
		},
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(cfg)
	t.Cleanup(func() {
		_ = registry.Close()
	})

	for i := 0; i < accountSyncBatchSize+1; i++ {
		if err := registry.appendOperation(defaultStorageCollection, fmt.Sprintf("key-%d", i), "put", mustJSON(t, i)); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := registry.pendingOperations()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.pushOperations(cfg.Accounts[0], pending); err != nil {
		t.Fatal(err)
	}

	if len(batchSizes) != 2 || batchSizes[0] != accountSyncBatchSize || batchSizes[1] != 1 {
		t.Fatalf("unexpected batch sizes: %+v", batchSizes)
	}
	pending, err = registry.pendingOperations()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected all operations synced, got %d pending", len(pending))
	}
}
