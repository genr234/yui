package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/config"
)

func testAccountRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "yui-store.db"),
		ServerURL:       "http://127.0.0.1:9",
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
	return registry
}

func TestCompletedStoreReadDoesNotLandOnNextAccount(t *testing.T) {
	registry := testAccountRegistry(t)
	if _, err := registry.Dispatch("store.put", mustJSON(t, map[string]any{
		"collection":  "shell",
		"id":          "recent-apps",
		"store_epoch": uint64(0),
		"value":       []any{map[string]any{"id": "links"}},
	})); err != nil {
		t.Fatal(err)
	}
	got, err := registry.Dispatch("store.get", mustJSON(t, map[string]string{
		"collection": "shell",
		"id":         "recent-apps",
	}))
	if err != nil {
		t.Fatal(err)
	}
	epoch := got.(map[string]any)["store_epoch"].(uint64)

	entered := make(chan struct{})
	release := make(chan struct{})
	accountSwitchBoundary = func(*Registry) {
		accountSwitchBoundary = nil
		close(entered)
		<-release
	}
	t.Cleanup(func() { accountSwitchBoundary = nil })

	switchErr := make(chan error, 1)
	go func() {
		_, err := registry.Dispatch("accounts.switch", mustJSON(t, map[string]string{"account_id": "two"}))
		switchErr <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("switch did not publish")
	}

	putErr := make(chan error, 1)
	go func() {
		_, err := registry.Dispatch("store.put", mustJSON(t, map[string]any{
			"collection":  "shell",
			"id":          "recent-apps",
			"store_epoch": epoch,
			"value":       []any{map[string]any{"id": "from-old-read"}},
		}))
		putErr <- err
	}()
	close(release)

	if err := <-switchErr; err != nil {
		t.Fatal(err)
	}
	if err := <-putErr; err == nil || err.Error() != "store scope changed" {
		t.Fatalf("put from the completed read: %v", err)
	}
	got, err = registry.Dispatch("store.get", mustJSON(t, map[string]string{
		"collection": "shell",
		"id":         "recent-apps",
	}))
	if err != nil {
		t.Fatal(err)
	}
	doc := got.(map[string]any)
	if doc["value"] != nil {
		t.Fatalf("completed read wrote the new account: %+v", doc)
	}
	if doc["store_epoch"] == epoch {
		t.Fatalf("expected a new epoch, got %v", doc["store_epoch"])
	}
}

func TestSwitchBeforeFirstStoreReadUsesNewAccount(t *testing.T) {
	registry := testAccountRegistry(t)
	if _, err := registry.Dispatch("accounts.switch", mustJSON(t, map[string]string{"account_id": "two"})); err != nil {
		t.Fatal(err)
	}
	// The switch linearized before this get's first read, so the read belongs
	// to the new account. That is the accepted limitation.
	got, err := registry.Dispatch("store.get", mustJSON(t, map[string]string{
		"collection": "shell",
		"id":         "recent-apps",
	}))
	if err != nil {
		t.Fatal(err)
	}
	doc := got.(map[string]any)
	if doc["value"] != nil || doc["store_epoch"] == uint64(0) {
		t.Fatalf("read after the switch should see the new empty store, got %+v", doc)
	}
}

func TestAccountSwitchKeepsConcurrentConfigAndPIN(t *testing.T) {
	registry := testAccountRegistry(t)

	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		_, err := registry.Dispatch("config.update", mustJSON(t, map[string]any{"chrome_path": `C:\chrome\chrome.exe`}))
		errCh <- err
	}()
	go func() {
		defer wg.Done()
		_, err := registry.Dispatch("auth.setPin", mustJSON(t, map[string]any{"pin": "129684"}))
		errCh <- err
	}()
	go func() {
		defer wg.Done()
		_, err := registry.Dispatch("accounts.switch", mustJSON(t, map[string]string{"account_id": "two"}))
		errCh <- err
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ChromePath != `C:\chrome\chrome.exe` {
		t.Fatalf("chrome path lost: %q", loaded.ChromePath)
	}
	if loaded.ActiveAccountID != "two" {
		t.Fatalf("active account: %s", loaded.ActiveAccountID)
	}
	if loaded.AdminPIN.Hash == "" {
		t.Fatal("pin hash was lost")
	}
	for _, account := range loaded.Accounts {
		if account.DeviceToken == "" {
			t.Fatalf("account %s lost its device token", account.ID)
		}
	}
	if _, ok := findAccount(loaded.Accounts, "one"); !ok {
		t.Fatal("account one was dropped by the concurrent update")
	}
	status, err := registry.Dispatch("accounts.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status.(accountStatus).ActiveAccount.ID != "two" {
		t.Fatalf("status account: %+v", status)
	}
}

func TestReloadDoesNotTearAccountDuringSwitch(t *testing.T) {
	registry := testAccountRegistry(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				registry.reloadAccountConfig()
				cfg := registry.snapshotConfig()
				account, ok := findAccount(cfg.Accounts, cfg.ActiveAccountID)
				if !ok || account.DeviceToken == "" {
					t.Errorf("torn config active=%q accounts=%d", cfg.ActiveAccountID, len(cfg.Accounts))
					return
				}
				if account.ID == "one" && account.DeviceToken != "token-one" {
					t.Errorf("account one token: %s", account.DeviceToken)
					return
				}
				if account.ID == "two" && account.DeviceToken != "token-two" {
					t.Errorf("account two token: %s", account.DeviceToken)
					return
				}
			}
		}()
	}
	for i := 0; i < 12; i++ {
		id := "two"
		if i%2 == 1 {
			id = "one"
		}
		if _, err := registry.Dispatch("accounts.switch", mustJSON(t, map[string]string{"account_id": id})); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestAccountSwitchCancelsInFlightReconcileAndDropsCredentials(t *testing.T) {
	entered := make(chan struct{})
	var mu sync.Mutex
	var tokens []string
	var firstCancelled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		tokens = append(tokens, token)
		block := len(tokens) == 1
		mu.Unlock()
		if block {
			close(entered)
			<-r.Context().Done()
			mu.Lock()
			firstCancelled = true
			mu.Unlock()
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assets":[]}`))
	}))
	t.Cleanup(server.Close)

	registry := testAccountRegistry(t)
	if _, err := registry.mutateConfig(func(cfg *config.Config) error {
		cfg.ServerURL = server.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	previous := accountHTTPClient
	accountHTTPClient = server.Client()
	t.Cleanup(func() { accountHTTPClient = previous })

	ctx, cancel := context.WithCancel(context.Background())
	registry.StartAssetSync(ctx)
	t.Cleanup(cancel)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile did not reach the manifest")
	}

	started := time.Now()
	if _, err := registry.Dispatch("accounts.switch", mustJSON(t, map[string]string{"account_id": "two"})); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("switch waited on the old reconcile instead of cancelling it")
	}
	mu.Lock()
	cancelled := firstCancelled
	mu.Unlock()
	if !cancelled {
		t.Fatal("in-flight manifest was not cancelled")
	}

	deadline := time.Now().Add(3 * time.Second)
	var sawNew bool
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, token := range tokens {
			if token == "token-two" {
				sawNew = true
			}
		}
		mu.Unlock()
		if sawNew {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawNew {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("new account reconcile did not run with fresh credentials, tokens=%v", tokens)
	}
}
