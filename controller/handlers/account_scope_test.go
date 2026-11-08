package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/store"

	"github.com/gorilla/websocket"
)

// scopedSyncRegistry is testAccountRegistry pointed at a fake Rails server.
func scopedSyncRegistry(t *testing.T, handler http.HandlerFunc) *Registry {
	t.Helper()
	server := httptest.NewServer(handler)
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
	return registry
}

func switchTo(t *testing.T, registry *Registry, id string) {
	t.Helper()
	if _, err := registry.Dispatch("accounts.switch", mustJSON(t, map[string]string{"account_id": id})); err != nil {
		t.Fatal(err)
	}
}

func outboxOf(t *testing.T, registry *Registry) []syncOperation {
	t.Helper()
	db, err := registry.Store()
	if err != nil {
		t.Fatal(err)
	}
	docs, err := db.Collection(syncOperationsCollection).List(store.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ops := make([]syncOperation, 0, len(docs))
	for _, doc := range docs {
		var op syncOperation
		if err := json.Unmarshal(doc.Value, &op); err != nil {
			t.Fatal(err)
		}
		ops = append(ops, op)
	}
	return ops
}

func shellValue(t *testing.T, registry *Registry, id string) any {
	t.Helper()
	got, err := registry.Dispatch("store.get", mustJSON(t, map[string]string{"collection": "shell", "id": id}))
	if err != nil {
		t.Fatal(err)
	}
	return got.(map[string]any)["value"]
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestSyncPushFinishingAfterSwitchKeepsNextAccountOutbox(t *testing.T) {
	pushEntered := make(chan struct{})
	releasePush := make(chan struct{})
	var pushes int32
	registry := scopedSyncRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/kiosk/sync/push":
			if atomic.AddInt32(&pushes, 1) == 1 {
				close(pushEntered)
				<-releasePush
			}
			_, _ = w.Write([]byte(`{}`))
		case "/api/kiosk/sync/pull":
			_, _ = w.Write([]byte(`{"operations":[],"sync_cursor":0}`))
		default:
			http.NotFound(w, r)
		}
	})

	// Account two has its own op 1. Op ids are per database.
	switchTo(t, registry, "two")
	if err := registry.appendOperation(registry.currentScope().epoch, "shell", "two-doc", "put", mustJSON(t, "two")); err != nil {
		t.Fatal(err)
	}
	switchTo(t, registry, "one")
	epoch := registry.currentScope().epoch
	for _, id := range []string{"one-a", "one-b"} {
		if err := registry.appendOperation(epoch, "shell", id, "put", mustJSON(t, "one")); err != nil {
			t.Fatal(err)
		}
	}

	syncErr := make(chan error, 1)
	go func() { syncErr <- registry.SyncNow() }()
	waitFor(t, pushEntered, "push")
	switchTo(t, registry, "two")
	close(releasePush)
	if err := <-syncErr; err != nil {
		t.Fatal(err)
	}

	ops := outboxOf(t, registry)
	if len(ops) != 1 || ops[0].RecordID != "two-doc" || ops[0].Synced {
		t.Fatalf("account two outbox was changed by account one's push: %+v", ops)
	}

	switchTo(t, registry, "one")
	pending, err := registry.pendingOperations(registry.currentScope().epoch)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("account one ops should stay pending for the next push, got %+v", pending)
	}
}

func TestSyncPullFinishingAfterSwitchDoesNotApplyOrAdvanceCursor(t *testing.T) {
	pullEntered := make(chan struct{})
	releasePull := make(chan struct{})
	var pulls int32
	registry := scopedSyncRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/kiosk/sync/push":
			_, _ = w.Write([]byte(`{}`))
		case "/api/kiosk/sync/pull":
			if atomic.AddInt32(&pulls, 1) == 1 {
				close(pullEntered)
				<-releasePull
			}
			if r.Header.Get("Authorization") != "Bearer token-one" {
				_, _ = w.Write([]byte(`{"operations":[],"sync_cursor":0}`))
				return
			}
			_, _ = w.Write([]byte(`{"operations":[{"id":"9","client_id":"phone","client_seq":1,"server_seq":42,` +
				`"collection":"shell","action":"replace_collection","payload":[{"id":"from-one","value":1}]}],` +
				`"sync_cursor":42,"has_more":false}`))
		default:
			http.NotFound(w, r)
		}
	})

	switchTo(t, registry, "two")
	if _, err := registry.Dispatch("store.put", mustJSON(t, map[string]any{"collection": "shell", "id": "keep", "value": "two"})); err != nil {
		t.Fatal(err)
	}
	switchTo(t, registry, "one")

	syncErr := make(chan error, 1)
	go func() { syncErr <- registry.SyncNow() }()
	waitFor(t, pullEntered, "pull")
	switchTo(t, registry, "two")
	close(releasePull)
	if err := <-syncErr; err != nil {
		t.Fatal(err)
	}

	if got := shellValue(t, registry, "keep"); got != "two" {
		t.Fatalf("account one's replace_collection cleared account two: keep=%v", got)
	}
	if got := shellValue(t, registry, "from-one"); got != nil {
		t.Fatalf("account one's op landed in account two: %v", got)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if one, _ := findAccount(loaded.Accounts, "one"); one == nil || one.SyncCursor != 0 {
		t.Fatalf("account one cursor advanced past ops it never applied: %+v", one)
	}

	// Back on account one, the next pull delivers the page.
	switchTo(t, registry, "one")
	if err := registry.SyncNow(); err != nil {
		t.Fatal(err)
	}
	if got := shellValue(t, registry, "from-one"); got != float64(1) {
		t.Fatalf("account one did not get its op: %v", got)
	}
	loaded, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if one, _ := findAccount(loaded.Accounts, "one"); one == nil || one.SyncCursor != 42 {
		t.Fatalf("account one cursor: %+v", one)
	}
}

// blockingCommand runs inner, then waits for release before returning.
type blockingCommand struct {
	inner   Command
	entered chan struct{}
	release chan struct{}
}

func (c blockingCommand) Name() string { return c.inner.Name() }

func (c blockingCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	out, err := c.inner.Handle(r, params)
	close(c.entered)
	<-c.release
	return out, err
}

func TestRecordedWriteAndOutboxEntryLandInSameAccount(t *testing.T) {
	registry := testAccountRegistry(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	registry.Register(blockingCommand{inner: StorePutCommand{}, entered: entered, release: release})

	putErr := make(chan error, 1)
	go func() {
		_, err := registry.DispatchAuthenticated("store.put", mustJSON(t, map[string]any{
			"collection":  "shell",
			"id":          "recent-apps",
			"store_epoch": registry.currentScope().epoch,
			"value":       []any{"links"},
		}))
		putErr <- err
	}()
	waitFor(t, entered, "store.put write")

	switchErr := make(chan error, 1)
	go func() {
		_, err := registry.Dispatch("accounts.switch", mustJSON(t, map[string]string{"account_id": "two"}))
		switchErr <- err
	}()
	select {
	case err := <-switchErr:
		t.Fatalf("switch published between a write and its outbox entry: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-putErr; err != nil {
		t.Fatal(err)
	}
	if err := <-switchErr; err != nil {
		t.Fatal(err)
	}

	if ops := outboxOf(t, registry); len(ops) != 0 {
		t.Fatalf("account one's outbox entry landed in account two: %+v", ops)
	}
	switchTo(t, registry, "one")
	ops := outboxOf(t, registry)
	if len(ops) != 1 || ops[0].Collection != "shell" || ops[0].RecordID != "recent-apps" {
		t.Fatalf("account one outbox: %+v", ops)
	}
}

func TestStoreDeleteRefusesStaleEpoch(t *testing.T) {
	registry := testAccountRegistry(t)
	if _, err := registry.Dispatch("store.put", mustJSON(t, map[string]any{"collection": "shell", "id": "recent-apps", "value": 1})); err != nil {
		t.Fatal(err)
	}
	stale := registry.currentScope().epoch
	switchTo(t, registry, "two")
	if _, err := registry.Dispatch("store.put", mustJSON(t, map[string]any{"collection": "shell", "id": "recent-apps", "value": 2})); err != nil {
		t.Fatal(err)
	}

	_, err := registry.Dispatch("store.delete", mustJSON(t, map[string]any{"collection": "shell", "id": "recent-apps", "store_epoch": stale}))
	if err == nil || err.Error() != "store scope changed" {
		t.Fatalf("delete with a stale epoch: %v", err)
	}
	if got := shellValue(t, registry, "recent-apps"); got != float64(2) {
		t.Fatalf("stale delete removed account two's document: %v", got)
	}

	current := registry.currentScope().epoch
	if _, err := registry.Dispatch("store.delete", mustJSON(t, map[string]any{"collection": "shell", "id": "recent-apps", "store_epoch": current})); err != nil {
		t.Fatal(err)
	}
	if got := shellValue(t, registry, "recent-apps"); got != nil {
		t.Fatalf("delete with the current epoch: %v", got)
	}
	// Without store_epoch the delete works as before.
	if _, err := registry.Dispatch("store.delete", mustJSON(t, map[string]any{"collection": "shell", "id": "missing"})); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch("store.delete", mustJSON(t, map[string]any{"collection": "installed-apps", "id": "x"})); err == nil {
		t.Fatal("store.delete must still reject platform collections")
	}
}

func TestReplaceConfigIsAnAccountTransition(t *testing.T) {
	registry := testAccountRegistry(t)
	if _, err := registry.Dispatch("store.put", mustJSON(t, map[string]any{"collection": "shell", "id": "recent-apps", "value": "one"})); err != nil {
		t.Fatal(err)
	}
	before := registry.currentScope().epoch

	imported := registry.snapshotConfig()
	imported.ActiveAccountID = ""
	imported.Accounts = nil
	imported.ServerURL = ""
	if err := registry.replaceConfig(imported); err != nil {
		t.Fatal(err)
	}

	if after := registry.currentScope().epoch; after == before {
		t.Fatal("reimport did not advance the store epoch")
	}
	_, err := registry.Dispatch("store.put", mustJSON(t, map[string]any{
		"collection": "shell", "id": "recent-apps", "value": "late", "store_epoch": before,
	}))
	if err == nil || err.Error() != "store scope changed" {
		t.Fatalf("put with the pre-reimport epoch: %v", err)
	}
	if got := shellValue(t, registry, "recent-apps"); got != nil {
		t.Fatalf("reimport kept serving account one's store: %v", got)
	}
}

type fakeCable struct {
	mu      sync.Mutex
	conns   chan string
	closed  chan string
	patches []string
}

func (f *fakeCable) handle(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	switch {
	case r.URL.Path == "/cable":
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		f.conns <- token
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				f.closed <- token
				return
			}
		}
	case r.Method == http.MethodPatch:
		f.mu.Lock()
		f.patches = append(f.patches, token+" "+r.URL.Path)
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeCable) patchList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.patches...)
}

func expectToken(t *testing.T, ch <-chan string, want, what string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("%s: got %q, want %q", what, got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestAccountSwitchReconnectsCommandSocket(t *testing.T) {
	cable := &fakeCable{conns: make(chan string, 4), closed: make(chan string, 4)}
	registry := scopedSyncRegistry(t, cable.handle)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go registry.accountCommandLoop(ctx)

	expectToken(t, cable.conns, "Bearer token-one", "first socket")
	switchTo(t, registry, "two")
	// The reconnect must not wait for accountReconnectDelay.
	expectToken(t, cable.closed, "Bearer token-one", "old socket close")
	expectToken(t, cable.conns, "Bearer token-two", "socket for the new account")
}

func TestRemoteCommandFromPreviousAccountIsNotRun(t *testing.T) {
	cable := &fakeCable{conns: make(chan string, 4), closed: make(chan string, 4)}
	registry := scopedSyncRegistry(t, cable.handle)
	scopeOne := registry.currentScope()

	switchTo(t, registry, "two")
	db, err := registry.Store()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Collection(installedAppsCollection).Put("game", map[string]string{"id": "game"}); err != nil {
		t.Fatal(err)
	}
	command := mustJSON(t, map[string]any{"id": "c1", "command_type": "apps.uninstall", "payload": map[string]string{"id": "game"}})

	if err := registry.handleRemoteCommandMessage(scopeOne, command); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.Collection(installedAppsCollection).Get("game"); err != nil || !ok {
		t.Fatalf("account one's command ran against account two: ok=%v err=%v", ok, err)
	}
	if patches := cable.patchList(); len(patches) != 0 {
		t.Fatalf("stale command was reported: %v", patches)
	}
	if ops := outboxOf(t, registry); len(ops) != 0 {
		t.Fatalf("stale command recorded in account two's outbox: %+v", ops)
	}

	if err := registry.handleRemoteCommandMessage(registry.currentScope(), command); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.Collection(installedAppsCollection).Get("game"); ok {
		t.Fatal("account two's own command did not run")
	}
	if patches := cable.patchList(); len(patches) != 1 || patches[0] != "Bearer token-two /api/kiosk/commands/c1" {
		t.Fatalf("command reported with the wrong account: %v", patches)
	}
}
