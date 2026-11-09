# EmulatorJS runtime

Retro Arcade runs games with [EmulatorJS](https://github.com/EmulatorJS/EmulatorJS), served from the kiosk itself. It loads nothing from a CDN.

- **Pinned version:** 4.2.3, from the release archive `4.2.3.7z` (sha256 `07d451bc06fa3ad04ab30d9b94eb63ac34ad0babee52d60357b002bde8f3850b`).
- **Runtime asset:** a zip of the release's `data/` folder trimmed to the cores you want. Upload it in the control room with metadata `runtime: emulatorjs`, `version: 4.2.3`. Kiosks sync it, and the app mounts it at `/_runtime/emulatorjs/`.

```bash
curl -LO https://github.com/EmulatorJS/EmulatorJS/releases/download/v4.2.3/4.2.3.7z
tools/build-runtime.sh 4.2.3.7z emulatorjs-4.2.3.zip              # the default cores
tools/build-runtime.sh 4.2.3.7z emulatorjs-4.2.3.zip fceumm gambatte mgba   # only these
```

A system whose core isn't in the zip fails to start with a plain message. Threaded cores are never used (`EJS_threads = false`).

## Trying it without a kiosk

1. Extract the release, then copy its `data/` folder (or the zip's contents) to `platform/.demo-runtimes/emulatorjs/`.
2. Drop test ROMs into `platform/.demo-assets/`. `python3 tools/make-test-rom.py platform/.demo-assets/yui-input-test.nes` makes one: the screen colour follows port 1's A/B and other buttons, and turns yellow for any port 2 button.
3. Run the overlay demo (`bun run dev -- --mode demo` in `platform/`). Both folders are git-ignored.
