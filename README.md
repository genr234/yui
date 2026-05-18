<div align="center">
  <img src="platform/src/assets/images/logo.png" width="256" height="256">
  <p>A battery included Digikiosk jailbreak</h2>
</div>

## Build

```sh
just deps-all
just build
```

This installs platform and Rails dependencies, builds the Svelte overlay into `controller/static/platform.js`, builds the Rails server container image, embeds the overlay into `controller.exe`, and packages the Windows installer.

The server image is tagged as `yui-server:dev` locally by default. Set `YUI_SERVER_IMAGE` to override it:

```sh
YUI_SERVER_IMAGE=ghcr.io/owner/yui-server:local just build-server
```

CI publishes the Rails server image to GitHub Container Registry as `ghcr.io/<owner>/<repo>-server` with commit SHA, branch, and default-branch `latest` tags.

`just package` remains kiosk-only for the installer release path.

## Development

```sh
just dev
```

This starts the Go controller, Vite overlay, and Rails server together. To run only the Rails app:

```sh
just dev-server
```

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

## Accounts and Sync

Kiosks can run anonymously or pair with a Rails-managed account. Create the account and pairing code in Rails, then unlock the kiosk with the local admin PIN and use the account control at the bottom of the Yui sidebar to connect.

When an account is active, the kiosk opens an account-specific Bolt DB under `accounts/<account id>/yui-store.db`, syncs app/plugin/storage changes to Rails, and listens over a Rails websocket for allowed app/plugin management commands. The admin PIN stays local to the kiosk and is required before connecting, disconnecting, or changing accounts.

Apps and plugins get scoped storage instead of direct access to arbitrary collections:

```ts
const previous = await ctx.storage.get("score");
await ctx.storage.set("score", Number(previous ?? 0) + 1);
const keys = await ctx.storage.keys();
```

Core platform data should be accessed through typed commands such as `apps.*` and `plugins.*`, not through a generic document-store API.

## How to install on a kiosk

Run `dist/installer.exe` from Explorer on the kiosk, select the existing kiosk `chrome.bat`, and the installer preserves it as `chrome.original.bat`, writes the Yui bootstrap, writes `controller.exe`, and starts the controller.
