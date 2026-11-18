# Wii U Arcade runtimes

Wii U Arcade runs Nintendo Land (and other Wii U games) in Cemu. Yui does not bundle, download or
redistribute Cemu, ffmpeg, any key or any game. The account owner uploads everything from files
they obtained themselves, through the asset library.

## Pinned versions

| What | Runtime metadata | Used for |
|---|---|---|
| Cemu 2.6 (Windows x64) | `runtime: cemu`, `version: 2.6` | the emulator |
| ffmpeg 8.0 (Windows x64, with `gfxcapture` and an H.264 encoder) | `runtime: ffmpeg`, `version: 8.0` | capture and encode the GamePad window for the phone |

The app prefers the pinned versions and falls back to the newest uploaded one.

## Cemu runtime zip

`tools/build-runtime.sh` repackages the official `cemu-2.6-windows-x64.zip` into a flat zip: no
versioned top folder, `Cemu.exe` at the root, plus an empty `portable/` folder (Cemu keeps its
settings, controller profiles, saves and shader caches there when the folder exists).

    tools/build-runtime.sh /path/to/cemu-2.6-windows-x64.zip out/cemu-2.6.zip

Upload `out/cemu-2.6.zip` as an asset of kind `runtime` with metadata `runtime: cemu`,
`version: 2.6`. The app launches Cemu with `install: "private"`, so each account gets its own copy
under its data directory and nothing is written to the Windows user's AppData.

## ffmpeg runtime zip

Use an ffmpeg 8.0 Windows x64 build that includes the `gfxcapture` source filter and at least
`libx264` (hardware encoders `h264_nvenc`, `h264_qsv`, `h264_amf`, `h264_mf` are used when
present). Zip it with `ffmpeg.exe` at the root or under `bin/`, and upload it as a `runtime` asset
with metadata `runtime: ffmpeg`, `version: 8.0`.

## Games: make a WUA from your own dump

Only `.wua` files are played. A WUA is one decrypted file holding the base game, its update and
DLC, so neither the kiosk nor the server ever holds a key.

1. Dump the game from a Wii U you own, and install it in Cemu on your own computer.
2. In Cemu, open File > "Title Manager" (or the Title Manager tab), select the title and choose
   "Convert to compressed Wii U archive (.wua)". Check Cemu's own documentation for the current menu names.
3. Upload the `.wua` to your Yui library as a game.

Uploaded `.wud` and `.wux` images are recognised and listed as "convert to WUA", not played.
Nothing here is downloaded by Yui. Only use files you are legally entitled to use.

## Spikes that still need a Windows PC with a GPU

- **S1: capture a hidden Cemu window.** Run Cemu with the GamePad View open, move the window
  off-screen, then `ffmpeg -f lavfi -i gfxcapture=hwnd=<HWND>:max_framerate=60 -t 10 out.mp4` and
  check that the file moves. Confirm the option names with `ffmpeg -h filter=gfxcapture`. Record
  which hide mode works.
- **S2: reference config.** In Cemu 2.6, bind controller 1 as Wii U GamePad and controllers 2 to 4
  as Wiimotes to a DSU source (`controller/cmd/dsu-demo --touch`), save, and copy `settings.xml`
  and `controllerProfiles/*.xml` into `golden/reference/`. Compare with `golden/` and replace every
  value marked `UNVERIFIED until S2` in `cemu-config.js` (language id, console language id, DSU axis
  sign, MotionPlus `device_type`, empty `PadDevice`). Confirm the pad window's exact title, and
  that DSU touch drives the GamePad touch and the Wii Remote pointer. Note any dialog a clean
  `portable/` shows.
- **S3: latency.** `controller/cmd/surface-demo` streams a window showing a millisecond clock to a
  phone; film both and read p50 and p95. Test iPhone Safari and Android Chrome, direct and relayed.
