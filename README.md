# Yui Kiosk Platform

Yui is a Go-based kiosk controller with a Svelte overlay injected into the live kiosk page. The watchdog still launches `chrome.bat`, but the controller owns Chrome, config, logs, status, recovery, and the platform bridge.

## Build

```sh
just deps
just build
```

This builds the Svelte overlay into `controller/static/platform.js`, embeds it into `controller.exe`, and packages the Windows installer.

## macOS Overlay Test

```sh
just dev-inject
```

The controller launches local Chrome with remote debugging, opens the URL, and injects the same Svelte overlay path used on the kiosk. Long-press the top-left corner of the page to open Yui.

For Svelte hot reload while still using controller/CDP injection:

```sh
just dev-hot
```

This starts Vite on `127.0.0.1:5173`, runs the controller with `YUI_PLATFORM_DEV_SERVER`, and injects the Vite module into the live page.

## Useful Checks

```sh
just check
cd controller && go run . --version
cd controller && go run . --check
```

## Platform Storage

The controller keeps durable local state in a Bolt-backed store at `store_path` in `controller.json`.
That store is an implementation detail for platform records such as app catalogs, plugin catalogs, installed items, settings, and audit data.

Apps and plugins get scoped storage instead of direct access to arbitrary collections:

```ts
const previous = await ctx.storage.get("score");
await ctx.storage.set("score", Number(previous ?? 0) + 1);
const keys = await ctx.storage.keys();
```

Core platform data should be accessed through typed commands such as `apps.*` and `plugins.*`, not through a generic document-store API.

## Kiosk Install Shape

Run `dist/installer.exe` from Explorer on the kiosk, select the existing kiosk `chrome.bat`, and the installer preserves it as `chrome.original.bat`, writes the Yui bootstrap, writes `controller.exe`, and starts the controller.
