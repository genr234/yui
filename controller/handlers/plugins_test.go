package handlers

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"kiosk/controller/internal/config"
)

func TestLocalStarlarkPluginEnableAndRun(t *testing.T) {
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.plugin",
    "name": "Test Plugin",
    "version": "0.1.0",
    "permissions": ["commands.register"],
}

def activate(ctx):
    ctx.commands.register({"id": "ping", "title": "Ping", "run": ping})

def ping(ctx):
    return {"ok": True}
`,
	})
	defer r.Close()

	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.plugin"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	result, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.plugin", "command": "ping"}))
	if err != nil {
		t.Fatalf("run plugin command: %v", err)
	}
	obj, ok := result.(map[string]any)
	if !ok || obj["ok"] != true {
		t.Fatalf("unexpected command result: %#v", result)
	}
}

func TestStarlarkPluginPermissionDenial(t *testing.T) {
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.denied",
    "name": "Denied Plugin",
    "version": "0.1.0",
    "permissions": ["commands.register", "system.status"],
}

def activate(ctx):
    ctx.commands.register({"id": "status", "title": "Status", "run": status})

def status(ctx):
    return ctx.system.status()
`,
	})
	defer r.Close()

	if _, err := r.Dispatch("plugins.permissions.update", mustJSON(t, map[string]any{
		"id":          "test.denied",
		"permissions": []string{"commands.register"},
	})); err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.denied"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.denied", "command": "status"})); err == nil {
		t.Fatalf("expected permission denial")
	}
}

func TestStarlarkPluginAdministratorGate(t *testing.T) {
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.admin",
    "name": "Admin Plugin",
    "version": "0.1.0",
    "permissions": ["commands.register", "process.run"],
}

def activate(ctx):
    ctx.commands.register({"id": "run", "title": "Run", "run": run})

def run(ctx):
    return ctx.process.run({"exe": "go", "args": ["version"], "timeout_ms": 5000})
`,
	})
	defer r.Close()

	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.admin"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.admin", "command": "run"})); err == nil {
		t.Fatalf("expected administrator denial")
	}
	if _, err := r.Dispatch("plugins.administrator.update", mustJSON(t, map[string]any{"id": "test.admin", "trusted": true})); err != nil {
		t.Fatalf("grant administrator access: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.admin", "command": "run"})); err != nil {
		t.Fatalf("run with administrator access: %v", err)
	}
	if _, err := r.Dispatch("plugins.administrator.update", mustJSON(t, map[string]any{"id": "test.admin", "trusted": false})); err != nil {
		t.Fatalf("revoke administrator access: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.admin", "command": "run"})); err == nil {
		t.Fatalf("expected administrator denial after revoke")
	}
}

func TestStarlarkPluginFilesystemScopedByDefault(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "inside.txt")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	files := map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.fs-scoped",
    "name": "FS Scoped Plugin",
    "version": "0.1.0",
    "permissions": ["commands.register", "fs.read"],
}

def activate(ctx):
    ctx.commands.register({"id": "read_inside", "title": "Read Inside", "run": read_inside})
    ctx.commands.register({"id": "read_outside", "title": "Read Outside", "run": read_outside})

def read_inside(ctx):
    return ctx.fs.read(` + strconv.Quote(inside) + `)

def read_outside(ctx):
    return ctx.fs.read(` + strconv.Quote(outside) + `)
`,
	}
	writeTestPluginFiles(t, dir, files)
	r := NewRegistry(config.Config{ConfigDir: dir, StorePath: filepath.Join(dir, "store.db")})
	defer r.Close()

	if err := os.WriteFile(inside, []byte("inside"), 0644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.fs-scoped"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.fs-scoped", "command": "read_inside"})); err != nil {
		t.Fatalf("read inside configured directory: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.fs-scoped", "command": "read_outside"})); err == nil {
		t.Fatalf("expected outside path denial")
	}
}

func TestStarlarkPluginFilesystemFullDiskRequiresAdministratorTrust(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.fs-full",
    "name": "FS Full Plugin",
    "version": "0.1.0",
    "permissions": ["commands.register", "fs.read", "fs.full_disk"],
}

def activate(ctx):
    ctx.commands.register({"id": "read", "title": "Read", "run": read})

def read(ctx):
    return ctx.fs.read(` + strconv.Quote(outside) + `)
`,
	})
	defer r.Close()

	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.fs-full"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.fs-full", "command": "read"})); err == nil {
		t.Fatalf("expected full disk administrator denial")
	}
	if _, err := r.Dispatch("plugins.administrator.update", mustJSON(t, map[string]any{"id": "test.fs-full", "trusted": true})); err != nil {
		t.Fatalf("grant administrator access: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.fs-full", "command": "read"})); err != nil {
		t.Fatalf("read outside path with full disk access: %v", err)
	}
}

func TestStarlarkPluginShellExtensionsRequireAdministratorGate(t *testing.T) {
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.shell",
    "name": "Shell Plugin",
    "version": "0.1.0",
    "permissions": ["shell.pages", "shell.css"],
}

def activate(ctx):
    ctx.shell.register_page({
        "id": "dashboard",
        "title": "Dashboard",
        "blocks": [{"type": "text", "title": "Hello", "body": "World"}],
    })
    ctx.shell.add_css(".settings-group { border-color: red; }")
`,
	})
	defer r.Close()

	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.shell"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	result, err := r.Dispatch("plugins.extensions.list", nil)
	if err != nil {
		t.Fatalf("list extensions: %v", err)
	}
	extensions := result.(pluginExtensions)
	if len(extensions.Pages) != 0 {
		t.Fatalf("unexpected pages without administrator access: %#v", extensions.Pages)
	}
	if _, err := r.Dispatch("plugins.disable", mustJSON(t, map[string]any{"id": "test.shell"})); err != nil {
		t.Fatalf("disable plugin: %v", err)
	}
	if _, err := r.Dispatch("plugins.administrator.update", mustJSON(t, map[string]any{"id": "test.shell", "trusted": true})); err != nil {
		t.Fatalf("grant administrator access: %v", err)
	}
	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.shell"})); err != nil {
		t.Fatalf("enable trusted plugin: %v", err)
	}
	result, err = r.Dispatch("plugins.extensions.list", nil)
	if err != nil {
		t.Fatalf("list trusted extensions: %v", err)
	}
	extensions = result.(pluginExtensions)
	if len(extensions.Pages) != 1 || extensions.Pages[0].ID != "test.shell:dashboard" {
		t.Fatalf("unexpected trusted pages: %#v", extensions.Pages)
	}
}

func TestStarlarkPluginCommandSpecMayContainFunction(t *testing.T) {
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.function-spec",
    "name": "Function Spec",
    "version": "0.1.0",
    "permissions": ["commands.register"],
}

def activate(ctx):
    spec = {"id": "ping", "title": "Ping", "run": ping}
    ctx.commands.register(spec)

def ping(ctx):
    return {"ok": True}
`,
	})
	defer r.Close()

	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.function-spec"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if _, err := r.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.function-spec", "command": "ping"})); err != nil {
		t.Fatalf("run plugin command: %v", err)
	}
}

func TestStarlarkPluginScheduleRunsHandler(t *testing.T) {
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.schedule",
    "name": "Schedule Plugin",
    "version": "0.1.0",
    "permissions": ["storage.write", "storage.read"],
    "schedules": [{"id": "heartbeat", "every": "10ms", "handler": "heartbeat"}],
}

def heartbeat(ctx):
    ctx.storage.set("last_heartbeat", ctx.time.now_ms())
`,
	})
	defer r.Close()

	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.schedule"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		db, err := r.Store()
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		if _, ok, err := db.Collection(pluginStorageCollection).Get("test.schedule:last_heartbeat"); err != nil {
			t.Fatalf("read plugin storage: %v", err)
		} else if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("scheduled heartbeat did not write plugin storage")
}

func TestPluginLogsListReturnsNewestEntries(t *testing.T) {
	r := newTestPluginRegistry(t, map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.logs",
    "name": "Logs Plugin",
    "version": "0.1.0",
}
`,
	})
	defer r.Close()
	for i := 0; i < 60; i++ {
		r.plugins.audit("test.logs", "old", "", true, "", "")
	}
	r.plugins.audit("test.logs", "schedule:heartbeat", "", true, "", "")

	result, err := r.Dispatch("plugins.logs.list", mustJSON(t, map[string]any{"id": "test.logs", "limit": 12}))
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	logs := result.([]pluginAuditRecord)
	if len(logs) == 0 || logs[0].Action != "schedule:heartbeat" {
		t.Fatalf("newest log missing from limited result: %#v", logs)
	}
}

func TestRunProcessLimitsCapturedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell pipeline")
	}
	result, err := runProcess(map[string]any{
		"command":    "yes x | head -c 50000",
		"timeout_ms": 5000,
	}, true)
	if err != nil {
		t.Fatalf("run process: %v", err)
	}
	stdout := result["stdout"].(string)
	if len(stdout) != maxProcessOutputBytes {
		t.Fatalf("stdout was not limited: got %d", len(stdout))
	}
	if !strings.HasPrefix(stdout, "x\n") {
		t.Fatalf("unexpected stdout prefix: %q", stdout[:min(10, len(stdout))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestEnabledPluginStartsAfterRegistryRestart(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"yui.plugin.json": `{"schema":"yui.local-plugin.v0","type":"starlark","entry":"./plugin.star","dev":true}`,
		"plugin.star": `
plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.autostart",
    "name": "Autostart Plugin",
    "version": "0.1.0",
    "permissions": ["commands.register"],
}

def activate(ctx):
    ctx.commands.register({"id": "ping", "title": "Ping", "run": ping})

def ping(ctx):
    return {"ok": True}
`,
	}
	writeTestPluginFiles(t, dir, files)
	cfg := config.Config{ConfigDir: dir, StorePath: filepath.Join(dir, "store.db")}
	r := NewRegistry(cfg)
	if _, err := r.Dispatch("plugins.enable", mustJSON(t, map[string]any{"id": "test.autostart"})); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close registry: %v", err)
	}

	restarted := NewRegistry(cfg)
	defer restarted.Close()
	restarted.StartPlugins(t.Context())
	if _, err := restarted.Dispatch("plugins.run", mustJSON(t, map[string]any{"id": "test.autostart", "command": "ping"})); err != nil {
		t.Fatalf("enabled plugin did not autostart after restart: %v", err)
	}
}

func newTestPluginRegistry(t *testing.T, files map[string]string) *Registry {
	t.Helper()
	dir := t.TempDir()
	writeTestPluginFiles(t, dir, files)
	return NewRegistry(config.Config{ConfigDir: dir, StorePath: filepath.Join(dir, "store.db")})
}

func writeTestPluginFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	pluginDir := filepath.Join(dir, "plugins", "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("create plugin dir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(pluginDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write plugin file: %v", err)
		}
	}
}
