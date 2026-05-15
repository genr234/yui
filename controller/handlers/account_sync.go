package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"kiosk/controller/internal/config"
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
	go r.accountLoop(ctx)
	go r.accountCommandLoop(ctx)
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
		if err := r.streamRemoteCommands(ctx); err != nil && ctx.Err() == nil {
			log.Printf("account command websocket failed: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(accountReconnectDelay):
		}
	}
}

func (r *Registry) SyncNow() error {
	r.reloadAccountConfig()
	account, ok := r.activeAccount()
	if !ok || r.cfg.ServerURL == "" || account.DeviceToken == "" {
		return nil
	}
	r.setSyncState(true, "")
	defer r.setSyncing(false)

	pending, err := r.pendingOperations()
	if err != nil {
		r.setSyncState(false, err.Error())
		return err
	}
	if len(pending) > 0 {
		if err := r.pushOperations(account, pending); err != nil {
			if errors.Is(err, errAccountUnauthorized) {
				_ = r.markAccountNeedsPairing(account.ID, err.Error())
			}
			r.setSyncState(false, err.Error())
			return err
		}
	}
	if err := r.pullOperations(account); err != nil {
		if errors.Is(err, errAccountUnauthorized) {
			_ = r.markAccountNeedsPairing(account.ID, err.Error())
		}
		r.setSyncState(false, err.Error())
		return err
	}
	r.setSyncState(false, "")
	return nil
}

func (r *Registry) refreshActiveAccount() error {
	r.reloadAccountConfig()
	account, ok := r.activeAccount()
	if !ok || r.cfg.ServerURL == "" || account.DeviceToken == "" {
		return nil
	}
	if err := r.pullOperations(account); err != nil {
		if errors.Is(err, errAccountUnauthorized) {
			if markErr := r.markAccountNeedsPairing(account.ID, err.Error()); markErr != nil {
				return markErr
			}
		}
		return err
	}
	return nil
}

func (r *Registry) activeAccount() (config.AccountConfig, bool) {
	account, ok := findAccount(r.cfg.Accounts, r.cfg.ActiveAccountID)
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
	cfg := r.cfg
	changed := false
	for i := range cfg.Accounts {
		if cfg.Accounts[i].ID != accountID {
			continue
		}
		if cfg.Accounts[i].DeviceToken != "" || cfg.Accounts[i].KioskID != "" {
			cfg.Accounts[i].DeviceToken = ""
			cfg.Accounts[i].KioskID = ""
			changed = true
		}
		break
	}
	if !changed {
		return nil
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	r.cfg = cfg
	r.setSyncState(false, reason)
	return nil
}

func (r *Registry) recordMutation(method string, params json.RawMessage, result any) {
	if r.cfg.ActiveAccountID == "" || r.cfg.ServerURL == "" {
		return
	}
	if err := r.recordMutationErr(method, params, result); err != nil {
		log.Printf("sync operation record failed for %s: %v", method, err)
	}
}

func (r *Registry) recordMutationErr(method string, params json.RawMessage, result any) error {
	switch method {
	case "storage.set":
		var p struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordPut(defaultStorageCollection, p.Key, p.Value)
	case "storage.delete":
		var p struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(defaultStorageCollection, p.Key)
	case "store.put":
		var p valueParams
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordPutRaw(p.Collection, p.ID, p.Value)
	case "store.delete":
		var p documentParams
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(p.Collection, p.ID)
	case "apps.sources.add", "apps.sources.refresh":
		if record, ok := result.(appSourceRecord); ok {
			if err := r.recordPut(appSourcesCollection, record.ID, record); err != nil {
				return err
			}
		}
		return r.recordReplaceCollection(appCatalogCollection)
	case "apps.sources.remove":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		if err := r.recordDelete(appSourcesCollection, p.ID); err != nil {
			return err
		}
		return r.recordReplaceCollection(appCatalogCollection)
	case "apps.install", "apps.dev.install":
		if record, ok := result.(installedAppRecord); ok {
			return r.recordPut(installedAppsCollection, record.ID, record)
		}
	case "apps.uninstall":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(installedAppsCollection, p.ID)
	case "plugins.sources.add", "plugins.sources.refresh":
		if record, ok := result.(pluginSourceRecord); ok {
			if err := r.recordPut(pluginSourcesCollection, record.ID, record); err != nil {
				return err
			}
		}
		return r.recordReplaceCollection(pluginCatalogCollection)
	case "plugins.sources.remove":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		if err := r.recordDelete(pluginSourcesCollection, p.ID); err != nil {
			return err
		}
		return r.recordReplaceCollection(pluginCatalogCollection)
	case "plugins.install":
		if record, ok := result.(installedPluginRecord); ok {
			if err := r.recordPut(installedPluginsCollection, record.ID, record); err != nil {
				return err
			}
			return r.recordReplaceCollection(pluginStateCollection)
		}
	case "plugins.uninstall":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		return r.recordDelete(installedPluginsCollection, p.ID)
	case "plugins.enable", "plugins.disable", "plugins.permissions.update", "plugins.administrator.update":
		return r.recordReplaceCollection(pluginStateCollection)
	case "plugins.settings.update":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
		db, err := r.Store()
		if err != nil {
			return err
		}
		doc, ok, err := db.Collection(pluginSettingsCollection).Get(p.ID)
		if err != nil || !ok {
			return err
		}
		return r.recordPutRaw(pluginSettingsCollection, p.ID, doc.Value)
	}
	return nil
}

func (r *Registry) recordPut(collection, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.recordPutRaw(collection, id, data)
}

func (r *Registry) recordPutRaw(collection, id string, value json.RawMessage) error {
	return r.appendOperation(collection, id, "put", value)
}

func (r *Registry) recordDelete(collection, id string) error {
	return r.appendOperation(collection, id, "delete", nil)
}

func (r *Registry) recordReplaceCollection(collection string) error {
	db, err := r.Store()
	if err != nil {
		return err
	}
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
	return r.appendOperation(collection, "", "replace_collection", data)
}

func (r *Registry) appendOperation(collection, id, action string, payload json.RawMessage) error {
	if r.cfg.DeviceUID == "" {
		cfg := r.cfg
		cfg.DeviceUID = newDeviceUID()
		if err := config.Save(cfg); err != nil {
			return err
		}
		r.cfg = cfg
	}
	db, err := r.Store()
	if err != nil {
		return err
	}
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
	meta := syncMeta{ClientID: r.cfg.DeviceUID, NextSeq: 1}
	_, err := db.Collection(syncMetaCollection).Decode(syncMetaStateID, &meta)
	if err != nil {
		return meta, err
	}
	if meta.ClientID == "" {
		meta.ClientID = r.cfg.DeviceUID
	}
	if meta.ClientID == "" {
		meta.ClientID = newDeviceUID()
	}
	if meta.NextSeq <= 0 {
		meta.NextSeq = 1
	}
	return meta, nil
}

func (r *Registry) pendingOperations() ([]syncOperation, error) {
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	docs, err := db.Collection(syncOperationsCollection).List(store.ListOptions{})
	if err != nil {
		return nil, err
	}
	ops := make([]syncOperation, 0, len(docs))
	for _, doc := range docs {
		var op syncOperation
		if err := json.Unmarshal(doc.Value, &op); err != nil {
			return nil, err
		}
		if !op.Synced {
			ops = append(ops, op)
		}
	}
	return ops, nil
}

func (r *Registry) pushOperations(account config.AccountConfig, ops []syncOperation) error {
	for start := 0; start < len(ops); start += accountSyncBatchSize {
		end := start + accountSyncBatchSize
		if end > len(ops) {
			end = len(ops)
		}
		batch := ops[start:end]
		if err := r.pushOperationBatch(account, batch); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) pushOperationBatch(account config.AccountConfig, ops []syncOperation) error {
	body, err := json.Marshal(map[string]any{"operations": ops})
	if err != nil {
		return err
	}
	req, err := r.accountRequest(http.MethodPost, "/api/kiosk/sync/push", account, bytes.NewReader(body))
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
	db, err := r.Store()
	if err != nil {
		return err
	}
	for _, op := range ops {
		op.Synced = true
		if err := db.Collection(syncOperationsCollection).Put(op.ID, op); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) pullOperations(account config.AccountConfig) error {
	for {
		next, hasMore, err := r.pullOperationPage(account)
		if err != nil {
			return err
		}
		account = next
		if !hasMore {
			return nil
		}
	}
}

func (r *Registry) pullOperationPage(account config.AccountConfig) (config.AccountConfig, bool, error) {
	path := fmt.Sprintf("/api/kiosk/sync/pull?cursor=%d", account.SyncCursor)
	req, err := r.accountRequest(http.MethodGet, path, account, nil)
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
	if err := r.applyRemoteOperations(result.Operations); err != nil {
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

func (r *Registry) applyRemoteOperations(ops []remoteOperation) error {
	db, err := r.Store()
	if err != nil {
		return err
	}
	meta, err := r.syncMeta(db)
	if err != nil {
		return err
	}
	touchedPlugins := false
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

func (r *Registry) saveAccount(account config.AccountConfig) error {
	cfg := r.cfg
	cfg.Accounts = upsertAccount(cfg.Accounts, account)
	if err := config.Save(cfg); err != nil {
		return err
	}
	r.cfg = cfg
	return nil
}

func (r *Registry) accountRequest(method, path string, account config.AccountConfig, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, strings.TrimRight(r.cfg.ServerURL, "/")+path, body)
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
		"plugins.permissions.update", "plugins.settings.update":
		return true
	default:
		return false
	}
}

func (r *Registry) streamRemoteCommands(ctx context.Context) error {
	r.reloadAccountConfig()
	account, ok := r.activeAccount()
	if !ok || r.cfg.ServerURL == "" || account.DeviceToken == "" {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(accountReconnectDelay):
			return nil
		}
	}

	wsURL, err := accountCableURL(r.cfg.ServerURL)
	if err != nil {
		return err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+account.DeviceToken)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
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
		case <-ctx.Done():
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
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
		if err := r.handleRemoteCommandMessage(message.Message); err != nil {
			log.Printf("account command execution failed: %v", err)
		}
	}
}

func accountCableURL(serverURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(serverURL, "/") + "/cable")
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported server URL scheme %q", parsed.Scheme)
	}
	return parsed.String(), nil
}

func (r *Registry) handleRemoteCommandMessage(data json.RawMessage) error {
	r.reloadAccountConfig()
	account, ok := r.activeAccount()
	if !ok || r.cfg.ServerURL == "" || account.DeviceToken == "" {
		return nil
	}
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
	out, err := r.DispatchRemote(command.CommandType, command.Payload)
	if err != nil {
		_ = r.completeRemoteCommand(account, command.ID, "failed", nil, err.Error())
		return err
	}
	return r.completeRemoteCommand(account, command.ID, "succeeded", out, "")
}

func (r *Registry) completeRemoteCommand(account config.AccountConfig, id, status string, result any, errText string) error {
	body, err := json.Marshal(map[string]any{"status": status, "result": result, "error": errText})
	if err != nil {
		return err
	}
	req, err := r.accountRequest(http.MethodPatch, "/api/kiosk/commands/"+id, account, bytes.NewReader(body))
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
