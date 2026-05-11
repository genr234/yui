package handlers

import (
	"os"
	"path/filepath"
	"testing"

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

func newTestPluginRegistry(t *testing.T, files map[string]string) *Registry {
	t.Helper()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("create plugin dir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(pluginDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write plugin file: %v", err)
		}
	}
	return NewRegistry(config.Config{ConfigDir: dir, StorePath: filepath.Join(dir, "store.db")})
}
