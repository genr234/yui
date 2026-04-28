# Yui Kiosk Platform

Yui is a Go-based kiosk controller with a Svelte overlay injected into the live kiosk page. The watchdog still launches `chrome.bat`, but the controller owns Chrome, config, logs, status, recovery, and the platform bridge.

## Build

```sh
make deps
make build
```

This builds the Svelte overlay into `controller/static/platform.js`, embeds it into `controller.exe`, and packages the Windows installer.

## macOS Overlay Test

```sh
make dev-inject URL=https://example.com
```

The controller launches local Chrome with remote debugging, opens the URL, and injects the same Svelte overlay path used on the kiosk. Long-press the top-left corner of the page to open Yui.

For Svelte hot reload while still using controller/CDP injection:

```sh
make dev-hot URL=https://example.com
```

This starts Vite on `127.0.0.1:5173`, runs the controller with `YUI_PLATFORM_DEV_SERVER`, and injects the Vite module into the live page.

## Useful Checks

```sh
make check
cd controller && go run . --version
cd controller && go run . --check
```

## Kiosk Install Shape

Run `dist/installer.exe` from Explorer on the kiosk, select the existing kiosk `chrome.bat`, and the installer preserves it as `chrome.original.bat`, writes the Yui bootstrap, writes `controller.exe`, and starts the controller.
