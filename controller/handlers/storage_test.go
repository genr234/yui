package handlers

import (
	"path/filepath"
	"testing"

	"kiosk/controller/internal/config"
)

func TestStoreCommandsRejectPlatformCollections(t *testing.T) {
	registry := testRegistry(t)

	_, err := StorePutCommand{}.Handle(registry, mustJSON(t, map[string]any{
		"collection": "installed-apps",
		"id":         "terminal",
		"value":      map[string]any{"name": "Terminal"},
	}))
	if err == nil {
		t.Fatal("expected generic store command to reject platform collection")
	}
}

func TestStoreCommandsAllowScopedStorageCollections(t *testing.T) {
	registry := testRegistry(t)

	if _, err := (StorePutCommand{}).Handle(registry, mustJSON(t, map[string]any{
		"collection": "app-storage",
		"id":         "demo:score",
		"value":      42,
	})); err != nil {
		t.Fatalf("put app storage: %v", err)
	}

	value, err := StoreGetCommand{}.Handle(registry, mustJSON(t, map[string]string{
		"collection": "app-storage",
		"id":         "demo:score",
	}))
	if err != nil {
		t.Fatalf("get app storage: %v", err)
	}

	doc := value.(map[string]any)
	if doc["id"] != "demo:score" || doc["value"] != float64(42) {
		t.Fatalf("unexpected document: %+v", doc)
	}

	keys, err := StoreKeysCommand{}.Handle(registry, mustJSON(t, map[string]any{
		"collection": "app-storage",
		"prefix":     "demo:",
	}))
	if err != nil {
		t.Fatalf("keys app storage: %v", err)
	}
	keyList := keys.([]string)
	if len(keyList) != 1 || keyList[0] != "demo:score" {
		t.Fatalf("unexpected keys: %+v", keyList)
	}
}

func TestStoreCommandsAllowShellCollection(t *testing.T) {
	registry := testRegistry(t)

	if _, err := (StorePutCommand{}).Handle(registry, mustJSON(t, map[string]any{
		"collection": "shell",
		"id":         "recent-apps",
		"value":      []any{map[string]any{"id": "links", "openedAt": float64(10)}},
	})); err != nil {
		t.Fatalf("put shell recent apps: %v", err)
	}

	value, err := StoreGetCommand{}.Handle(registry, mustJSON(t, map[string]string{
		"collection": "shell",
		"id":         "recent-apps",
	}))
	if err != nil {
		t.Fatalf("get shell recent apps: %v", err)
	}
	doc := value.(map[string]any)
	entries, _ := doc["value"].([]any)
	if doc["id"] != "recent-apps" || len(entries) != 1 {
		t.Fatalf("unexpected document: %+v", doc)
	}
	entry := entries[0].(map[string]any)
	if entry["id"] != "links" || entry["openedAt"] != float64(10) {
		t.Fatalf("unexpected recent-apps value: %+v", doc["value"])
	}
	if doc["store_epoch"] != uint64(0) {
		t.Fatalf("expected the initial store epoch, got %+v", doc["store_epoch"])
	}
}

func TestStorePutRejectsStaleEpochWithoutWriting(t *testing.T) {
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
	t.Cleanup(func() { _ = registry.Close() })

	if _, err := (StorePutCommand{}).Handle(registry, mustJSON(t, map[string]any{
		"collection":  "shell",
		"id":          "recent-apps",
		"store_epoch": uint64(0),
		"value":       []any{map[string]any{"id": "links", "openedAt": float64(10)}},
	})); err != nil {
		t.Fatalf("put account one: %v", err)
	}

	if _, err := (AccountSwitchCommand{}).Handle(registry, mustJSON(t, map[string]string{"account_id": "two"})); err != nil {
		t.Fatal(err)
	}

	_, err := (StorePutCommand{}).Handle(registry, mustJSON(t, map[string]any{
		"collection":  "shell",
		"id":          "recent-apps",
		"store_epoch": uint64(0),
		"value":       []any{map[string]any{"id": "links", "openedAt": float64(10)}},
	}))
	if err == nil || err.Error() != "store scope changed" {
		t.Fatalf("expected stale epoch to be refused, got %v", err)
	}

	got, err := StoreGetCommand{}.Handle(registry, mustJSON(t, map[string]string{
		"collection": "shell",
		"id":         "recent-apps",
	}))
	if err != nil {
		t.Fatal(err)
	}
	doc := got.(map[string]any)
	if doc["value"] != nil {
		t.Fatalf("stale write landed on the new account: %+v", doc)
	}
	if doc["store_epoch"] == uint64(0) {
		t.Fatalf("expected a new store epoch after the switch, got %+v", doc["store_epoch"])
	}

	if _, err := (AccountSwitchCommand{}).Handle(registry, mustJSON(t, map[string]string{"account_id": "one"})); err != nil {
		t.Fatal(err)
	}
	got, err = StoreGetCommand{}.Handle(registry, mustJSON(t, map[string]string{
		"collection": "shell",
		"id":         "recent-apps",
	}))
	if err != nil {
		t.Fatal(err)
	}
	doc = got.(map[string]any)
	entries, _ := doc["value"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["id"] != "links" {
		t.Fatalf("account one history changed: %+v", doc["value"])
	}
}

func TestStoreKeysReturnsEmptySliceForMissingCollection(t *testing.T) {
	registry := testRegistry(t)

	keys, err := StoreKeysCommand{}.Handle(registry, mustJSON(t, map[string]any{
		"collection": "app-storage",
		"prefix":     "missing:",
	}))
	if err != nil {
		t.Fatalf("keys app storage: %v", err)
	}
	keyList, ok := keys.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", keys)
	}
	if len(keyList) != 0 {
		t.Fatalf("expected no keys, got %+v", keyList)
	}
}
