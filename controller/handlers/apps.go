package handlers

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"kiosk/controller/internal/store"
)

const (
	appSourcesCollection    = "app-sources"
	appCatalogCollection    = "app-catalog"
	installedAppsCollection = "installed-apps"
	maxAppFetchBytes        = 2 * 1024 * 1024
)

var appMetadataFields = map[string]*regexp.Regexp{
	"schema":      regexp.MustCompile(`schema\s*:\s*["']([^"']+)["']`),
	"id":          regexp.MustCompile(`id\s*:\s*["']([^"']+)["']`),
	"name":        regexp.MustCompile(`name\s*:\s*["']([^"']+)["']`),
	"version":     regexp.MustCompile(`version\s*:\s*["']([^"']+)["']`),
	"description": regexp.MustCompile(`description\s*:\s*["']([^"']+)["']`),
	"author":      regexp.MustCompile(`author\s*:\s*["']([^"']+)["']`),
	"homepage":    regexp.MustCompile(`homepage\s*:\s*["']([^"']+)["']`),
	"license":     regexp.MustCompile(`license\s*:\s*["']([^"']+)["']`),
	"icon":        regexp.MustCompile(`icon\s*:\s*["']([^"']+)["']`),
	"category":    regexp.MustCompile(`category\s*:\s*["']([^"']+)["']`),
}

var appPermissionsField = regexp.MustCompile(`permissions\s*:\s*\[([\s\S]*?)\]`)
var appPermissionItem = regexp.MustCompile(`["']([^"']+)["']`)
var appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,}[a-z0-9_-]$`)
var appHTTPClient = &http.Client{Timeout: 15 * time.Second}

func AppsCommands() []Command {
	return []Command{
		AppSourcesListCommand{},
		AppSourcesAddCommand{},
		AppSourcesRemoveCommand{},
		AppSourcesRefreshCommand{},
		AppCatalogListCommand{},
		AppInstallCommand{},
		AppUninstallCommand{},
		AppInstalledListCommand{},
	}
}

type catalogDocument struct {
	Schema      string             `json:"schema"`
	Name        string             `json:"name"`
	Publisher   string             `json:"publisher"`
	SigningKeys []string           `json:"signingKeys"`
	Apps        []catalogAppRecord `json:"apps"`
}

type catalogAppRecord struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description,omitempty"`
	Icon         string   `json:"icon,omitempty"`
	Category     string   `json:"category,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
	SourceURL    string   `json:"sourceUrl"`
	SourceSHA256 string   `json:"sourceSha256"`
	Signature    string   `json:"signature"`
}

type appSourceRecord struct {
	ID             string   `json:"id"`
	URL            string   `json:"url"`
	Name           string   `json:"name"`
	Publisher      string   `json:"publisher"`
	SigningKeys    []string `json:"signingKeys"`
	LastRefreshed  string   `json:"lastRefreshed,omitempty"`
	LastStatus     string   `json:"lastStatus"`
	LastError      string   `json:"lastError,omitempty"`
	DiscoveredApps int      `json:"discoveredApps"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

type catalogEntryRecord struct {
	SourceID  string           `json:"sourceId"`
	SourceURL string           `json:"sourceUrl"`
	Catalog   string           `json:"catalog"`
	Publisher string           `json:"publisher"`
	App       catalogAppRecord `json:"app"`
	Verified  bool             `json:"verified"`
	UpdatedAt string           `json:"updatedAt"`
}

type installedAppRecord struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Type         string            `json:"type"`
	Entry        string            `json:"entry"`
	SourceID     string            `json:"sourceId"`
	SourceURL    string            `json:"sourceUrl"`
	InstalledAt  string            `json:"installedAt"`
	App          appMetadataRecord `json:"app"`
	Source       string            `json:"source"`
	Signature    string            `json:"signature"`
	SourceSHA256 string            `json:"sourceSha256"`
}

type appMetadataRecord struct {
	Schema      string   `json:"schema"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	License     string   `json:"license,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type AppSourcesListCommand struct{}

func (AppSourcesListCommand) Name() string { return "apps.sources.list" }
func (AppSourcesListCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return listCollection[appSourceRecord](r, appSourcesCollection)
}

type AppSourcesAddCommand struct{}

func (AppSourcesAddCommand) Name() string { return "apps.sources.add" }
func (AppSourcesAddCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if err := requireHTTPS(p.URL); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record := appSourceRecord{ID: sourceID(p.URL), URL: p.URL, LastStatus: "pending", CreatedAt: now, UpdatedAt: now}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	if err := db.Collection(appSourcesCollection).Put(record.ID, record); err != nil {
		return nil, err
	}
	return record, nil
}

type AppSourcesRemoveCommand struct{}

func (AppSourcesRemoveCommand) Name() string { return "apps.sources.remove" }
func (AppSourcesRemoveCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	entries, err := db.Collection(appCatalogCollection).List(store.ListOptions{Prefix: p.ID + ":"})
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := db.Collection(appCatalogCollection).Delete(entry.ID); err != nil {
			return nil, err
		}
	}
	return nil, db.Collection(appSourcesCollection).Delete(p.ID)
}

type AppSourcesRefreshCommand struct{}

func (AppSourcesRefreshCommand) Name() string { return "apps.sources.refresh" }
func (AppSourcesRefreshCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return refreshSource(r, p.ID)
}

type AppCatalogListCommand struct{}

func (AppCatalogListCommand) Name() string { return "apps.catalog.list" }
func (AppCatalogListCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return listCollection[catalogEntryRecord](r, appCatalogCollection)
}

type AppInstalledListCommand struct{}

func (AppInstalledListCommand) Name() string { return "apps.installed.list" }
func (AppInstalledListCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return listCollection[installedAppRecord](r, installedAppsCollection)
}

type AppUninstallCommand struct{}

func (AppUninstallCommand) Name() string { return "apps.uninstall" }
func (AppUninstallCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	return nil, db.Collection(installedAppsCollection).Delete(p.ID)
}

type AppInstallCommand struct{}

func (AppInstallCommand) Name() string { return "apps.install" }
func (AppInstallCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		CatalogID string `json:"catalogId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	var entry catalogEntryRecord
	ok, err := db.Collection(appCatalogCollection).Decode(p.CatalogID, &entry)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("catalog app not found")
	}
	var sourceRecord appSourceRecord
	ok, err = db.Collection(appSourcesCollection).Decode(entry.SourceID, &sourceRecord)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("app source trust record not found")
	}
	source, err := fetchText(entry.App.SourceURL, maxAppFetchBytes)
	if err != nil {
		return nil, err
	}
	if err := verifyCatalogApp(entry, sourceRecord.SigningKeys, []byte(source)); err != nil {
		return nil, err
	}
	meta, err := metadataFromAppSource(source)
	if err != nil {
		return nil, err
	}
	if err := metadataMatchesCatalog(meta, entry.App); err != nil {
		return nil, err
	}
	installed := installedAppRecord{
		ID: meta.ID, Name: meta.Name, Version: meta.Version, Type: "simple-js", Entry: entry.App.SourceURL,
		SourceID: entry.SourceID, SourceURL: entry.App.SourceURL, InstalledAt: time.Now().UTC().Format(time.RFC3339),
		App: meta, Source: source, Signature: entry.App.Signature, SourceSHA256: strings.ToLower(entry.App.SourceSHA256),
	}
	if err := db.Collection(installedAppsCollection).Put(installed.ID, installed); err != nil {
		return nil, err
	}
	return installed, nil
}

func refreshSource(r *Registry, id string) (appSourceRecord, error) {
	db, err := r.Store()
	if err != nil {
		return appSourceRecord{}, err
	}
	var source appSourceRecord
	ok, err := db.Collection(appSourcesCollection).Decode(id, &source)
	if err != nil {
		return appSourceRecord{}, err
	}
	if !ok {
		return appSourceRecord{}, fmt.Errorf("app source not found")
	}
	catalog, err := fetchCatalog(source.URL)
	now := time.Now().UTC().Format(time.RFC3339)
	source.UpdatedAt = now
	source.LastRefreshed = now
	if err != nil {
		source.LastStatus = "error"
		source.LastError = err.Error()
		_ = db.Collection(appSourcesCollection).Put(source.ID, source)
		return source, err
	}
	source.Name = catalog.Name
	source.Publisher = catalog.Publisher
	source.SigningKeys = catalog.SigningKeys
	source.LastStatus = "ok"
	source.LastError = ""
	source.DiscoveredApps = len(catalog.Apps)
	oldEntries, err := db.Collection(appCatalogCollection).List(store.ListOptions{Prefix: source.ID + ":"})
	if err != nil {
		return source, err
	}
	for _, old := range oldEntries {
		if err := db.Collection(appCatalogCollection).Delete(old.ID); err != nil {
			return source, err
		}
	}
	for _, app := range catalog.Apps {
		if err := validateCatalogApp(app); err != nil {
			return source, err
		}
		entry := catalogEntryRecord{
			SourceID: source.ID, SourceURL: source.URL, Catalog: catalog.Name, Publisher: catalog.Publisher,
			App: app, Verified: true, UpdatedAt: now,
		}
		if err := db.Collection(appCatalogCollection).Put(catalogEntryID(source.ID, app.ID, app.Version), entry); err != nil {
			return source, err
		}
	}
	if err := db.Collection(appSourcesCollection).Put(source.ID, source); err != nil {
		return source, err
	}
	return source, nil
}

func fetchCatalog(rawURL string) (catalogDocument, error) {
	var catalog catalogDocument
	body, err := fetchText(rawURL, maxAppFetchBytes)
	if err != nil {
		return catalog, err
	}
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		return catalog, err
	}
	if catalog.Schema != "yui.catalog.v0" {
		return catalog, fmt.Errorf("unsupported catalog schema")
	}
	if catalog.Name == "" || catalog.Publisher == "" {
		return catalog, fmt.Errorf("catalog name and publisher are required")
	}
	if len(catalog.SigningKeys) == 0 {
		return catalog, fmt.Errorf("catalog signingKeys are required")
	}
	for _, key := range catalog.SigningKeys {
		if _, err := decodeEd25519PublicKey(key); err != nil {
			return catalog, err
		}
	}
	return catalog, nil
}

func fetchText(rawURL string, limit int64) (string, error) {
	if err := requireHTTPS(rawURL); err != nil {
		return "", err
	}
	resp, err := appHTTPClient.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > limit {
		return "", fmt.Errorf("download exceeded %d bytes", limit)
	}
	return string(body), nil
}

func requireHTTPS(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("app sources require https urls")
	}
	if parsed.Host == "" {
		return fmt.Errorf("app source url requires a host")
	}
	return nil
}

func sourceID(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])[:24]
}

func catalogEntryID(sourceID, appID, version string) string {
	return sourceID + ":" + appID + ":" + version
}

func verifyCatalogApp(entry catalogEntryRecord, signingKeys []string, source []byte) error {
	hash := sha256.Sum256(source)
	actual := hex.EncodeToString(hash[:])
	if !strings.EqualFold(actual, entry.App.SourceSHA256) {
		return fmt.Errorf("app source hash mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(entry.App.Signature)
	if err != nil {
		return fmt.Errorf("decode app signature: %w", err)
	}
	for _, keyValue := range signingKeys {
		key, err := decodeEd25519PublicKey(keyValue)
		if err != nil {
			return err
		}
		if ed25519.Verify(key, source, signature) {
			return nil
		}
	}
	return fmt.Errorf("app signature could not be verified")
}

func decodeEd25519PublicKey(value string) (ed25519.PublicKey, error) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode signing key: %w", err)
	}
	if len(data) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key size")
	}
	return ed25519.PublicKey(data), nil
}

func validateCatalogApp(app catalogAppRecord) error {
	if !appIDPattern.MatchString(app.ID) {
		return fmt.Errorf("invalid app id: %s", app.ID)
	}
	if app.Name == "" || app.Version == "" {
		return fmt.Errorf("catalog app name and version are required")
	}
	if err := requireHTTPS(app.SourceURL); err != nil {
		return err
	}
	if len(app.SourceSHA256) != 64 {
		return fmt.Errorf("sourceSha256 must be a hex sha256")
	}
	if _, err := hex.DecodeString(app.SourceSHA256); err != nil {
		return fmt.Errorf("sourceSha256 must be a hex sha256")
	}
	if app.Signature == "" {
		return fmt.Errorf("app signature is required")
	}
	return nil
}

func metadataFromAppSource(source string) (appMetadataRecord, error) {
	meta := appMetadataRecord{}
	for field, pattern := range appMetadataFields {
		match := pattern.FindStringSubmatch(source)
		if len(match) < 2 {
			continue
		}
		switch field {
		case "schema":
			meta.Schema = match[1]
		case "id":
			meta.ID = match[1]
		case "name":
			meta.Name = match[1]
		case "version":
			meta.Version = match[1]
		case "description":
			meta.Description = match[1]
		case "author":
			meta.Author = match[1]
		case "homepage":
			meta.Homepage = match[1]
		case "license":
			meta.License = match[1]
		case "icon":
			meta.Icon = match[1]
		case "category":
			meta.Category = match[1]
		}
	}
	if match := appPermissionsField.FindStringSubmatch(source); len(match) > 1 {
		for _, item := range appPermissionItem.FindAllStringSubmatch(match[1], -1) {
			meta.Permissions = append(meta.Permissions, item[1])
		}
	}
	if meta.Schema != "yui.simple-js.v0" {
		return meta, fmt.Errorf("unsupported app schema")
	}
	if !appIDPattern.MatchString(meta.ID) {
		return meta, fmt.Errorf("invalid app id")
	}
	if meta.Name == "" || meta.Version == "" {
		return meta, fmt.Errorf("app name and version are required")
	}
	return meta, nil
}

func metadataMatchesCatalog(meta appMetadataRecord, app catalogAppRecord) error {
	if meta.ID != app.ID || meta.Name != app.Name || meta.Version != app.Version {
		return fmt.Errorf("app metadata does not match catalog")
	}
	if !sameStringSet(meta.Permissions, app.Permissions) {
		return fmt.Errorf("app permissions do not match catalog")
	}
	return nil
}

func sameStringSet(a, b []string) bool {
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func listCollection[T any](r *Registry, name string) ([]T, error) {
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	docs, err := db.Collection(name).List(store.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]T, 0, len(docs))
	for _, doc := range docs {
		var value T
		if err := json.Unmarshal(doc.Value, &value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}
