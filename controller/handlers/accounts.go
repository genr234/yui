package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/store"
)

const (
	accountStoreDir  = "accounts"
	accountStoreName = "yui-store.db"
)

var safeAccountPath = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type accountSyncState struct {
	LastSyncAt    string `json:"last_sync_at,omitempty"`
	LastSyncError string `json:"last_sync_error,omitempty"`
	Syncing       bool   `json:"syncing"`
}

type accountStatus struct {
	ServerURL       string        `json:"server_url,omitempty"`
	Connected       bool          `json:"connected"`
	NeedsPairing    bool          `json:"needs_pairing"`
	ActiveAccount   *accountView  `json:"active_account,omitempty"`
	Accounts        []accountView `json:"accounts"`
	Anonymous       bool          `json:"anonymous"`
	Syncing         bool          `json:"syncing"`
	LastSyncAt      string        `json:"last_sync_at,omitempty"`
	LastSyncError   string        `json:"last_sync_error,omitempty"`
	PendingCommands int           `json:"pending_commands,omitempty"`
}

type accountView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ProfileImageURL string `json:"profile_image_url,omitempty"`
	KioskID         string `json:"kiosk_id"`
	SyncCursor      int64  `json:"sync_cursor"`
}

func AccountCommands() []Command {
	return []Command{
		AccountStatusCommand{},
		AccountConnectCommand{},
		AccountSwitchCommand{},
		AccountDisconnectCommand{},
		AccountImportAnonymousCommand{},
		AccountSyncNowCommand{},
	}
}

type AccountStatusCommand struct{}

func (AccountStatusCommand) Name() string { return "accounts.status" }

func (AccountStatusCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	if err := r.refreshActiveAccount(); err != nil {
		r.setSyncState(false, err.Error())
	}
	return r.accountStatus(), nil
}

type AccountConnectCommand struct{}

func (AccountConnectCommand) Name() string { return "accounts.connect" }

func (AccountConnectCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var req struct {
		ServerURL string `json:"server_url"`
		Code      string `json:"code"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	serverURL, err := normalizeServerURL(req.ServerURL)
	if err != nil {
		return nil, err
	}

	cfg := r.cfg
	if cfg.DeviceUID == "" {
		cfg.DeviceUID = newDeviceUID()
	}

	pairing, err := pairWithServer(serverURL, cfg.DeviceUID, req.Code, req.Name)
	if err != nil {
		return nil, err
	}

	account := config.AccountConfig{
		ID:              pairing.Account.ID,
		Name:            pairing.Account.Name,
		ProfileImageURL: pairing.Account.ProfileImageURL,
		KioskID:         pairing.Kiosk.ID,
		DeviceToken:     pairing.DeviceToken,
		SyncCursor:      pairing.SyncCursor,
	}

	cfg.ServerURL = serverURL
	cfg.ActiveAccountID = account.ID
	cfg.Accounts = upsertAccount(cfg.Accounts, account)
	if err := config.Save(cfg); err != nil {
		return nil, err
	}
	if err := r.switchConfig(cfg); err != nil {
		return nil, err
	}
	if err := r.applyRemoteOperations(pairing.Operations); err != nil {
		return nil, err
	}
	return r.accountStatus(), nil
}

type AccountSwitchCommand struct{}

func (AccountSwitchCommand) Name() string { return "accounts.switch" }

func (AccountSwitchCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var req struct {
		AccountID string `json:"account_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	cfg := r.cfg
	if req.AccountID != "" {
		if _, ok := findAccount(cfg.Accounts, req.AccountID); !ok {
			return nil, fmt.Errorf("account %q is not cached on this kiosk", req.AccountID)
		}
	}
	cfg.ActiveAccountID = req.AccountID
	if err := config.Save(cfg); err != nil {
		return nil, err
	}
	if err := r.switchConfig(cfg); err != nil {
		return nil, err
	}
	return r.accountStatus(), nil
}

type AccountDisconnectCommand struct{}

func (AccountDisconnectCommand) Name() string { return "accounts.disconnect" }

func (AccountDisconnectCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	cfg := r.cfg
	cfg.ServerURL = ""
	cfg.ActiveAccountID = ""
	cfg.Accounts = nil
	if err := config.Save(cfg); err != nil {
		return nil, err
	}
	if err := r.switchConfig(cfg); err != nil {
		return nil, err
	}
	return r.accountStatus(), nil
}

type AccountImportAnonymousCommand struct{}

func (AccountImportAnonymousCommand) Name() string { return "accounts.importAnonymous" }

func (AccountImportAnonymousCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	if r.cfg.ActiveAccountID == "" {
		return nil, fmt.Errorf("connect or switch to an account before importing anonymous data")
	}
	anonymous, err := store.Open(r.cfg.StorePath)
	if err != nil {
		return nil, err
	}
	defer anonymous.Close()
	current, err := r.Store()
	if err != nil {
		return nil, err
	}

	imported := 0
	for _, collection := range syncCollections {
		docs, err := anonymous.Collection(collection).List(store.ListOptions{})
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			continue
		}
		for _, doc := range docs {
			if err := current.Collection(collection).Put(doc.ID, doc.Value); err != nil {
				return nil, err
			}
			imported++
		}
		if err := r.recordReplaceCollection(collection); err != nil {
			return nil, err
		}
	}
	go r.SyncNow()
	return map[string]any{"imported": imported}, nil
}

type AccountSyncNowCommand struct{}

func (AccountSyncNowCommand) Name() string { return "accounts.syncNow" }

func (AccountSyncNowCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	if err := r.SyncNow(); err != nil {
		return r.accountStatus(), err
	}
	return r.accountStatus(), nil
}

func (r *Registry) activeStorePath() string {
	if r.cfg.ActiveAccountID == "" {
		return r.cfg.StorePath
	}
	return filepath.Join(r.cfg.ConfigDir, accountStoreDir, accountPathID(r.cfg.ActiveAccountID), accountStoreName)
}

func (r *Registry) accountStatus() accountStatus {
	r.reloadAccountConfig()
	r.syncMu.Lock()
	syncState := r.syncState
	r.syncMu.Unlock()

	active, _ := findAccount(r.cfg.Accounts, r.cfg.ActiveAccountID)
	return accountStatus{
		ServerURL:     r.cfg.ServerURL,
		Connected:     r.cfg.ServerURL != "" && r.cfg.ActiveAccountID != "" && active != nil && active.DeviceToken != "",
		NeedsPairing:  r.cfg.ServerURL != "" && r.cfg.ActiveAccountID != "" && (active == nil || active.DeviceToken == ""),
		ActiveAccount: accountViewPtr(active),
		Accounts:      accountViews(r.cfg.Accounts),
		Anonymous:     r.cfg.ActiveAccountID == "",
		Syncing:       syncState.Syncing,
		LastSyncAt:    syncState.LastSyncAt,
		LastSyncError: syncState.LastSyncError,
	}
}

func (r *Registry) reloadAccountConfig() {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if cfg.ConfigPath != "" && r.cfg.ConfigPath != "" && cfg.ConfigPath != r.cfg.ConfigPath {
		return
	}
	r.cfg.ServerURL = cfg.ServerURL
	r.cfg.DeviceUID = cfg.DeviceUID
	r.cfg.ActiveAccountID = cfg.ActiveAccountID
	r.cfg.Accounts = cfg.Accounts
}

func accountViews(accounts []config.AccountConfig) []accountView {
	views := make([]accountView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, accountView{ID: account.ID, Name: account.Name, ProfileImageURL: account.ProfileImageURL, KioskID: account.KioskID, SyncCursor: account.SyncCursor})
	}
	return views
}

func accountViewPtr(account *config.AccountConfig) *accountView {
	if account == nil {
		return nil
	}
	view := accountView{ID: account.ID, Name: account.Name, ProfileImageURL: account.ProfileImageURL, KioskID: account.KioskID, SyncCursor: account.SyncCursor}
	return &view
}

func (r *Registry) switchConfig(cfg config.Config) error {
	if r.plugins != nil {
		r.plugins.Stop()
	}
	if err := r.resetStore(); err != nil {
		return err
	}
	r.cfg = cfg
	if r.plugins != nil && r.pluginCtx != nil {
		r.plugins.Start(r.pluginCtx)
	}
	return nil
}

func normalizeServerURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "", fmt.Errorf("server URL is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("server URL must include scheme and host")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("server URL must use http or https")
	}
	return value, nil
}

func newDeviceUID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("device-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data[:])
}

func accountPathID(id string) string {
	safe := safeAccountPath.ReplaceAllString(id, "_")
	if safe == "" {
		return "account"
	}
	return safe
}

func upsertAccount(accounts []config.AccountConfig, account config.AccountConfig) []config.AccountConfig {
	next := append([]config.AccountConfig(nil), accounts...)
	for i := range next {
		if next[i].ID == account.ID {
			next[i] = account
			return next
		}
	}
	return append(next, account)
}

func findAccount(accounts []config.AccountConfig, id string) (*config.AccountConfig, bool) {
	for i := range accounts {
		if accounts[i].ID == id {
			account := accounts[i]
			return &account, true
		}
	}
	return nil, false
}

type pairingResponse struct {
	Account struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		ProfileImageURL string `json:"profile_image_url"`
	} `json:"account"`
	Kiosk struct {
		ID string `json:"id"`
	} `json:"kiosk"`
	DeviceToken string            `json:"device_token"`
	SyncCursor  int64             `json:"sync_cursor"`
	HasMore     bool              `json:"has_more"`
	Operations  []remoteOperation `json:"operations"`
}

func pairWithServer(serverURL, deviceUID, code, name string) (pairingResponse, error) {
	body, err := json.Marshal(map[string]string{
		"device_uid": deviceUID,
		"code":       code,
		"name":       name,
	})
	if err != nil {
		return pairingResponse{}, err
	}
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/kiosk/pair", bytes.NewReader(body))
	if err != nil {
		return pairingResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := accountHTTPClient.Do(req)
	if err != nil {
		return pairingResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return pairingResponse{}, fmt.Errorf("pairing failed: %s", strings.TrimSpace(string(text)))
	}
	var pairing pairingResponse
	if err := json.NewDecoder(resp.Body).Decode(&pairing); err != nil {
		return pairingResponse{}, err
	}
	return pairing, nil
}

func (r *Registry) StartPlugins(ctx context.Context) {
	r.pluginCtx = ctx
	if r.plugins != nil {
		r.plugins.Start(ctx)
	}
}
