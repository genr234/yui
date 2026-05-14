package handlers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/store"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	pluginSourcesCollection    = "plugin-sources"
	pluginCatalogCollection    = "plugin-catalog"
	installedPluginsCollection = "installed-plugins"
	pluginStateCollection      = "plugin-state"
	pluginSettingsCollection   = "plugin-settings"
	pluginSecretsCollection    = "plugin-secrets"
	pluginStorageCollection    = "plugin-storage"
	pluginAuditCollection      = "plugin-audit"
	maxPluginFetchBytes        = 2 * 1024 * 1024
	defaultPluginTimeout       = 30 * time.Second
	defaultPluginMaxSteps      = 500000
	maxProcessOutputBytes      = 20000
)

func PluginCommands() []Command {
	return []Command{
		PluginSourcesListCommand{},
		PluginSourcesAddCommand{},
		PluginSourcesRemoveCommand{},
		PluginSourcesRefreshCommand{},
		PluginCatalogListCommand{},
		PluginInstalledListCommand{},
		PluginInstallCommand{},
		PluginUninstallCommand{},
		PluginEnableCommand{},
		PluginDisableCommand{},
		PluginPermissionsUpdateCommand{},
		PluginAdministratorUpdateCommand{},
		PluginSettingsGetCommand{},
		PluginSettingsUpdateCommand{},
		PluginLogsListCommand{},
		PluginExtensionsListCommand{},
		PluginRunCommand{},
	}
}

type PluginManager struct {
	r          *Registry
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	instances  map[string]*pluginInstance
	localCache localPluginCache
}

type localPluginCache struct {
	signature string
	plugins   []pluginViewRecord
}

type pluginInstance struct {
	record     pluginViewRecord
	globals    starlark.StringDict
	commands   map[string]pluginCommand
	extensions pluginExtensions
	events     map[string][]starlark.Callable
	cancel     context.CancelFunc
	timers     []context.CancelFunc
	lastError  string
}

type pluginCommand struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Function starlark.Callable
}

type pluginSourceRecord struct {
	ID              string   `json:"id"`
	URL             string   `json:"url"`
	Name            string   `json:"name"`
	Publisher       string   `json:"publisher"`
	SigningKeys     []string `json:"signingKeys"`
	LastRefreshed   string   `json:"lastRefreshed,omitempty"`
	LastStatus      string   `json:"lastStatus"`
	LastError       string   `json:"lastError,omitempty"`
	DiscoveredItems int      `json:"discoveredPlugins"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
}

type pluginCatalogDocument struct {
	Schema      string                `json:"schema"`
	Name        string                `json:"name"`
	Publisher   string                `json:"publisher"`
	SigningKeys []string              `json:"signingKeys"`
	Plugins     []pluginCatalogRecord `json:"plugins"`
}

type pluginCatalogRecord struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description,omitempty"`
	Author       string   `json:"author,omitempty"`
	Homepage     string   `json:"homepage,omitempty"`
	License      string   `json:"license,omitempty"`
	Icon         string   `json:"icon,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
	SourceURL    string   `json:"sourceUrl"`
	SourceSHA256 string   `json:"sourceSha256"`
	Signature    string   `json:"signature"`
}

type pluginCatalogEntryRecord struct {
	SourceID  string              `json:"sourceId"`
	SourceURL string              `json:"sourceUrl"`
	Catalog   string              `json:"catalog"`
	Publisher string              `json:"publisher"`
	Plugin    pluginCatalogRecord `json:"plugin"`
	Verified  bool                `json:"verified"`
	UpdatedAt string              `json:"updatedAt"`
}

type pluginMetadataRecord struct {
	Schema      string                         `json:"schema"`
	ID          string                         `json:"id"`
	Name        string                         `json:"name"`
	Version     string                         `json:"version"`
	Description string                         `json:"description,omitempty"`
	Author      string                         `json:"author,omitempty"`
	Homepage    string                         `json:"homepage,omitempty"`
	License     string                         `json:"license,omitempty"`
	Icon        string                         `json:"icon,omitempty"`
	Permissions []string                       `json:"permissions,omitempty"`
	Settings    map[string]pluginSettingSchema `json:"settings,omitempty"`
	Schedules   []pluginScheduleRecord         `json:"schedules,omitempty"`
}

type pluginSettingSchema struct {
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Required    bool     `json:"required,omitempty"`
}

type pluginScheduleRecord struct {
	ID      string `json:"id"`
	Every   string `json:"every"`
	Handler string `json:"handler"`
}

type installedPluginRecord struct {
	ID                   string               `json:"id"`
	Name                 string               `json:"name"`
	Version              string               `json:"version"`
	Type                 string               `json:"type"`
	Entry                string               `json:"entry"`
	SourceID             string               `json:"sourceId"`
	SourceURL            string               `json:"sourceUrl"`
	InstalledAt          string               `json:"installedAt"`
	Plugin               pluginMetadataRecord `json:"plugin"`
	Source               string               `json:"source"`
	Signature            string               `json:"signature"`
	SourceSHA256         string               `json:"sourceSha256"`
	Enabled              bool                 `json:"enabled"`
	GrantedPermissions   []string             `json:"grantedPermissions"`
	AdministratorTrusted bool                 `json:"administratorTrusted"`
}

type pluginStateRecord struct {
	ID                   string   `json:"id"`
	Enabled              bool     `json:"enabled"`
	GrantedPermissions   []string `json:"grantedPermissions"`
	AdministratorTrusted bool     `json:"administratorTrusted"`
	UpdatedAt            string   `json:"updatedAt"`
}

type pluginViewRecord struct {
	ID                   string               `json:"id"`
	Name                 string               `json:"name"`
	Version              string               `json:"version"`
	Type                 string               `json:"type"`
	Entry                string               `json:"entry"`
	Dev                  bool                 `json:"dev"`
	Installed            bool                 `json:"installed"`
	SourceID             string               `json:"sourceId,omitempty"`
	SourceURL            string               `json:"sourceUrl,omitempty"`
	InstalledAt          string               `json:"installedAt,omitempty"`
	Plugin               pluginMetadataRecord `json:"plugin"`
	Source               string               `json:"source,omitempty"`
	Enabled              bool                 `json:"enabled"`
	GrantedPermissions   []string             `json:"grantedPermissions"`
	AdministratorTrusted bool                 `json:"administratorTrusted"`
	Commands             []pluginCommandView  `json:"commands,omitempty"`
	LastError            string               `json:"lastError,omitempty"`
	Settings             map[string]any       `json:"settings,omitempty"`
}

type pluginCommandView struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
}

type pluginShellPage struct {
	ID       string `json:"id"`
	PluginID string `json:"pluginId"`
	Title    string `json:"title"`
	Icon     string `json:"icon,omitempty"`
	Order    int    `json:"order,omitempty"`
	Blocks   []any  `json:"blocks,omitempty"`
	CSS      string `json:"css,omitempty"`
}

type pluginShellAction struct {
	ID       string `json:"id"`
	PluginID string `json:"pluginId"`
	Location string `json:"location"`
	Title    string `json:"title"`
	Icon     string `json:"icon,omitempty"`
	Command  string `json:"command,omitempty"`
}

type pluginExtensions struct {
	Pages   []pluginShellPage   `json:"pages"`
	Actions []pluginShellAction `json:"actions"`
	CSS     []string            `json:"css"`
}

type pluginAuditRecord struct {
	ID         string `json:"id"`
	PluginID   string `json:"pluginId"`
	Action     string `json:"action"`
	Permission string `json:"permission,omitempty"`
	At         string `json:"at"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type localPluginManifest struct {
	Schema string `json:"schema"`
	Type   string `json:"type"`
	Entry  string `json:"entry"`
	Dev    bool   `json:"dev"`
}

func NewPluginManager(r *Registry) *PluginManager {
	return &PluginManager{r: r, instances: make(map[string]*pluginInstance)}
}

func (m *PluginManager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	plugins, err := m.listPlugins()
	if err != nil {
		log.Printf("plugin discovery failed: %v", err)
		return
	}
	for _, plugin := range plugins {
		if plugin.Enabled {
			if err := m.enable(plugin.ID); err != nil {
				log.Printf("plugin %s enable failed: %v", plugin.ID, err)
			}
		}
	}
}

func (m *PluginManager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.ctx = nil
	ids := make([]string, 0, len(m.instances))
	for id := range m.instances {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, id := range ids {
		m.stopInstance(id, false)
	}
}

func (m *PluginManager) listPlugins() ([]pluginViewRecord, error) {
	installed, err := m.listInstalledPlugins()
	if err != nil {
		return nil, err
	}
	local, err := m.discoverLocalPlugins()
	if err != nil {
		return nil, err
	}
	byID := map[string]pluginViewRecord{}
	for _, plugin := range local {
		byID[plugin.ID] = plugin
	}
	for _, plugin := range installed {
		byID[plugin.ID] = plugin
	}
	result := make([]pluginViewRecord, 0, len(byID))
	for _, plugin := range byID {
		m.mu.Lock()
		if inst := m.instances[plugin.ID]; inst != nil {
			plugin.Commands = inst.commandViews()
			plugin.LastError = inst.lastError
		}
		m.mu.Unlock()
		result = append(result, plugin)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (m *PluginManager) listInstalledPlugins() ([]pluginViewRecord, error) {
	db, err := m.r.Store()
	if err != nil {
		return nil, err
	}
	docs, err := db.Collection(installedPluginsCollection).List(store.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]pluginViewRecord, 0, len(docs))
	for _, doc := range docs {
		var record installedPluginRecord
		if err := json.Unmarshal(doc.Value, &record); err != nil {
			return nil, err
		}
		state := m.stateFor(record.ID, record.Plugin.Permissions)
		result = append(result, pluginViewRecord{
			ID: record.ID, Name: record.Name, Version: record.Version, Type: record.Type, Entry: record.Entry,
			Installed: true, SourceID: record.SourceID, SourceURL: record.SourceURL, InstalledAt: record.InstalledAt,
			Plugin: record.Plugin, Source: record.Source, Enabled: state.Enabled, GrantedPermissions: state.GrantedPermissions,
			AdministratorTrusted: state.AdministratorTrusted,
		})
	}
	return result, nil
}

func (m *PluginManager) discoverLocalPlugins() ([]pluginViewRecord, error) {
	signature, err := m.localPluginSignature()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if signature == m.localCache.signature {
		plugins := clonePluginViews(m.localCache.plugins)
		m.mu.Unlock()
		return m.withCurrentPluginState(plugins)
	}
	m.mu.Unlock()

	var result []pluginViewRecord
	seen := map[string]bool{}
	for _, root := range m.localPluginRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(root, entry.Name())
			manifestPath := filepath.Join(dir, "yui.plugin.json")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			var manifest localPluginManifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
			}
			if manifest.Schema != "yui.local-plugin.v0" || manifest.Type != "starlark" {
				return nil, fmt.Errorf("unsupported plugin manifest: %s", manifestPath)
			}
			entryPath := filepath.Join(dir, strings.TrimPrefix(manifest.Entry, "./"))
			source, err := os.ReadFile(entryPath)
			if err != nil {
				return nil, err
			}
			meta, _, err := metadataFromPluginSource(entryPath, string(source))
			if err != nil {
				return nil, err
			}
			if seen[meta.ID] {
				continue
			}
			seen[meta.ID] = true
			result = append(result, pluginViewRecord{
				ID: meta.ID, Name: meta.Name, Version: meta.Version, Type: "starlark", Entry: entryPath,
				Dev: true, Plugin: meta, Source: string(source),
			})
		}
	}
	m.mu.Lock()
	m.localCache = localPluginCache{signature: signature, plugins: clonePluginViews(result)}
	m.mu.Unlock()
	return m.withCurrentPluginState(result)
}

func (m *PluginManager) withCurrentPluginState(plugins []pluginViewRecord) ([]pluginViewRecord, error) {
	for i := range plugins {
		state := m.stateFor(plugins[i].ID, plugins[i].Plugin.Permissions)
		plugins[i].Enabled = state.Enabled
		plugins[i].GrantedPermissions = state.GrantedPermissions
		plugins[i].AdministratorTrusted = state.AdministratorTrusted
	}
	return plugins, nil
}

func clonePluginViews(plugins []pluginViewRecord) []pluginViewRecord {
	cloned := make([]pluginViewRecord, len(plugins))
	copy(cloned, plugins)
	return cloned
}

func (m *PluginManager) localPluginSignature() (string, error) {
	var parts []string
	for _, root := range m.localPluginRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(root, entry.Name())
			manifestPath := filepath.Join(dir, "yui.plugin.json")
			manifestInfo, err := os.Stat(manifestPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", err
			}
			parts = append(parts, fileSignature(manifestPath, manifestInfo))

			data, err := os.ReadFile(manifestPath)
			if err != nil {
				return "", err
			}
			var manifest localPluginManifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return "", fmt.Errorf("parse %s: %w", manifestPath, err)
			}
			if manifest.Entry == "" {
				continue
			}
			entryPath := filepath.Join(dir, strings.TrimPrefix(manifest.Entry, "./"))
			entryInfo, err := os.Stat(entryPath)
			if err != nil {
				if os.IsNotExist(err) {
					parts = append(parts, entryPath+":missing")
					continue
				}
				return "", err
			}
			parts = append(parts, fileSignature(entryPath, entryInfo))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|"), nil
}

func fileSignature(path string, info os.FileInfo) string {
	return fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano())
}

func (m *PluginManager) localPluginRoots() []string {
	cwd, _ := os.Getwd()
	roots := []string{
		filepath.Join(cwd, "plugins"),
		filepath.Join(cwd, "..", "plugins"),
		filepath.Join(m.r.cfg.ConfigDir, "plugins"),
	}
	uniqueRoots := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		uniqueRoots = append(uniqueRoots, abs)
	}
	return uniqueRoots
}

func (m *PluginManager) stateFor(id string, declared []string) pluginStateRecord {
	db, err := m.r.Store()
	if err != nil {
		return pluginStateRecord{ID: id, GrantedPermissions: declared}
	}
	var state pluginStateRecord
	ok, err := db.Collection(pluginStateCollection).Decode(id, &state)
	if err != nil || !ok {
		return pluginStateRecord{ID: id, Enabled: false, GrantedPermissions: append([]string(nil), declared...)}
	}
	state.GrantedPermissions = filterDeclared(state.GrantedPermissions, declared)
	if len(state.GrantedPermissions) == 0 && len(declared) > 0 {
		state.GrantedPermissions = append([]string(nil), declared...)
	}
	return state
}

func (m *PluginManager) setState(id string, enabled bool, permissions []string, administratorTrusted bool) error {
	db, err := m.r.Store()
	if err != nil {
		return err
	}
	return db.Collection(pluginStateCollection).Put(id, pluginStateRecord{
		ID: id, Enabled: enabled, GrantedPermissions: permissions, AdministratorTrusted: administratorTrusted,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (m *PluginManager) enable(id string) error {
	plugin, err := m.findPlugin(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.instances[id] != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if plugin.Source == "" {
		return fmt.Errorf("plugin source is empty")
	}
	_, globals, err := metadataFromPluginSource(plugin.Entry, plugin.Source)
	if err != nil {
		return err
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	instCtx, cancel := context.WithCancel(ctx)
	inst := &pluginInstance{
		record: plugin, globals: globals, commands: map[string]pluginCommand{}, extensions: pluginExtensions{}, events: map[string][]starlark.Callable{}, cancel: cancel,
	}
	m.mu.Lock()
	m.instances[id] = inst
	m.mu.Unlock()
	if err := m.callHook(instCtx, inst, "enable"); err != nil {
		inst.lastError = err.Error()
	}
	if err := m.callHook(instCtx, inst, "activate"); err != nil {
		inst.lastError = err.Error()
		m.audit(id, "activate", "", false, err.Error(), "")
	} else {
		m.audit(id, "activate", "", true, "", "")
	}
	m.startSchedules(instCtx, inst)
	return m.setState(id, true, plugin.GrantedPermissions, plugin.AdministratorTrusted)
}

func (m *PluginManager) disable(id string) error {
	inst := m.stopInstance(id, true)
	if inst == nil {
		plugin, err := m.findPlugin(id)
		if err != nil {
			return err
		}
		return m.setState(id, false, plugin.GrantedPermissions, plugin.AdministratorTrusted)
	}
	return m.setState(id, false, inst.record.GrantedPermissions, inst.record.AdministratorTrusted)
}

func (m *PluginManager) stopInstance(id string, persistAudit bool) *pluginInstance {
	m.mu.Lock()
	inst := m.instances[id]
	delete(m.instances, id)
	m.mu.Unlock()
	if inst == nil {
		return nil
	}
	for _, cancel := range inst.timers {
		cancel()
	}
	if inst.cancel != nil {
		inst.cancel()
	}
	_ = m.callHook(context.Background(), inst, "deactivate")
	if persistAudit {
		m.audit(id, "deactivate", "", true, "", "")
	}
	return inst
}

func (m *PluginManager) findPlugin(id string) (pluginViewRecord, error) {
	plugins, err := m.listPlugins()
	if err != nil {
		return pluginViewRecord{}, err
	}
	for _, plugin := range plugins {
		if plugin.ID == id {
			return plugin, nil
		}
	}
	return pluginViewRecord{}, fmt.Errorf("plugin not found: %s", id)
}

func (m *PluginManager) startSchedules(ctx context.Context, inst *pluginInstance) {
	for _, schedule := range inst.record.Plugin.Schedules {
		interval, err := time.ParseDuration(schedule.Every)
		if err != nil || interval <= 0 || schedule.Handler == "" {
			inst.lastError = fmt.Sprintf("invalid schedule %s", schedule.ID)
			continue
		}
		handler, ok := inst.globals[schedule.Handler].(starlark.Callable)
		if !ok {
			inst.lastError = fmt.Sprintf("missing schedule handler %s", schedule.Handler)
			continue
		}
		timerCtx, cancel := context.WithCancel(ctx)
		inst.timers = append(inst.timers, cancel)
		go func(s pluginScheduleRecord, fn starlark.Callable) {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-timerCtx.Done():
					return
				case <-ticker.C:
					if _, err := m.callStarlark(timerCtx, inst, fn, starlark.Tuple{m.contextValue(inst)}); err != nil {
						inst.lastError = err.Error()
						m.audit(inst.record.ID, "schedule:"+s.ID, "", false, err.Error(), "")
					} else {
						m.audit(inst.record.ID, "schedule:"+s.ID, "", true, "", "")
					}
				}
			}
		}(schedule, handler)
	}
}

func (m *PluginManager) callHook(ctx context.Context, inst *pluginInstance, name string) error {
	fn, ok := inst.globals[name].(starlark.Callable)
	if !ok {
		return nil
	}
	_, err := m.callStarlark(ctx, inst, fn, starlark.Tuple{m.contextValue(inst)})
	return err
}

func (m *PluginManager) callStarlark(ctx context.Context, inst *pluginInstance, fn starlark.Callable, args starlark.Tuple) (starlark.Value, error) {
	runCtx, cancel := context.WithTimeout(ctx, defaultPluginTimeout)
	defer cancel()
	thread := &starlark.Thread{Name: inst.record.ID}
	thread.SetMaxExecutionSteps(defaultPluginMaxSteps)
	go func() {
		<-runCtx.Done()
		if runCtx.Err() != nil {
			thread.Cancel(runCtx.Err().Error())
		}
	}()
	return starlark.Call(thread, fn, args, nil)
}

func (inst *pluginInstance) commandViews() []pluginCommandView {
	commands := make([]pluginCommandView, 0, len(inst.commands))
	for _, command := range inst.commands {
		commands = append(commands, pluginCommandView{ID: command.ID, Title: command.Title, Subtitle: command.Subtitle})
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].ID < commands[j].ID })
	return commands
}

func metadataFromPluginSource(filename, source string) (pluginMetadataRecord, starlark.StringDict, error) {
	thread := &starlark.Thread{Name: "plugin-metadata"}
	thread.SetMaxExecutionSteps(defaultPluginMaxSteps)
	globals, err := starlark.ExecFile(thread, filename, source, starlark.StringDict{
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	})
	if err != nil {
		return pluginMetadataRecord{}, nil, err
	}
	value := globals["plugin"]
	if value == nil {
		return pluginMetadataRecord{}, nil, fmt.Errorf("plugin metadata is required")
	}
	meta, err := decodePluginMetadata(value)
	if err != nil {
		return pluginMetadataRecord{}, nil, err
	}
	return meta, globals, nil
}

func decodePluginMetadata(value starlark.Value) (pluginMetadataRecord, error) {
	raw, err := fromStarlark(value)
	if err != nil {
		return pluginMetadataRecord{}, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return pluginMetadataRecord{}, fmt.Errorf("plugin metadata must be a dict")
	}
	meta := pluginMetadataRecord{
		Schema: stringMapValue(obj, "schema"), ID: stringMapValue(obj, "id"), Name: stringMapValue(obj, "name"),
		Version: stringMapValue(obj, "version"), Description: stringMapValue(obj, "description"), Author: stringMapValue(obj, "author"),
		Homepage: stringMapValue(obj, "homepage"), License: stringMapValue(obj, "license"), Icon: stringMapValue(obj, "icon"),
		Permissions: stringSliceMapValue(obj, "permissions"), Settings: map[string]pluginSettingSchema{},
	}
	if meta.Schema != "yui.starlark-plugin.v0" {
		return meta, fmt.Errorf("unsupported plugin schema")
	}
	if !appIDPattern.MatchString(meta.ID) {
		return meta, fmt.Errorf("invalid plugin id")
	}
	if meta.Name == "" || meta.Version == "" {
		return meta, fmt.Errorf("plugin name and version are required")
	}
	if settings, ok := obj["settings"].(map[string]any); ok {
		for key, value := range settings {
			setting, ok := value.(map[string]any)
			if !ok {
				return meta, fmt.Errorf("setting %s must be a dict", key)
			}
			meta.Settings[key] = pluginSettingSchema{
				Type: stringMapValue(setting, "type"), Label: stringMapValue(setting, "label"), Description: stringMapValue(setting, "description"),
				Default: setting["default"], Options: stringSliceMapValue(setting, "options"), Required: boolMapValue(setting, "required"),
			}
			if meta.Settings[key].Type == "" {
				return meta, fmt.Errorf("setting %s type is required", key)
			}
		}
	}
	if schedules, ok := obj["schedules"].([]any); ok {
		for _, value := range schedules {
			schedule, ok := value.(map[string]any)
			if !ok {
				return meta, fmt.Errorf("schedule must be a dict")
			}
			meta.Schedules = append(meta.Schedules, pluginScheduleRecord{
				ID: stringMapValue(schedule, "id"), Every: stringMapValue(schedule, "every"), Handler: stringMapValue(schedule, "handler"),
			})
		}
	}
	return meta, nil
}

func (m *PluginManager) contextValue(inst *pluginInstance) starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"plugin": starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
			"id": starlark.String(inst.record.ID), "name": starlark.String(inst.record.Name), "version": starlark.String(inst.record.Version),
		}),
		"storage":  m.storageModule(inst),
		"settings": m.settingsModule(inst),
		"secrets":  m.secretsModule(inst),
		"commands": m.commandsModule(inst),
		"events":   m.eventsModule(inst),
		"network":  m.networkModule(inst),
		"fs":       m.fsModule(inst),
		"process":  m.processModule(inst),
		"shell":    m.shellModule(inst),
		"ui":       m.shellUIModule(inst),
		"logs":     m.logsModule(inst),
		"config":   m.configModule(inst),
		"system":   m.systemModule(inst),
		"time":     m.timeModule(inst),
	})
}

func (m *PluginManager) module(name string, members starlark.StringDict) *starlarkstruct.Module {
	return &starlarkstruct.Module{Name: name, Members: members}
}

func (m *PluginManager) storageModule(inst *pluginInstance) starlark.Value {
	return m.module("storage", starlark.StringDict{
		"get": starlark.NewBuiltin("storage.get", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "storage.read"); err != nil {
				return nil, err
			}
			var key string
			if err := starlark.UnpackPositionalArgs("storage.get", args, nil, 1, &key); err != nil {
				return nil, err
			}
			value, ok, err := m.pluginKV(pluginStorageCollection, inst.record.ID, key)
			if err != nil || !ok {
				return starlark.None, err
			}
			return toStarlark(value)
		}),
		"set": starlark.NewBuiltin("storage.set", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "storage.write"); err != nil {
				return nil, err
			}
			var key string
			var value starlark.Value
			if err := starlark.UnpackPositionalArgs("storage.set", args, nil, 2, &key, &value); err != nil {
				return nil, err
			}
			goValue, err := fromStarlark(value)
			if err != nil {
				return nil, err
			}
			return starlark.None, m.putPluginKV(pluginStorageCollection, inst.record.ID, key, goValue)
		}),
		"delete": starlark.NewBuiltin("storage.delete", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "storage.write"); err != nil {
				return nil, err
			}
			var key string
			if err := starlark.UnpackPositionalArgs("storage.delete", args, nil, 1, &key); err != nil {
				return nil, err
			}
			return starlark.None, m.deletePluginKV(pluginStorageCollection, inst.record.ID, key)
		}),
		"keys": starlark.NewBuiltin("storage.keys", func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "storage.read"); err != nil {
				return nil, err
			}
			keys, err := m.pluginKeys(pluginStorageCollection, inst.record.ID)
			if err != nil {
				return nil, err
			}
			return toStarlark(keys)
		}),
	})
}

func (m *PluginManager) settingsModule(inst *pluginInstance) starlark.Value {
	return m.module("settings", starlark.StringDict{
		"get": starlark.NewBuiltin("settings.get", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "settings.read"); err != nil {
				return nil, err
			}
			var key string
			if err := starlark.UnpackPositionalArgs("settings.get", args, nil, 1, &key); err != nil {
				return nil, err
			}
			if inst.record.Plugin.Settings[key].Type == "secret" {
				return m.secretValue(inst, key)
			}
			settings, err := m.settingsFor(inst.record.ID, inst.record.Plugin.Settings, false)
			if err != nil {
				return nil, err
			}
			return toStarlark(settings[key])
		}),
		"all": starlark.NewBuiltin("settings.all", func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "settings.read"); err != nil {
				return nil, err
			}
			settings, err := m.settingsFor(inst.record.ID, inst.record.Plugin.Settings, true)
			if err != nil {
				return nil, err
			}
			return toStarlark(settings)
		}),
	})
}

func (m *PluginManager) secretsModule(inst *pluginInstance) starlark.Value {
	return m.module("secrets", starlark.StringDict{
		"get": starlark.NewBuiltin("secrets.get", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "secrets.read"); err != nil {
				return nil, err
			}
			var key string
			if err := starlark.UnpackPositionalArgs("secrets.get", args, nil, 1, &key); err != nil {
				return nil, err
			}
			return m.secretValue(inst, key)
		}),
	})
}

func (m *PluginManager) commandsModule(inst *pluginInstance) starlark.Value {
	return m.module("commands", starlark.StringDict{
		"register": starlark.NewBuiltin("commands.register", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "commands.register"); err != nil {
				return nil, err
			}
			var spec starlark.Value
			if err := starlark.UnpackPositionalArgs("commands.register", args, nil, 1, &spec); err != nil {
				return nil, err
			}
			dict, ok := spec.(*starlark.Dict)
			if !ok {
				return nil, fmt.Errorf("command spec must be a dict")
			}
			runValue, _, err := dict.Get(starlark.String("run"))
			if err != nil {
				return nil, err
			}
			run, ok := runValue.(starlark.Callable)
			if !ok {
				return nil, fmt.Errorf("command run must be callable")
			}
			command := pluginCommand{ID: dictString(dict, "id"), Title: dictString(dict, "title"), Subtitle: dictString(dict, "subtitle"), Function: run}
			if command.ID == "" || command.Title == "" {
				return nil, fmt.Errorf("command id and title are required")
			}
			inst.commands[command.ID] = command
			return starlark.None, nil
		}),
	})
}

func (m *PluginManager) eventsModule(inst *pluginInstance) starlark.Value {
	return m.module("events", starlark.StringDict{
		"on": starlark.NewBuiltin("events.on", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "events"); err != nil {
				return nil, err
			}
			var event string
			var fn starlark.Value
			if err := starlark.UnpackPositionalArgs("events.on", args, nil, 2, &event, &fn); err != nil {
				return nil, err
			}
			callable, ok := fn.(starlark.Callable)
			if !ok {
				return nil, fmt.Errorf("event handler must be callable")
			}
			inst.events[event] = append(inst.events[event], callable)
			return starlark.None, nil
		}),
		"emit": starlark.NewBuiltin("events.emit", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "events"); err != nil {
				return nil, err
			}
			var event string
			var data starlark.Value = starlark.None
			if err := starlark.UnpackPositionalArgs("events.emit", args, nil, 1, &event, &data); err != nil {
				return nil, err
			}
			for _, fn := range inst.events[event] {
				if _, err := m.callStarlark(context.Background(), inst, fn, starlark.Tuple{m.contextValue(inst), data}); err != nil {
					return nil, err
				}
			}
			return starlark.None, nil
		}),
	})
}

func (m *PluginManager) networkModule(inst *pluginInstance) starlark.Value {
	return m.module("network", starlark.StringDict{
		"fetch": starlark.NewBuiltin("network.fetch", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "network.fetch"); err != nil {
				return nil, err
			}
			var request starlark.Value
			if err := starlark.UnpackPositionalArgs("network.fetch", args, nil, 1, &request); err != nil {
				return nil, err
			}
			raw, err := fromStarlark(request)
			if err != nil {
				return nil, err
			}
			obj, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("network.fetch expects a dict")
			}
			result, err := pluginFetch(obj)
			if err != nil {
				m.audit(inst.record.ID, "network.fetch", "network.fetch", false, err.Error(), stringMapValue(obj, "url"))
				return nil, err
			}
			m.audit(inst.record.ID, "network.fetch", "network.fetch", true, "", stringMapValue(obj, "url"))
			return toStarlark(result)
		}),
	})
}

func (m *PluginManager) fsModule(inst *pluginInstance) starlark.Value {
	return m.module("fs", starlark.StringDict{
		"read": starlark.NewBuiltin("fs.read", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "fs.read"); err != nil {
				return nil, err
			}
			var path string
			if err := starlark.UnpackPositionalArgs("fs.read", args, nil, 1, &path); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				m.audit(inst.record.ID, "fs.read", "fs.read", false, err.Error(), path)
				return nil, err
			}
			m.audit(inst.record.ID, "fs.read", "fs.read", true, "", path)
			return starlark.String(string(data)), nil
		}),
		"write": starlark.NewBuiltin("fs.write", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "fs.write"); err != nil {
				return nil, err
			}
			var path, data string
			if err := starlark.UnpackPositionalArgs("fs.write", args, nil, 2, &path, &data); err != nil {
				return nil, err
			}
			err := os.WriteFile(path, []byte(data), 0644)
			m.audit(inst.record.ID, "fs.write", "fs.write", err == nil, errorString(err), path)
			return starlark.None, err
		}),
		"list": starlark.NewBuiltin("fs.list", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "fs.list"); err != nil {
				return nil, err
			}
			var path string
			if err := starlark.UnpackPositionalArgs("fs.list", args, nil, 1, &path); err != nil {
				return nil, err
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() {
					name += string(os.PathSeparator)
				}
				names = append(names, name)
			}
			return toStarlark(names)
		}),
	})
}

func (m *PluginManager) processModule(inst *pluginInstance) starlark.Value {
	return m.module("process", starlark.StringDict{
		"run": starlark.NewBuiltin("process.run", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "process.run"); err != nil {
				return nil, err
			}
			var spec starlark.Value
			if err := starlark.UnpackPositionalArgs("process.run", args, nil, 1, &spec); err != nil {
				return nil, err
			}
			obj, err := starlarkDict(spec)
			if err != nil {
				return nil, err
			}
			result, err := runProcess(obj, false)
			m.audit(inst.record.ID, "process.run", "process.run", err == nil, errorString(err), stringMapValue(obj, "exe"))
			if err != nil {
				return nil, err
			}
			return toStarlark(result)
		}),
	})
}

func (m *PluginManager) shellModule(inst *pluginInstance) starlark.Value {
	return m.module("shell", starlark.StringDict{
		"run": starlark.NewBuiltin("shell.run", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "shell.run"); err != nil {
				return nil, err
			}
			var spec starlark.Value
			if err := starlark.UnpackPositionalArgs("shell.run", args, nil, 1, &spec); err != nil {
				return nil, err
			}
			obj, err := starlarkDict(spec)
			if err != nil {
				return nil, err
			}
			result, err := runProcess(obj, true)
			m.audit(inst.record.ID, "shell.run", "shell.run", err == nil, errorString(err), stringMapValue(obj, "command"))
			if err != nil {
				return nil, err
			}
			return toStarlark(result)
		}),
		"register_page": starlark.NewBuiltin("shell.register_page", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return m.registerShellPage(inst, "shell.register_page", args, kwargs)
		}),
		"register_action": starlark.NewBuiltin("shell.register_action", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return m.registerShellAction(inst, "shell.register_action", args, kwargs)
		}),
		"add_css": starlark.NewBuiltin("shell.add_css", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return m.addShellCSS(inst, "shell.add_css", args, kwargs)
		}),
	})
}

func (m *PluginManager) shellUIModule(inst *pluginInstance) starlark.Value {
	return m.module("ui", starlark.StringDict{
		"register_page": starlark.NewBuiltin("ui.register_page", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return m.registerShellPage(inst, "ui.register_page", args, kwargs)
		}),
		"register_action": starlark.NewBuiltin("ui.register_action", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return m.registerShellAction(inst, "ui.register_action", args, kwargs)
		}),
		"add_css": starlark.NewBuiltin("ui.add_css", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			return m.addShellCSS(inst, "ui.add_css", args, kwargs)
		}),
	})
}

func (m *PluginManager) registerShellPage(inst *pluginInstance, name string, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if err := m.require(inst, "shell.pages"); err != nil {
		return nil, err
	}
	var spec starlark.Value
	if err := starlark.UnpackPositionalArgs(name, args, nil, 1, &spec); err != nil {
		return nil, err
	}
	obj, err := starlarkDict(spec)
	if err != nil {
		return nil, err
	}
	page := pluginShellPage{
		ID:       pluginExtensionID(inst.record.ID, stringMapValue(obj, "id")),
		PluginID: inst.record.ID,
		Title:    stringMapValue(obj, "title"),
		Icon:     stringMapValue(obj, "icon"),
		Order:    intMapValue(obj, "order"),
		CSS:      stringMapValue(obj, "css"),
	}
	if page.ID == "" || page.Title == "" {
		return nil, fmt.Errorf("page id and title are required")
	}
	if blocks, ok := obj["blocks"].([]any); ok {
		page.Blocks = blocks
	}
	inst.extensions.Pages = append(inst.extensions.Pages, page)
	m.audit(inst.record.ID, name, "shell.pages", true, "", page.ID)
	return starlark.None, nil
}

func (m *PluginManager) registerShellAction(inst *pluginInstance, name string, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if err := m.require(inst, "shell.actions"); err != nil {
		return nil, err
	}
	var spec starlark.Value
	if err := starlark.UnpackPositionalArgs(name, args, nil, 1, &spec); err != nil {
		return nil, err
	}
	obj, err := starlarkDict(spec)
	if err != nil {
		return nil, err
	}
	action := pluginShellAction{
		ID:       pluginExtensionID(inst.record.ID, stringMapValue(obj, "id")),
		PluginID: inst.record.ID,
		Location: stringMapValue(obj, "location"),
		Title:    stringMapValue(obj, "title"),
		Icon:     stringMapValue(obj, "icon"),
		Command:  stringMapValue(obj, "command"),
	}
	if action.ID == "" || action.Location == "" || action.Title == "" {
		return nil, fmt.Errorf("action id, location, and title are required")
	}
	inst.extensions.Actions = append(inst.extensions.Actions, action)
	m.audit(inst.record.ID, name, "shell.actions", true, "", action.ID)
	return starlark.None, nil
}

func (m *PluginManager) addShellCSS(inst *pluginInstance, name string, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if err := m.require(inst, "shell.css"); err != nil {
		return nil, err
	}
	var css string
	if err := starlark.UnpackPositionalArgs(name, args, nil, 1, &css); err != nil {
		return nil, err
	}
	inst.extensions.CSS = append(inst.extensions.CSS, css)
	m.audit(inst.record.ID, name, "shell.css", true, "", truncate(css, 120))
	return starlark.None, nil
}

func (m *PluginManager) logsModule(inst *pluginInstance) starlark.Value {
	return m.module("logs", starlark.StringDict{
		"info": starlark.NewBuiltin("logs.info", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			var message string
			if err := starlark.UnpackPositionalArgs("logs.info", args, nil, 1, &message); err != nil {
				return nil, err
			}
			m.audit(inst.record.ID, "log.info", "", true, "", message)
			log.Printf("plugin %s: %s", inst.record.ID, message)
			return starlark.None, nil
		}),
		"error": starlark.NewBuiltin("logs.error", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			var message string
			if err := starlark.UnpackPositionalArgs("logs.error", args, nil, 1, &message); err != nil {
				return nil, err
			}
			m.audit(inst.record.ID, "log.error", "", false, message, "")
			log.Printf("plugin %s error: %s", inst.record.ID, message)
			return starlark.None, nil
		}),
	})
}

func (m *PluginManager) configModule(inst *pluginInstance) starlark.Value {
	return m.module("config", starlark.StringDict{
		"get": starlark.NewBuiltin("config.get", func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "config.read"); err != nil {
				return nil, err
			}
			return toStarlark(map[string]any{
				"config_path": m.r.cfg.ConfigPath, "config_dir": m.r.cfg.ConfigDir, "url": m.r.cfg.URL,
				"platform_http_addr": m.r.cfg.PlatformHTTPAddr, "platform_bridge_addr": m.r.cfg.PlatformBridgeAddr,
			})
		}),
	})
}

func (m *PluginManager) systemModule(inst *pluginInstance) starlark.Value {
	return m.module("system", starlark.StringDict{
		"status": starlark.NewBuiltin("system.status", func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if err := m.require(inst, "system.status"); err != nil {
				return nil, err
			}
			return toStarlark(map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "time": time.Now().UTC().Format(time.RFC3339)})
		}),
	})
}

func (m *PluginManager) timeModule(inst *pluginInstance) starlark.Value {
	return m.module("time", starlark.StringDict{
		"now_ms": starlark.NewBuiltin("time.now_ms", func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.MakeInt64(time.Now().UnixMilli()), nil
		}),
	})
}

func (m *PluginManager) require(inst *pluginInstance, permission string) error {
	for _, granted := range inst.record.GrantedPermissions {
		if granted == permission {
			if isAdminGatedPermission(permission) && !inst.record.AdministratorTrusted {
				err := fmt.Errorf("plugin %s requires administrator access: %s", inst.record.ID, permission)
				m.audit(inst.record.ID, "administrator.denied", permission, false, err.Error(), "")
				return err
			}
			return nil
		}
	}
	err := fmt.Errorf("plugin %s permission denied: %s", inst.record.ID, permission)
	m.audit(inst.record.ID, "permission.denied", permission, false, err.Error(), "")
	return err
}

func isAdminGatedPermission(permission string) bool {
	switch permission {
	case "process.run", "shell.run", "fs.write", "shell.pages", "shell.css", "shell.actions":
		return true
	default:
		return false
	}
}

func pluginExtensionID(pluginID, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return pluginID + ":" + id
}

func (m *PluginManager) pluginKV(collectionName, pluginID, key string) (any, bool, error) {
	db, err := m.r.Store()
	if err != nil {
		return nil, false, err
	}
	doc, ok, err := db.Collection(collectionName).Get(pluginID + ":" + key)
	if err != nil || !ok {
		return nil, ok, err
	}
	var value any
	return value, true, json.Unmarshal(doc.Value, &value)
}

func (m *PluginManager) putPluginKV(collectionName, pluginID, key string, value any) error {
	db, err := m.r.Store()
	if err != nil {
		return err
	}
	return db.Collection(collectionName).Put(pluginID+":"+key, value)
}

func (m *PluginManager) deletePluginKV(collectionName, pluginID, key string) error {
	db, err := m.r.Store()
	if err != nil {
		return err
	}
	return db.Collection(collectionName).Delete(pluginID + ":" + key)
}

func (m *PluginManager) pluginKeys(collectionName, pluginID string) ([]string, error) {
	db, err := m.r.Store()
	if err != nil {
		return nil, err
	}
	prefix := pluginID + ":"
	ids, err := db.Collection(collectionName).Keys(store.ListOptions{Prefix: prefix})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, strings.TrimPrefix(id, prefix))
	}
	return keys, nil
}

func (m *PluginManager) settingsFor(pluginID string, schema map[string]pluginSettingSchema, redactSecrets bool) (map[string]any, error) {
	db, err := m.r.Store()
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	_, _ = db.Collection(pluginSettingsCollection).Decode(pluginID, &values)
	for key, spec := range schema {
		if _, ok := values[key]; !ok {
			values[key] = spec.Default
		}
		if spec.Type == "secret" && redactSecrets {
			if _, ok := values[key]; ok {
				values[key] = "********"
			}
		}
	}
	return values, nil
}

func (m *PluginManager) secretValue(inst *pluginInstance, key string) (starlark.Value, error) {
	if err := m.require(inst, "secrets.read"); err != nil {
		return nil, err
	}
	value, ok, err := m.pluginKV(pluginSecretsCollection, inst.record.ID, key)
	if err != nil || !ok {
		return starlark.None, err
	}
	return toStarlark(value)
}

func (m *PluginManager) audit(pluginID, action, permission string, ok bool, errText, detail string) {
	db, err := m.r.Store()
	if err != nil {
		log.Printf("plugin audit failed: %v", err)
		return
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("%s:%d", pluginID, now.UnixNano())
	record := pluginAuditRecord{ID: id, PluginID: pluginID, Action: action, Permission: permission, At: now.Format(time.RFC3339), OK: ok, Error: errText, Detail: truncate(detail, 500)}
	if err := db.Collection(pluginAuditCollection).Put(id, record); err != nil {
		log.Printf("plugin audit failed: %v", err)
	}
}

type PluginSourcesListCommand struct{}

func (PluginSourcesListCommand) Name() string { return "plugins.sources.list" }
func (PluginSourcesListCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return listCollection[pluginSourceRecord](r, pluginSourcesCollection)
}

type PluginSourcesAddCommand struct{}

func (PluginSourcesAddCommand) Name() string { return "plugins.sources.add" }
func (PluginSourcesAddCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if err := requireHTTPS(p.URL); err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record := pluginSourceRecord{ID: sourceID(p.URL), URL: p.URL, LastStatus: "pending", CreatedAt: now, UpdatedAt: now}
	if err := db.Collection(pluginSourcesCollection).Put(record.ID, record); err != nil {
		return nil, err
	}
	return r.plugins.refreshSource(record.ID)
}

type PluginSourcesRemoveCommand struct{}

func (PluginSourcesRemoveCommand) Name() string { return "plugins.sources.remove" }
func (PluginSourcesRemoveCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
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
	entries, err := db.Collection(pluginCatalogCollection).List(store.ListOptions{Prefix: p.ID + ":"})
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := db.Collection(pluginCatalogCollection).Delete(entry.ID); err != nil {
			return nil, err
		}
	}
	return nil, db.Collection(pluginSourcesCollection).Delete(p.ID)
}

type PluginSourcesRefreshCommand struct{}

func (PluginSourcesRefreshCommand) Name() string { return "plugins.sources.refresh" }
func (PluginSourcesRefreshCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return r.plugins.refreshSource(p.ID)
}

func (m *PluginManager) refreshSource(id string) (pluginSourceRecord, error) {
	db, err := m.r.Store()
	if err != nil {
		return pluginSourceRecord{}, err
	}
	var source pluginSourceRecord
	ok, err := db.Collection(pluginSourcesCollection).Decode(id, &source)
	if err != nil {
		return pluginSourceRecord{}, err
	}
	if !ok {
		return pluginSourceRecord{}, fmt.Errorf("plugin source not found")
	}
	catalog, err := fetchPluginCatalog(source.URL)
	now := time.Now().UTC().Format(time.RFC3339)
	source.UpdatedAt = now
	source.LastRefreshed = now
	if err != nil {
		source.LastStatus = "error"
		source.LastError = err.Error()
		_ = db.Collection(pluginSourcesCollection).Put(source.ID, source)
		return source, err
	}
	source.Name = catalog.Name
	source.Publisher = catalog.Publisher
	source.SigningKeys = catalog.SigningKeys
	source.LastStatus = "ok"
	source.LastError = ""
	source.DiscoveredItems = len(catalog.Plugins)
	oldEntries, err := db.Collection(pluginCatalogCollection).List(store.ListOptions{Prefix: source.ID + ":"})
	if err != nil {
		return source, err
	}
	for _, old := range oldEntries {
		if err := db.Collection(pluginCatalogCollection).Delete(old.ID); err != nil {
			return source, err
		}
	}
	for _, plugin := range catalog.Plugins {
		if err := validateCatalogPlugin(plugin); err != nil {
			return source, err
		}
		entry := pluginCatalogEntryRecord{SourceID: source.ID, SourceURL: source.URL, Catalog: catalog.Name, Publisher: catalog.Publisher, Plugin: plugin, Verified: true, UpdatedAt: now}
		if err := db.Collection(pluginCatalogCollection).Put(catalogEntryID(source.ID, plugin.ID, plugin.Version), entry); err != nil {
			return source, err
		}
	}
	return source, db.Collection(pluginSourcesCollection).Put(source.ID, source)
}

type PluginCatalogListCommand struct{}

func (PluginCatalogListCommand) Name() string { return "plugins.catalog.list" }
func (PluginCatalogListCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return listCollection[pluginCatalogEntryRecord](r, pluginCatalogCollection)
}

type PluginInstalledListCommand struct{}

func (PluginInstalledListCommand) Name() string { return "plugins.installed.list" }
func (PluginInstalledListCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return r.plugins.listPlugins()
}

type PluginInstallCommand struct{}

func (PluginInstallCommand) Name() string { return "plugins.install" }
func (PluginInstallCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
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
	var entry pluginCatalogEntryRecord
	ok, err := db.Collection(pluginCatalogCollection).Decode(p.CatalogID, &entry)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("catalog plugin not found")
	}
	var sourceRecord pluginSourceRecord
	ok, err = db.Collection(pluginSourcesCollection).Decode(entry.SourceID, &sourceRecord)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("plugin source trust record not found")
	}
	source, err := fetchText(entry.Plugin.SourceURL, maxPluginFetchBytes)
	if err != nil {
		return nil, err
	}
	if err := verifyCatalogPlugin(entry, sourceRecord.SigningKeys, []byte(source)); err != nil {
		return nil, err
	}
	meta, _, err := metadataFromPluginSource(entry.Plugin.SourceURL, source)
	if err != nil {
		return nil, err
	}
	if err := pluginMetadataMatchesCatalog(meta, entry.Plugin); err != nil {
		return nil, err
	}
	installed := installedPluginRecord{
		ID: meta.ID, Name: meta.Name, Version: meta.Version, Type: "starlark", Entry: entry.Plugin.SourceURL,
		SourceID: entry.SourceID, SourceURL: entry.Plugin.SourceURL, InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Plugin: meta, Source: source, Signature: entry.Plugin.Signature, SourceSHA256: strings.ToLower(entry.Plugin.SourceSHA256),
		Enabled: false, GrantedPermissions: append([]string(nil), meta.Permissions...),
	}
	if err := db.Collection(installedPluginsCollection).Put(installed.ID, installed); err != nil {
		return nil, err
	}
	if err := r.plugins.setState(installed.ID, false, installed.GrantedPermissions, false); err != nil {
		return nil, err
	}
	return installed, nil
}

type PluginUninstallCommand struct{}

func (PluginUninstallCommand) Name() string { return "plugins.uninstall" }
func (PluginUninstallCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	_ = r.plugins.disable(p.ID)
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	return nil, db.Collection(installedPluginsCollection).Delete(p.ID)
}

type PluginEnableCommand struct{}

func (PluginEnableCommand) Name() string { return "plugins.enable" }
func (PluginEnableCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return nil, r.plugins.enable(p.ID)
}

type PluginDisableCommand struct{}

func (PluginDisableCommand) Name() string { return "plugins.disable" }
func (PluginDisableCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return nil, r.plugins.disable(p.ID)
}

type PluginPermissionsUpdateCommand struct{}

func (PluginPermissionsUpdateCommand) Name() string { return "plugins.permissions.update" }
func (PluginPermissionsUpdateCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID          string   `json:"id"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	plugin, err := r.plugins.findPlugin(p.ID)
	if err != nil {
		return nil, err
	}
	granted := filterDeclared(p.Permissions, plugin.Plugin.Permissions)
	if err := r.plugins.setState(p.ID, plugin.Enabled, granted, plugin.AdministratorTrusted); err != nil {
		return nil, err
	}
	r.plugins.mu.Lock()
	if inst := r.plugins.instances[p.ID]; inst != nil {
		inst.record.GrantedPermissions = granted
	}
	r.plugins.mu.Unlock()
	return granted, nil
}

type PluginAdministratorUpdateCommand struct{}

func (PluginAdministratorUpdateCommand) Name() string { return "plugins.administrator.update" }
func (PluginAdministratorUpdateCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID      string `json:"id"`
		Trusted bool   `json:"trusted"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	plugin, err := r.plugins.findPlugin(p.ID)
	if err != nil {
		return nil, err
	}
	if err := r.plugins.setState(p.ID, plugin.Enabled, plugin.GrantedPermissions, p.Trusted); err != nil {
		return nil, err
	}
	r.plugins.mu.Lock()
	if inst := r.plugins.instances[p.ID]; inst != nil {
		inst.record.AdministratorTrusted = p.Trusted
	}
	r.plugins.mu.Unlock()
	r.plugins.audit(p.ID, "administrator.update", "", true, "", fmt.Sprintf("trusted=%t", p.Trusted))
	if plugin.Enabled {
		if err := r.plugins.disable(p.ID); err != nil {
			return nil, err
		}
		if err := r.plugins.enable(p.ID); err != nil {
			return nil, err
		}
	}
	return p.Trusted, nil
}

type PluginSettingsGetCommand struct{}

func (PluginSettingsGetCommand) Name() string { return "plugins.settings.get" }
func (PluginSettingsGetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	plugin, err := r.plugins.findPlugin(p.ID)
	if err != nil {
		return nil, err
	}
	return r.plugins.settingsFor(p.ID, plugin.Plugin.Settings, true)
}

type PluginSettingsUpdateCommand struct{}

func (PluginSettingsUpdateCommand) Name() string { return "plugins.settings.update" }
func (PluginSettingsUpdateCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID       string         `json:"id"`
		Settings map[string]any `json:"settings"`
		Secrets  map[string]any `json:"secrets"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	plugin, err := r.plugins.findPlugin(p.ID)
	if err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	current, err := r.plugins.settingsFor(p.ID, plugin.Plugin.Settings, false)
	if err != nil {
		return nil, err
	}
	for key, value := range p.Settings {
		if plugin.Plugin.Settings[key].Type == "secret" {
			continue
		}
		current[key] = value
	}
	if err := db.Collection(pluginSettingsCollection).Put(p.ID, current); err != nil {
		return nil, err
	}
	for key, value := range p.Secrets {
		if plugin.Plugin.Settings[key].Type == "secret" {
			if err := r.plugins.putPluginKV(pluginSecretsCollection, p.ID, key, value); err != nil {
				return nil, err
			}
		}
	}
	r.plugins.mu.Lock()
	inst := r.plugins.instances[p.ID]
	r.plugins.mu.Unlock()
	if inst != nil {
		if fn, ok := inst.globals["settings_changed"].(starlark.Callable); ok {
			changed, _ := toStarlark(map[string]any{"settings": p.Settings, "secrets": keysOf(p.Secrets)})
			_, _ = r.plugins.callStarlark(context.Background(), inst, fn, starlark.Tuple{r.plugins.contextValue(inst), changed})
		}
	}
	return r.plugins.settingsFor(p.ID, plugin.Plugin.Settings, true)
}

type PluginLogsListCommand struct{}

func (PluginLogsListCommand) Name() string { return "plugins.logs.list" }
func (PluginLogsListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	docs, err := db.Collection(pluginAuditCollection).List(store.ListOptions{Prefix: p.ID + ":", Limit: p.Limit, Reverse: true})
	if err != nil {
		return nil, err
	}
	result := make([]pluginAuditRecord, 0, len(docs))
	for _, doc := range docs {
		var record pluginAuditRecord
		if err := json.Unmarshal(doc.Value, &record); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

type PluginExtensionsListCommand struct{}

func (PluginExtensionsListCommand) Name() string { return "plugins.extensions.list" }
func (PluginExtensionsListCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	extensions := pluginExtensions{
		Pages:   []pluginShellPage{},
		Actions: []pluginShellAction{},
		CSS:     []string{},
	}
	r.plugins.mu.Lock()
	instances := make([]*pluginInstance, 0, len(r.plugins.instances))
	for _, inst := range r.plugins.instances {
		instances = append(instances, inst)
	}
	r.plugins.mu.Unlock()
	sort.Slice(instances, func(i, j int) bool { return instances[i].record.Name < instances[j].record.Name })
	for _, inst := range instances {
		extensions.Pages = append(extensions.Pages, inst.extensions.Pages...)
		extensions.Actions = append(extensions.Actions, inst.extensions.Actions...)
		extensions.CSS = append(extensions.CSS, inst.extensions.CSS...)
	}
	sort.Slice(extensions.Pages, func(i, j int) bool {
		if extensions.Pages[i].Order == extensions.Pages[j].Order {
			return extensions.Pages[i].Title < extensions.Pages[j].Title
		}
		return extensions.Pages[i].Order < extensions.Pages[j].Order
	})
	return extensions, nil
}

type PluginRunCommand struct{}

func (PluginRunCommand) Name() string { return "plugins.run" }
func (PluginRunCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		ID      string `json:"id"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	r.plugins.mu.Lock()
	inst := r.plugins.instances[p.ID]
	r.plugins.mu.Unlock()
	if inst == nil {
		return nil, fmt.Errorf("plugin is not enabled")
	}
	command, ok := inst.commands[p.Command]
	if !ok {
		return nil, fmt.Errorf("unknown plugin command: %s", p.Command)
	}
	value, err := r.plugins.callStarlark(context.Background(), inst, command.Function, starlark.Tuple{r.plugins.contextValue(inst)})
	if err != nil {
		inst.lastError = err.Error()
		r.plugins.audit(p.ID, "command:"+p.Command, "", false, err.Error(), "")
		return nil, err
	}
	result, err := fromStarlark(value)
	if err != nil {
		return nil, err
	}
	r.plugins.audit(p.ID, "command:"+p.Command, "", true, "", "")
	return result, nil
}

func fetchPluginCatalog(rawURL string) (pluginCatalogDocument, error) {
	var catalog pluginCatalogDocument
	body, err := fetchText(rawURL, maxPluginFetchBytes)
	if err != nil {
		return catalog, err
	}
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		return catalog, err
	}
	if catalog.Schema != "yui.plugin-catalog.v0" {
		return catalog, fmt.Errorf("unsupported plugin catalog schema")
	}
	if catalog.Name == "" || catalog.Publisher == "" {
		return catalog, fmt.Errorf("plugin catalog name and publisher are required")
	}
	if len(catalog.SigningKeys) == 0 {
		return catalog, fmt.Errorf("plugin catalog signingKeys are required")
	}
	for _, key := range catalog.SigningKeys {
		if _, err := decodeEd25519PublicKey(key); err != nil {
			return catalog, err
		}
	}
	return catalog, nil
}

func validateCatalogPlugin(plugin pluginCatalogRecord) error {
	if !appIDPattern.MatchString(plugin.ID) {
		return fmt.Errorf("invalid plugin id: %s", plugin.ID)
	}
	if plugin.Name == "" || plugin.Version == "" {
		return fmt.Errorf("catalog plugin name and version are required")
	}
	if err := requireHTTPS(plugin.SourceURL); err != nil {
		return err
	}
	if len(plugin.SourceSHA256) != 64 {
		return fmt.Errorf("sourceSha256 must be a hex sha256")
	}
	if _, err := hex.DecodeString(plugin.SourceSHA256); err != nil {
		return fmt.Errorf("sourceSha256 must be a hex sha256")
	}
	if plugin.Signature == "" {
		return fmt.Errorf("plugin signature is required")
	}
	return nil
}

func verifyCatalogPlugin(entry pluginCatalogEntryRecord, signingKeys []string, source []byte) error {
	hash := sha256.Sum256(source)
	actual := hex.EncodeToString(hash[:])
	if !strings.EqualFold(actual, entry.Plugin.SourceSHA256) {
		return fmt.Errorf("plugin source hash mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(entry.Plugin.Signature)
	if err != nil {
		return fmt.Errorf("decode plugin signature: %w", err)
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
	return fmt.Errorf("plugin signature could not be verified")
}

func pluginMetadataMatchesCatalog(meta pluginMetadataRecord, plugin pluginCatalogRecord) error {
	if meta.ID != plugin.ID || meta.Name != plugin.Name || meta.Version != plugin.Version {
		return fmt.Errorf("plugin metadata does not match catalog")
	}
	if !sameStringSet(meta.Permissions, plugin.Permissions) {
		return fmt.Errorf("plugin permissions do not match catalog")
	}
	return nil
}

func filterDeclared(granted, declared []string) []string {
	declaredSet := map[string]bool{}
	for _, permission := range declared {
		declaredSet[permission] = true
	}
	var result []string
	for _, permission := range granted {
		if declaredSet[permission] {
			result = append(result, permission)
		}
	}
	sort.Strings(result)
	return result
}

func pluginFetch(obj map[string]any) (map[string]any, error) {
	rawURL := stringMapValue(obj, "url")
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("network.fetch only supports http and https urls")
	}
	method := strings.ToUpper(stringMapValue(obj, "method"))
	if method == "" {
		method = http.MethodGet
	}
	body := stringMapValue(obj, "body")
	request, err := http.NewRequest(method, parsedURL.String(), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	if headers, ok := obj["headers"].(map[string]any); ok {
		for key, value := range headers {
			request.Header.Set(key, fmt.Sprint(value))
		}
	}
	client := http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxNetworkFetchBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxNetworkFetchBodyBytes {
		return nil, fmt.Errorf("network.fetch response exceeded %d bytes", maxNetworkFetchBodyBytes)
	}
	return map[string]any{"url": response.Request.URL.String(), "status": response.StatusCode, "body": string(data)}, nil
}

func runProcess(obj map[string]any, shell bool) (map[string]any, error) {
	timeout := intMapValue(obj, "timeout_ms")
	if timeout <= 0 {
		timeout = int(defaultPluginTimeout / time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
	defer cancel()
	var cmd *exec.Cmd
	if shell {
		command := stringMapValue(obj, "command")
		if command == "" {
			return nil, fmt.Errorf("shell command is required")
		}
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/C", command)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-lc", command)
		}
	} else {
		exe := stringMapValue(obj, "exe")
		if exe == "" {
			return nil, fmt.Errorf("process exe is required")
		}
		cmd = exec.CommandContext(ctx, exe, stringSliceMapValue(obj, "args")...)
	}
	if cwd := stringMapValue(obj, "cwd"); cwd != "" {
		cmd.Dir = cwd
	}
	stdout := &limitedBuffer{limit: maxProcessOutputBytes}
	stderr := &limitedBuffer{limit: maxProcessOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
			err = nil
		}
	}
	return map[string]any{"code": code, "stdout": stdout.String(), "stderr": stderr.String()}, err
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w.limit <= 0 || w.buf.Len() >= w.limit {
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if len(p) > remaining {
		_, _ = w.buf.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

func (w *limitedBuffer) String() string {
	return w.buf.String()
}

func starlarkDict(value starlark.Value) (map[string]any, error) {
	raw, err := fromStarlark(value)
	if err != nil {
		return nil, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected dict")
	}
	return obj, nil
}

func dictString(dict *starlark.Dict, key string) string {
	value, ok, err := dict.Get(starlark.String(key))
	if err != nil || !ok {
		return ""
	}
	text, ok := starlark.AsString(value)
	if !ok {
		return ""
	}
	return text
}

func toStarlark(value any) (starlark.Value, error) {
	switch v := value.(type) {
	case nil:
		return starlark.None, nil
	case string:
		return starlark.String(v), nil
	case bool:
		return starlark.Bool(v), nil
	case int:
		return starlark.MakeInt(v), nil
	case int64:
		return starlark.MakeInt64(v), nil
	case float64:
		return starlark.Float(v), nil
	case []string:
		items := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			items = append(items, starlark.String(item))
		}
		return starlark.NewList(items), nil
	case []any:
		items := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return starlark.NewList(items), nil
	case map[string]any:
		dict := starlark.NewDict(len(v))
		for key, item := range v {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var decoded any
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, err
		}
		return toStarlark(decoded)
	}
}

func fromStarlark(value starlark.Value) (any, error) {
	switch v := value.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.String:
		return string(v), nil
	case starlark.Bool:
		return bool(v), nil
	case starlark.Int:
		if i, ok := v.Int64(); ok {
			return i, nil
		}
		return nil, fmt.Errorf("integer out of range")
	case starlark.Float:
		return float64(v), nil
	case *starlark.List:
		result := make([]any, 0, v.Len())
		iter := v.Iterate()
		defer iter.Done()
		var item starlark.Value
		for iter.Next(&item) {
			converted, err := fromStarlark(item)
			if err != nil {
				return nil, err
			}
			result = append(result, converted)
		}
		return result, nil
	case starlark.Tuple:
		result := make([]any, 0, len(v))
		for _, item := range v {
			converted, err := fromStarlark(item)
			if err != nil {
				return nil, err
			}
			result = append(result, converted)
		}
		return result, nil
	case *starlark.Dict:
		result := map[string]any{}
		for _, item := range v.Items() {
			key, ok := starlark.AsString(item[0])
			if !ok {
				return nil, fmt.Errorf("dict keys must be strings")
			}
			if _, ok := item[1].(starlark.Callable); ok {
				continue
			}
			converted, err := fromStarlark(item[1])
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported starlark value: %s", value.Type())
	}
}

func stringMapValue(obj map[string]any, key string) string {
	if value, ok := obj[key].(string); ok {
		return value
	}
	return ""
}

func boolMapValue(obj map[string]any, key string) bool {
	if value, ok := obj[key].(bool); ok {
		return value
	}
	return false
}

func intMapValue(obj map[string]any, key string) int {
	switch value := obj[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}

func stringSliceMapValue(obj map[string]any, key string) []string {
	raw, ok := obj[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return value
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	}
	return nil
}

func keysOf(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
