package handlers

import (
	"path/filepath"
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
