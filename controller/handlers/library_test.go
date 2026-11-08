package handlers

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/library"
	"kiosk/controller/internal/native"
)

func libraryTestRegistry(t *testing.T) (*Registry, *[]string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "yui-store.db"),
		ActiveAccountID: "one",
		Accounts: []config.AccountConfig{
			{ID: "one", Name: "One", KioskID: "kiosk-one"},
			{ID: "two", Name: "Two", KioskID: "kiosk-two"},
		},
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(cfg)
	t.Cleanup(func() { _ = registry.Close() })
	var mu sync.Mutex
	events := []string{}
	registry.SetEmitter(func(event string, _ any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})
	return registry, &events
}

func TestLibraryCommandsArePerAccount(t *testing.T) {
	registry, events := libraryTestRegistry(t)
	if _, err := registry.DispatchAuthenticated("library.publish", mustJSON(t, map[string]any{
		"app_id": "dev.genr.arcade",
		"titles": []map[string]any{{"id": "RMCE01", "title": "Kart", "players": 4, "progress": 0.5, "art": "12"}},
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.DispatchAuthenticated("library.played", mustJSON(t, map[string]any{
		"app_id": "dev.genr.arcade", "id": "RMCE01",
	})); err != nil {
		t.Fatal(err)
	}
	result, err := registry.DispatchAuthenticated("library.recent", nil)
	if err != nil {
		t.Fatal(err)
	}
	recent := result.([]recentTitle)
	if len(recent) != 1 || recent[0].ID != "RMCE01" || recent[0].PlayedAt == "" || !recent[0].ArtAsset {
		t.Fatalf("recent = %+v", recent)
	}
	data, _ := json.Marshal(recent[0])
	var wire map[string]any
	_ = json.Unmarshal(data, &wire)
	if wire["app_id"] != "dev.genr.arcade" || wire["progress"] != 0.5 {
		t.Fatalf("wire shape = %s", data)
	}
	if len(*events) != 2 || (*events)[0] != "library.changed" {
		t.Fatalf("events = %v", *events)
	}

	if _, err := (AccountSwitchCommand{}).Handle(registry, mustJSON(t, map[string]string{"account_id": "two"})); err != nil {
		t.Fatal(err)
	}
	result, err = registry.DispatchAuthenticated("library.recent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.([]recentTitle); len(got) != 0 {
		t.Fatalf("account two sees account one's titles: %+v", got)
	}
	list, err := registry.DispatchAuthenticated("library.list", mustJSON(t, map[string]any{"app_id": "dev.genr.arcade"}))
	if err != nil || len(list.([]library.Title)) != 0 {
		t.Fatalf("list = %v, %v", list, err)
	}
}

func TestNativeStatusIdleBeforeAnySession(t *testing.T) {
	registry, _ := libraryTestRegistry(t)
	st, err := registry.DispatchAuthenticated("native.status", nil)
	if err != nil || st.(native.Status).State != native.StateIdle {
		t.Fatalf("native.status = %+v, %v", st, err)
	}
	if _, err := registry.DispatchAuthenticated("native.launch", mustJSON(t, map[string]any{
		"app_id": "dev.genr.arcade", "runtime": "404", "executable": "emu.exe",
	})); err == nil {
		t.Fatal("launch with a missing runtime asset succeeded")
	}
	st, _ = registry.DispatchAuthenticated("native.status", nil)
	if st.(native.Status).State != native.StateFailed {
		t.Fatalf("status after failed launch = %+v", st)
	}
}
