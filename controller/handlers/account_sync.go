package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"kiosk/controller/internal/telemetry"
	"kiosk/controller/internal/version"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/native"
	"kiosk/controller/internal/store"

	"github.com/gorilla/websocket"
)

const (
	syncOperationsCollection = "sync-operations"
	syncMetaCollection       = "sync-meta"
	syncMetaStateID          = "state"
	accountSyncInterval      = 5 * time.Second
	accountReconnectDelay    = 5 * time.Second
	accountSyncBatchSize     = 250
)

var accountHTTPClient = &http.Client{Timeout: 20 * time.Second}

var errAccountUnauthorized = errors.New("account authorization expired; reconnect this account")

// errCommandStreamSwitched ends a command socket whose account is no longer
// active. The loop reconnects at once for the next account.
var errCommandStreamSwitched = errors.New("account changed; reconnecting command stream")

var syncCollections = []string{
	defaultStorageCollection,
	appSourcesCollection,
	appCatalogCollection,
	installedAppsCollection,
	appStorageCollection,
	pluginSourcesCollection,
	pluginCatalogCollection,
	installedPluginsCollection,
	pluginStateCollection,
	pluginSettingsCollection,
	pluginSecretsCollection,
	pluginStorageCollection,
	"shell",
}

type syncOperation struct {
	ID         string          `json:"id"`
	ClientID   string          `json:"client_id"`
	ClientSeq  int64           `json:"client_seq"`
	ServerSeq  int64           `json:"server_seq,omitempty"`
	Collection string          `json:"collection"`
	RecordID   string          `json:"record_id,omitempty"`
	Action     string          `json:"action"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	OccurredAt string          `json:"occurred_at,omitempty"`
	Synced     bool            `json:"synced,omitempty"`
}

type remoteOperation struct {
	ID         string          `json:"id"`
	ClientID   string          `json:"client_id"`
	ClientSeq  int64           `json:"client_seq"`
	ServerSeq  int64           `json:"server_seq"`
	Collection string          `json:"collection"`
	RecordID   string          `json:"record_id"`
	Action     string          `json:"action"`
	Payload    json.RawMessage `json:"payload"`
}

type syncMeta struct {
	ClientID string `json:"client_id"`
	NextSeq  int64  `json:"next_seq"`
}

func (r *Registry) StartAccountSync(ctx context.Context) {
	go func() {
		dir := filepath.Join(r.snapshotConfig().ConfigDir, "runtimes")
		if n := native.CloseLeftovers(dir); n > 0 {
			telemetry.Record("controller_closed_leftover_programs", map[string]any{"count": n})
		}
	}()
	go r.accountLoop(ctx)
	go r.accountCommandLoop(ctx)
	r.StartAssetSync(ctx)
	go telemetry.Run(ctx, r.assetEndpointFunc(&assetEndpointCache{}), version.Version, version.Commit, time.Minute)
}

func (r *Registry) accountLoop(ctx context.Context) {
	ticker := time.NewTicker(accountSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.SyncNow(); err != nil {
				log.Printf("account sync failed: %v", err)
			}
		}
	}
}

func (r *Registry) accountCommandLoop(ctx context.Context) {
	for {
		err := r.streamRemoteCommands(ctx)
		if errors.Is(err, errCommandStreamSwitched) && ctx.Err() == nil {
			continue
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("account command websocket failed: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(accountReconnectDelay):
		}
	}
}

// SyncNow pushes and pulls for the account active when it starts. Every
// store read and write checks that account's store epoch under the store
// lock. If the account changes mid-sync, the rest is dropped: nothing lands in
// the next account's database, and the previous account's ops stay pending
// and its cursor stays put until it is active again.
func (r *Registry) SyncNow() error {
	scope := r.refreshedScope()
	if !scope.connected() {
		return nil
	}
	r.setSyncState(true, "")
	defer r.setSyncing(false)

	err := r.syncScope(scope)
	if errors.Is(err, errStoreScopeChanged) {
		return nil
	}
	if err != nil {
		if errors.Is(err, errAccountUnauthorized) {
			_ = r.markAccountNeedsPairing(scope.account.ID, err.Error())
		}
		r.setSyncState(false, err.Error())
		return err
	}
	r.setSyncState(false, "")
	return nil
}

func (r *Registry) syncScope(scope storeScope) error {
	pending, err := r.pendingOperations(scope.epoch)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		if err := r.pushOperations(scope, pending); err != nil {
			return err
		}
	}
	return r.pullOperations(scope)
}

func (r *Registry) refreshActiveAccount() error {
	scope := r.refreshedScope()
	if !scope.connected() {
		return nil
	}
	if err := r.pullOperations(scope); err != nil {
		if errors.Is(err, errStoreScopeChanged) {
			return nil
		}
		if errors.Is(err, errAccountUnauthorized) {
			if markErr := r.markAccountNeedsPairing(scope.account.ID, err.Error()); markErr != nil {
				return markErr
			}
		}
		return err
	}
	return nil
}

func (r *Registry) activeAccount() (config.AccountConfig, bool) {
	cfg := r.snapshotConfig()
	account, ok := findAccount(cfg.Accounts, cfg.ActiveAccountID)
	if !ok {
		return config.AccountConfig{}, false
	}
	return *account, true
}

func (r *Registry) setSyncState(syncing bool, errText string) {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	r.syncState.Syncing = syncing
	r.syncState.LastSyncAt = time.Now().UTC().Format(time.RFC3339)
	r.syncState.LastSyncError = errText
}

func (r *Registry) setSyncing(syncing bool) {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	r.syncState.Syncing = syncing
}

func (r *Registry) markAccountNeedsPairing(accountID, reason string) error {
	if accountID == "" {
		return nil
	}
	_, err := r.mutateConfig(func(cfg *config.Config) error {
		for i := range cfg.Accounts {
			if cfg.Accounts[i].ID != accountID {
				continue
			}
			if cfg.Accounts[i].DeviceToken != "" || cfg.Accounts[i].KioskID != "" {
				cfg.Accounts[i].DeviceToken = ""
				cfg.Accounts[i].KioskID = ""
				return nil
			}
			return errConfigUnchanged
		}
		return errConfigUnchanged
	})
	if errors.Is(err, errConfigUnchanged) {
		return nil
	}
	if err != nil {
		return err
	}
	r.setSyncState(false, reason)
	return nil
}

// recordsMutation lists the methods recordMutationErr writes an outbox entry
// for. DispatchAuthenticated holds mutationMu for reading around them.
func recordsMutation(method string) bool {
	switch method {
	case "storage.set", "storage.delete", "store.put", "store.delete",
		"apps.sources.add", "apps.sources.refresh", "apps.sources.remove",
		"apps.install", "apps.dev.install", "apps.uninstall",
		"plugins.sources.add", "plugins.sources.refresh", "plugins.sources.remove",
		"plugins.install", "plugins.uninstall", "plugins.enable", "plugins.disable",
		"plugins.permissions.update", "plugins.administrator.update", "plugins.settings.update":
		return true
	default:
		return false
	}
}

// recordMutation appends the outbox entry for a command to the store at
// epoch, the one the command ran against. The caller holds mutationMu for
// reading, so an account transition cannot publish between the command's
// write and this entry. If the store has moved on anyway, the entry is
// dropped rather than written to another account's outbox.
func (r *Registry) recordMutation(epoch uint64, method string, params json.RawMessage, result any) {
	cfg := r.snapshotConfig()
	if cfg.ActiveAccountID == "" || cfg.ServerURL == "" {
		return
	}
	if err := r.recordMutationErr(epoch, method, params, result); err != nil {
		log.Printf("sync operation record failed for %s: %v", method, err)
	}
}

func (r *Registry) recordMutationErr(epoch uint64, method string, params json.RawMessage, result any) error {
	switch method {
	case "storage.set":
		var p struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordPut(epoch, defaultStorageCollection, p.Key, p.Value)
	case "storage.delete":
		var p struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(epoch, defaultStorageCollection, p.Key)
	case "store.put":
		var p valueParams
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordPutRaw(epoch, p.Collection, p.ID, p.Value)
	case "store.delete":
		var p documentParams
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(epoch, p.Collection, p.ID)
	case "apps.sources.add", "apps.sources.refresh":
		if record, ok := result.(appSourceRecord); ok {
			if err := r.recordPut(epoch, appSourcesCollection, record.ID, record); err != nil {
				return err
			}
		}
		return r.recordReplaceCollection(epoch, appCatalogCollection)
	case "apps.sources.remove":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		if err := r.recordDelete(epoch, appSourcesCollection, p.ID); err != nil {
			return err
		}
		return r.recordReplaceCollection(epoch, appCatalogCollection)
	case "apps.install", "apps.dev.install":
		if record, ok := result.(installedAppRecord); ok {
			return r.recordPut(epoch, installedAppsCollection, record.ID, record)
		}
	case "apps.uninstall":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(epoch, installedAppsCollection, p.ID)
	case "plugins.sources.add", "plugins.sources.refresh":
		if record, ok := result.(pluginSourceRecord); ok {
			if err := r.recordPut(epoch, pluginSourcesCollection, record.ID, record); err != nil {
				return err
			}
		}
		return r.recordReplaceCollection(epoch, pluginCatalogCollection)
	case "plugins.sources.remove":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		if err := r.recordDelete(epoch, pluginSourcesCollection, p.ID); err != nil {
			return err
		}
		return r.recordReplaceCollection(epoch, pluginCatalogCollection)
	case "plugins.install":
		if record, ok := result.(installedPluginRecord); ok {
			if err := r.recordPut(epoch, installedPluginsCollection, record.ID, record); err != nil {
				return err
			}
			return r.recordReplaceCollection(epoch, pluginStateCollection)
		}
	case "plugins.uninstall":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(epoch, installedPluginsCollection, p.ID)
	case "plugins.enable", "plugins.disable", "plugins.permissions.update", "plugins.administrator.update":
		return r.recordReplaceCollection(epoch, pluginStateCollection)
	case "plugins.settings.update":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		if err := r.ensureDeviceUID(); err != nil {
			return err
		}
		return r.withStoreAt(epoch, func(db *store.DB) error {
			doc, ok, err := db.Collection(pluginSettingsCollection).Get(p.ID)
			if err != nil || !ok {
				return err
			}
			return r.appendOperationDB(db, pluginSettingsCollection, p.ID, "put", doc.Value)
		})
	}
	return nil
}

func (r *Registry) recordPut(epoch uint64, collection, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.recordPutRaw(epoch, collection, id, data)
}

func (r *Registry) recordPutRaw(epoch uint64, collection, id string, value json.RawMessage) error {
	return r.appendOperation(epoch, collection, id, "put", value)
}

func (r *Registry) recordDelete(epoch uint64, collection, id string) error {
	return r.appendOperation(epoch, collection, id, "delete", nil)
}

func (r *Registry) recordReplaceCollection(epoch uint64, collection string) error {
	if err := r.ensureDeviceUID(); err != nil {
		return err
	}
	return r.withStoreAt(epoch, func(db *store.DB) error {
		docs, err := db.Collection(collection).List(store.ListOptions{})
		if err != nil {
			return err
		}
		payloadDocs := make([]map[string]any, 0, len(docs))
		for _, doc := range docs {
			var value any
			if err := json.Unmarshal(doc.Value, &value); err != nil {
				return err
			}
			payloadDocs = append(payloadDocs, map[string]any{"id": doc.ID, "value": value})
		}
		data, err := json.Marshal(payloadDocs)
		if err != nil {
			return err
		}
		return r.appendOperationDB(db, collection, "", "replace_collection", data)
	})
}

// appendOperation adds one outbox entry to the store at epoch, or returns
// errStoreScopeChanged if that store is no longer active.
func (r *Registry) appendOperation(epoch uint64, collection, id, action string, payload json.RawMessage) error {
	if err := r.ensureDeviceUID(); err != nil {
		return err
	}
	return r.withStoreAt(epoch, func(db *store.DB) error {
		return r.appendOperationDB(db, collection, id, action, payload)
	})
}

// ensureDeviceUID saves a device id before the first outbox entry. It takes
// configMu, so it must run before withStoreAt, never inside it.
func (r *Registry) ensureDeviceUID() error {
	if r.snapshotConfig().DeviceUID != "" {
		return nil
	}
	_, err := r.mutateConfig(func(cfg *config.Config) error {
		if cfg.DeviceUID == "" {
			cfg.DeviceUID = newDeviceUID()
		}
		return nil
	})
	return err
}

// appendOperationDB writes the entry and the next sequence number. The
// caller holds storeMu through withStoreAt.
func (r *Registry) appendOperationDB(db *store.DB, collection, id, action string, payload json.RawMessage) error {
	meta, err := r.syncMeta(db)
	if err != nil {
		return err
	}
	seq := meta.NextSeq
	if seq <= 0 {
		seq = 1
	}
	meta.NextSeq = seq + 1
	op := syncOperation{
		ID:         fmt.Sprintf("%020d", seq),
		ClientID:   meta.ClientID,
		ClientSeq:  seq,
		Collection: collection,
		RecordID:   id,
		Action:     action,
		Payload:    payload,
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := db.Collection(syncOperationsCollection).Put(op.ID, op); err != nil {
		return err
	}
	return db.Collection(syncMetaCollection).Put(syncMetaStateID, meta)
}

func (r *Registry) syncMeta(db *store.DB) (syncMeta, error) {
	deviceUID := r.snapshotConfig().DeviceUID
	meta := syncMeta{ClientID: deviceUID, NextSeq: 1}
	_, err := db.Collection(syncMetaCollection).Decode(syncMetaStateID, &meta)
	if err != nil {
		return meta, err
	}
	if meta.ClientID == "" {
		meta.ClientID = deviceUID
	}
	if meta.ClientID == "" {
		meta.ClientID = newDeviceUID()
	}
	if meta.NextSeq <= 0 {
		meta.NextSeq = 1
	}
	return meta, nil
}

func (r *Registry) pendingOperations(epoch uint64) ([]syncOperation, error) {
	var ops []syncOperation
	err := r.withStoreAt(epoch, func(db *store.DB) error {
		docs, err := db.Collection(syncOperationsCollection).List(store.ListOptions{})
		if err != nil {
			return err
		}
		ops = make([]syncOperation, 0, len(docs))
		for _, doc := range docs {
			var op syncOperation
			if err := json.Unmarshal(doc.Value, &op); err != nil {
				return err
			}
			if !op.Synced {
				ops = append(ops, op)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ops, nil
}

func (r *Registry) pushOperations(scope storeScope, ops []syncOperation) error {
	for start := 0; start < len(ops); start += accountSyncBatchSize {
		end := start + accountSyncBatchSize
		if end > len(ops) {
			end = len(ops)
		}
		batch := ops[start:end]
		if err := r.pushOperationBatch(scope, batch); err != nil {
			return err
		}
	}
	return nil
}

// pushOperationBatch sends ops for scope's account and marks them synced in
// that account's database. If the account changed during the request, the
// ops stay pending there and are pushed again next time; ids are per
// database, so marking them in the next account would overwrite its ops.
func (r *Registry) pushOperationBatch(scope storeScope, ops []syncOperation) error {
	body, err := json.Marshal(map[string]any{"operations": ops})
	if err != nil {
		return err
	}
	req, err := accountRequest(scope.cfg.ServerURL, http.MethodPost, "/api/kiosk/sync/push", scope.account, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := accountHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := requireOK(resp); err != nil {
		return err
	}
	return r.withStoreAt(scope.epoch, func(db *store.DB) error {
		for _, op := range ops {
			op.Synced = true
			if err := db.Collection(syncOperationsCollection).Put(op.ID, op); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Registry) pullOperations(scope storeScope) error {
	account := scope.account
	for {
		next, hasMore, err := r.pullOperationPage(scope, account)
		if err != nil {
			return err
		}
		account = next
		if !hasMore {
			return nil
		}
	}
}

// pullOperationPage applies one page to scope's database and only then
// advances the cursor. A page that arrives after the account changed is not
// applied and the cursor is not saved, so the account gets it next time.
func (r *Registry) pullOperationPage(scope storeScope, account config.AccountConfig) (config.AccountConfig, bool, error) {
	path := fmt.Sprintf("/api/kiosk/sync/pull?cursor=%d", account.SyncCursor)
	req, err := accountRequest(scope.cfg.ServerURL, http.MethodGet, path, account, nil)
	if err != nil {
		return account, false, err
	}
	resp, err := accountHTTPClient.Do(req)
	if err != nil {
		return account, false, err
	}
	defer resp.Body.Close()
	if err := requireOK(resp); err != nil {
		return account, false, err
	}
	var result struct {
		Operations []remoteOperation `json:"operations"`
		SyncCursor int64             `json:"sync_cursor"`
		HasMore    bool              `json:"has_more"`
		Account    struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			ProfileImageURL string `json:"profile_image_url"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return account, false, err
	}
	if err := r.applyRemoteOperations(scope.epoch, result.Operations); err != nil {
		return account, false, err
	}
	if result.Account.ID != "" {
		account.ID = result.Account.ID
	}
	if result.Account.Name != "" {
		account.Name = result.Account.Name
	}
	account.ProfileImageURL = result.Account.ProfileImageURL
	account.SyncCursor = result.SyncCursor
	return account, result.HasMore, r.saveAccount(account)
}

// applyRemoteOperations writes ops into the store at epoch. It returns
// errStoreScopeChanged without writing if that store is no longer active.
func (r *Registry) applyRemoteOperations(epoch uint64, ops []remoteOperation) error {
	touchedPlugins := false
	err := r.withStoreAt(epoch, func(db *store.DB) error {
		meta, err := r.syncMeta(db)
		if err != nil {
			return err
		}
		for _, op := range ops {
			if op.ClientID == meta.ClientID {
				continue
			}
			if collectionAffectsPlugins(op.Collection) {
				touchedPlugins = true
			}
			switch op.Action {
			case "put":
				if err := db.Collection(op.Collection).Put(op.RecordID, op.Payload); err != nil {
					return err
				}
			case "delete":
				if err := db.Collection(op.Collection).Delete(op.RecordID); err != nil {
					return err
				}
			case "replace_collection":
				if err := db.Collection(op.Collection).Clear(); err != nil {
					return err
				}
				var docs []struct {
					ID    string          `json:"id"`
					Value json.RawMessage `json:"value"`
				}
				if err := json.Unmarshal(op.Payload, &docs); err != nil {
					return err
				}
				for _, doc := range docs {
					if err := db.Collection(op.Collection).Put(doc.ID, doc.Value); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if touchedPlugins && r.plugins != nil && r.pluginCtx != nil {
		r.plugins.Stop()
		r.plugins.Start(r.pluginCtx)
	}
	return nil
}

func collectionAffectsPlugins(collection string) bool {
	switch collection {
	case installedPluginsCollection, pluginStateCollection, pluginSettingsCollection, pluginSecretsCollection, pluginStorageCollection:
		return true
	default:
		return false
	}
}

// saveAccount stores the profile and cursor from a pull. It edits the cached
// entry in place: a pull that finishes after the account was disconnected
// does not add it back, and it does not restore a token cleared meanwhile.
func (r *Registry) saveAccount(account config.AccountConfig) error {
	_, err := r.mutateConfig(func(cfg *config.Config) error {
		for i := range cfg.Accounts {
			if cfg.Accounts[i].ID != account.ID {
				continue
			}
			cfg.Accounts[i].Name = account.Name
			cfg.Accounts[i].ProfileImageURL = account.ProfileImageURL
			cfg.Accounts[i].SyncCursor = account.SyncCursor
			return nil
		}
		return errConfigUnchanged
	})
	if errors.Is(err, errConfigUnchanged) {
		return nil
	}
	return err
}

// accountRequest builds a request for account against serverURL. Both come
// from the same scope, so a token is never sent to another account's server.
func accountRequest(serverURL, method, path string, account config.AccountConfig, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, strings.TrimRight(serverURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+account.DeviceToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func requireOK(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %s", errAccountUnauthorized, strings.TrimSpace(string(text)))
	}
	return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(text)))
}

func remoteCommandAllowed(method string) bool {
	switch method {
	case "apps.sources.add", "apps.sources.remove", "apps.sources.refresh",
		"apps.install", "apps.uninstall",
		"plugins.sources.add", "plugins.sources.remove", "plugins.sources.refresh",
		"plugins.install", "plugins.uninstall", "plugins.enable", "plugins.disable",
		"plugins.permissions.update", "plugins.settings.update",
		"assets.reconcile",
		"update.check", "update.apply",
		"kiosk.status", "kiosk.restartChrome", "kiosk.screenshot", "kiosk.input":
		return true
	default:
		return false
	}
}

// streamRemoteCommands keeps one KioskCommandsChannel socket open for the
// account active when it starts. An account transition cancels it through
// cancelCommandStream, and it returns errCommandStreamSwitched so the loop
// reconnects for the next account.
func (r *Registry) streamRemoteCommands(ctx context.Context) error {
	scope := r.refreshedScope()
	if !scope.connected() {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(accountReconnectDelay):
			return nil
		}
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !r.bindCommandStream(scope.epoch, cancel) {
		return errCommandStreamSwitched
	}
	defer r.bindCommandStream(scope.epoch, nil)
	ended := func(err error) error {
		if ctx.Err() == nil && streamCtx.Err() != nil {
			return errCommandStreamSwitched
		}
		return err
	}

	wsURL, err := accountCableURL(scope.cfg.ServerURL)
	if err != nil {
		return err
	}
	header, err := accountCableHeader(scope.cfg.ServerURL, scope.account.DeviceToken)
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(streamCtx, wsURL, header)
	if err != nil {
		return ended(err)
	}
	defer conn.Close()
	go func() {
		<-streamCtx.Done()
		_ = conn.Close()
	}()

	identifier, err := json.Marshal(map[string]string{"channel": "KioskCommandsChannel"})
	if err != nil {
		return err
	}
	subscribe, err := json.Marshal(map[string]string{
		"command":    "subscribe",
		"identifier": string(identifier),
	})
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, subscribe); err != nil {
		return err
	}

	for {
		select {
		case <-streamCtx.Done():
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return ended(nil)
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return ended(err)
		}
		var message struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		if len(message.Message) == 0 {
			continue
		}
		if err := r.handleRemoteCommandMessage(scope, message.Message); err != nil {
			log.Printf("account command execution failed: %v", err)
		}
	}
}

func accountCableURL(serverURL string) (string, error) {
	parsed, _, err := accountCableEndpoint(serverURL)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// accountCableHeader authenticates the kiosk and sets Origin to the server's
// own origin. Production Rails rejects a foreign Origin for every connection.
// A missing Origin is accepted only after this bearer token authenticates a
// kiosk. Sending the server Origin is what makes the same-origin check pass.
func accountCableHeader(serverURL, token string) (http.Header, error) {
	_, origin, err := accountCableEndpoint(serverURL)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Origin", origin)
	return header, nil
}

func accountCableEndpoint(serverURL string) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimRight(serverURL, "/") + "/cable")
	if err != nil {
		return nil, "", err
	}
	originScheme := ""
	switch parsed.Scheme {
	case "http":
		originScheme = "http"
		parsed.Scheme = "ws"
	case "https":
		originScheme = "https"
		parsed.Scheme = "wss"
	case "ws":
		originScheme = "http"
	case "wss":
		originScheme = "https"
	default:
		return nil, "", fmt.Errorf("unsupported server URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, "", fmt.Errorf("server URL is missing a host")
	}
	return parsed, originScheme + "://" + parsed.Host, nil
}

// bindCommandStream records cancel as the closer of the live command socket
// (nil clears it). It reports false when the store epoch already moved past
// epoch: a transition that published before the socket registered could not
// cancel it, so the caller must not keep it.
func (r *Registry) bindCommandStream(epoch uint64, cancel context.CancelFunc) bool {
	r.cmdMu.Lock()
	r.cmdCancel = cancel
	r.cmdMu.Unlock()
	return r.currentScope().epoch == epoch
}

// cancelCommandStream closes the command socket. Account transitions call it
// after publishing the next account.
func (r *Registry) cancelCommandStream() {
	r.cmdMu.Lock()
	cancel := r.cmdCancel
	r.cmdMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// handleRemoteCommandMessage runs a command delivered on scope's socket and
// reports the result with scope's credentials. A command that arrives after
// the account changed is neither run nor reported; the server delivers
// pending commands again when that account's socket subscribes.
func (r *Registry) handleRemoteCommandMessage(scope storeScope, data json.RawMessage) error {
	var command struct {
		ID          string          `json:"id"`
		CommandType string          `json:"command_type"`
		Payload     json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &command); err != nil {
		return err
	}
	if command.ID == "" || command.CommandType == "" {
		return nil
	}
	out, ran, err := r.runRemoteCommand(scope, command.CommandType, command.Payload)
	if !ran {
		return nil
	}
	if err != nil {
		_ = r.completeRemoteCommand(scope, command.ID, "failed", nil, err.Error())
		return err
	}
	return r.completeRemoteCommand(scope, command.ID, "succeeded", out, "")
}

// runRemoteCommand holds mutationMu for reading so an account transition waits
// for the command and its outbox entry before it publishes. ran is false when
// scope's account is no longer the active, connected one.
func (r *Registry) runRemoteCommand(scope storeScope, method string, params json.RawMessage) (out any, ran bool, err error) {
	r.reloadAccountConfig()
	r.mutationMu.RLock()
	defer r.mutationMu.RUnlock()
	current := r.currentScope()
	if current.epoch != scope.epoch || !current.connected() || current.account.ID != scope.account.ID {
		return nil, false, nil
	}
	out, err = r.dispatchRemoteAt(scope.epoch, method, params)
	return out, true, err
}

func (r *Registry) completeRemoteCommand(scope storeScope, id, status string, result any, errText string) error {
	body, err := json.Marshal(map[string]any{"status": status, "result": result, "error": errText})
	if err != nil {
		return err
	}
	req, err := accountRequest(scope.cfg.ServerURL, http.MethodPatch, "/api/kiosk/commands/"+id, scope.account, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := accountHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return requireOK(resp)
}
