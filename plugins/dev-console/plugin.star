plugin = {
    "schema": "yui.starlark-plugin.v0",
    "id": "test.plugin",
    "name": "test plugin",
    "version": "0.1.0",
    "description": "test test test",
    "permissions": [
        "commands.register",
        "process.run",
        "settings.read",
        "storage.read",
        "storage.write",
        "system.status",
    ],
    "settings": {
        "cwd": {
            "type": "path",
            "label": "Working directory",
            "description": "Folder used when running development commands.",
            "default": ".",
        },
        "timeout_ms": {
            "type": "number",
            "label": "Command timeout",
            "description": "Maximum runtime for each command.",
            "default": 30000,
        },
    },
    "schedules": [
        {"id": "heartbeat", "every": "5m", "handler": "heartbeat"},
    ],
}

def activate(ctx):
    ctx.commands.register({
        "id": "go-test-controller",
        "title": "Run controller tests",
        "subtitle": "go test ./...",
        "run": go_test_controller,
    })

def go_test_controller(ctx):
    started_at = ctx.time.now_ms()
    result = ctx.process.run({
        "exe": "go",
        "args": ["test", "./..."],
        "cwd": ctx.settings.get("cwd"),
        "timeout_ms": ctx.settings.get("timeout_ms"),
    })
    history = ctx.storage.get("runs") or []
    history = [{
        "command": "go test ./...",
        "code": result["code"],
        "duration_ms": ctx.time.now_ms() - started_at,
    }] + history
    ctx.storage.set("runs", history[:20])
    return result

def heartbeat(ctx):
    ctx.storage.set("last_heartbeat", ctx.time.now_ms())
