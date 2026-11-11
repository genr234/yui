export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.wii-arcade",
  name: "Wii Arcade",
  version: "0.1.0",
  icon: "🎮",
  category: "Games",
  description: "Wii and GameCube games from your library. Every phone is a motion remote.",
  permissions: ["controllers", "virtual-pads", "assets:game", "assets:runtime", "native", "audio.speaker", "library"],

  // Declaring controllers gives this app the phones while it is open.
  controllers: {},

  mount(ctx) {
    const pad = ctx.controllers;
    const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
    const errorText = (error) => String((error && error.message) || error || "Something went wrong").replace(/^YUI_[A-Z_]+:\s*/, "");

    const state = ctx.state({
      loaded: false,
      games: [],
      pending: 0,
      runtime: null,
      phase: "lobby", // lobby | starting | playing | stopping
      current: null, // the game being played
      history: {}, // asset id → { saved }
      fresh: false, // ignore saved states when starting a game
      error: "",
      slots: {}, // player id → Wii Remote slot (0-3)
    });

    // Player pads by slot, as the platform reports them from attach().
    const padAddresses = [];
    let systemPadAddress = null;
    // A game opened from Jump back in waits here until a host phone is in.
    let pendingStart = null;
    const launchItem = ctx.launch.item;

    // ---- assets ----------------------------------------------------------

    let watching = false;

    async function refresh() {
      try {
        const [games, runtimes] = await Promise.all([ctx.assets("game"), ctx.assets("runtime")]);
        const picked = selectGames(games);
        state.games = picked.ready;
        state.pending = picked.pending;
        state.runtime = selectRuntime(runtimes);
        if (state.error.startsWith("Could not read")) state.error = "";
        // Subscribe once the permission is granted (the first list asks for it).
        if (!watching) {
          watching = true;
          ctx.assets.onChange(() => refresh());
        }
      } catch (error) {
        state.error = `Could not read the asset library: ${errorText(error)}`;
      }
      state.loaded = true;
      maybeStartPending();
    }

    async function loadHistory() {
      try {
        const titles = await ctx.library.list();
        const history = {};
        for (const title of titles) {
          const assetId = assetIdFromLibraryItem(title);
          if (assetId) history[assetId] = { saved: Boolean(title.resume && title.resume.saved) };
        }
        state.history = history;
      } catch (error) {
        ctx.log.warn("library unavailable", errorText(error));
      }
    }

    // ---- helpers ---------------------------------------------------------

    const gameById = (id) => state.games.find((game) => game.id === String(id)) || null;
    const hostPlayer = () => pad.players.find((player) => player.host && player.connected) || null;

    async function tapSystem(button) {
      try {
        await ctx.virtualPads.press(button, "tap");
      } catch (error) {
        ctx.log.warn("system pad press failed", button, errorText(error));
      }
    }

    function markPlayed(game, saved) {
      const entry = { id: libraryIdFor(game.id), title: game.title, players: 4, resume: { assetId: game.id, saved } };
      if (game.cover) entry.art = game.id;
      ctx.library
        .publish([entry])
        .then(() => ctx.library.played(entry.id, { resume: entry.resume }))
        .catch((error) => ctx.log.warn("library update failed", errorText(error)));
      state.history = { ...state.history, [game.id]: { saved } };
    }

    function freeSlot() {
      const used = new Set(Object.values(state.slots));
      for (let slot = 0; slot < PLAYER_SLOTS; slot++) if (!used.has(slot)) return slot;
      return -1;
    }

    async function attachPlayer(player, slot) {
      const attachment = await ctx.virtualPads.attach(player, { slot });
      padAddresses[slot] = attachment.address;
      state.slots = { ...state.slots, [player.id]: slot };
    }

    async function detachPlayer(player) {
      const slots = { ...state.slots };
      delete slots[player.id];
      state.slots = slots;
      try {
        await ctx.virtualPads.detach(player);
      } catch (error) {
        ctx.log.warn("detach failed", errorText(error));
      }
    }

    // ---- starting and stopping a game -------------------------------------

    // Each start (and each cancel) bumps the attempt, so a launch that comes
    // back late, or a status event from an earlier program, cannot move a
    // newer attempt along. `nativeSession` is the program this app is
    // following; status events from any other session are ignored.
    let attempt = 0;
    let nativeSession = null;

    async function start(game, options) {
      if (state.phase !== "lobby") return;
      if (!state.runtime) {
        state.error = "The Dolphin runtime is not uploaded yet.";
        return;
      }
      const saved = Boolean(options && options.saved) && Boolean(game.gameId);
      const mine = ++attempt;
      nativeSession = null;
      state.phase = "starting";
      state.current = game;
      state.error = "";
      pendingStart = null;
      try {
        // The host is Wii Remote 1; everyone else follows in join order.
        const players = pad.players
          .filter((player) => player.connected)
          .sort((a, b) => Number(b.host) - Number(a.host))
          .slice(0, PLAYER_SLOTS);
        padAddresses.length = 0;
        state.slots = {};
        for (let slot = 0; slot < players.length; slot++) {
          await attachPlayer(players[slot], slot);
          if (mine !== attempt) return;
        }
        systemPadAddress = (await ctx.virtualPads.systemPad()).address;
        if (mine !== attempt) return;
        const spec = dolphinSpec({
          runtime: state.runtime.id,
          gameAsset: game.id,
          stateFor: saved ? game.gameId : undefined,
          pads: padAddresses,
          systemPad: systemPadAddress,
        });
        const status = await ctx.native.launch(spec);
        if (mine !== attempt) {
          // Cancelled while the launch was on its way: stop what it started.
          if (status.state === "running" || status.state === "preparing") ctx.native.stop({ force: true }).catch(() => {});
          return;
        }
        if (status.state === "busy") throw new Error("Another app is running a program right now.");
        if (status.state === "failed" || status.state === "crashed") throw new Error(status.error || "The emulator did not start.");
        if (status.state === "exited") throw new Error("The emulator closed right away.");
        if (status.session_id) nativeSession = status.session_id;
        if (state.phase !== "starting") return; // a status event already moved on
        state.phase = "playing";
        markPlayed(game, Boolean(state.history[game.id] && state.history[game.id].saved));
      } catch (error) {
        if (mine !== attempt) return;
        attempt++;
        nativeSession = null;
        await resetPlayers();
        state.phase = "lobby";
        state.current = null;
        state.error = errorText(error);
      }
    }

    // The host gave up waiting. Whatever the launch got to is stopped; a
    // program still being prepared never starts.
    async function cancelStart() {
      if (state.phase !== "starting") return;
      attempt++;
      nativeSession = null;
      state.phase = "stopping";
      try {
        await ctx.native.stop({ force: true });
      } catch (error) {
        ctx.log.warn("cancel: stop failed", errorText(error));
      }
      await resetPlayers();
      state.phase = "lobby";
      state.current = null;
    }

    async function resetPlayers() {
      const ids = Object.keys(state.slots);
      state.slots = {};
      for (const id of ids) {
        try {
          await ctx.virtualPads.detach(id);
        } catch (error) {
          ctx.log.warn("detach failed", errorText(error));
        }
      }
    }

    // The program ended (a quit, a crash, or the platform's own watchdog).
    async function ended(status) {
      if (state.phase === "lobby") return;
      attempt++;
      nativeSession = null;
      await resetPlayers();
      state.phase = "lobby";
      state.current = null;
      if (status && status.state === "crashed") state.error = status.error || "The emulator stopped unexpectedly.";
      else if (status && status.state === "failed") state.error = status.error || "The emulator did not start.";
    }

    ctx.native.on("status", (status) => {
      const id = status.session_id || null;
      if (status.state === "preparing" || status.state === "running") {
        // Our launch's program (it is the only one this app starts). Following
        // it here keeps the phase right even when launch()'s reply is late.
        if (state.phase === "starting" || state.phase === "playing") nativeSession = id;
        if (status.state === "running" && state.phase === "starting" && id) state.phase = "playing";
        return;
      }
      if (status.state !== "exited" && status.state !== "crashed" && status.state !== "failed") return;
      // An earlier program's end (a late event, or a replay after a reconnect)
      // must not end the game that is starting or running now.
      if (state.phase === "starting" && nativeSession === null) return;
      if (nativeSession !== null && id !== null && id !== nativeSession) return;
      ended(status);
    });

    // Save a state, give the emulator a moment to write it, then stop.
    async function quit() {
      if (state.phase !== "playing") return;
      const game = state.current;
      state.phase = "stopping";
      await tapSystem(SYSTEM_BUTTONS.saveState);
      if (game) markPlayed(game, true);
      await sleep(1500);
      try {
        await ctx.native.stop();
      } catch (error) {
        ctx.log.warn("graceful stop failed, forcing", errorText(error));
        try {
          await ctx.native.stop({ force: true });
        } catch (forced) {
          state.error = errorText(forced);
        }
      }
      await ended(null);
    }

    function maybeStartPending() {
      if (!pendingStart || state.phase !== "lobby" || !state.loaded || !state.runtime) return;
      if (!hostPlayer()) return;
      const game = gameById(pendingStart.assetId);
      if (!game) {
        if (state.loaded && state.pending === 0) {
          pendingStart = null;
          state.error = "That game is not in the library anymore.";
        }
        return;
      }
      start(game, { saved: pendingStart.saved });
    }

    // Opened from Jump back in: resume that game.
    if (launchItem) {
      const assetId = assetIdFromLibraryItem(launchItem);
      if (assetId) pendingStart = { assetId, saved: Boolean(launchItem.resume && launchItem.resume.saved) };
    }

    // ---- players ----------------------------------------------------------

    // A phone that joins while a game runs gets the next free Wii Remote.
    async function joinMidGame(player) {
      if (state.phase !== "playing" || state.slots[player.id] !== undefined) return;
      const slot = freeSlot();
      if (slot < 0) return;
      try {
        await attachPlayer(player, slot);
        await sleep(250);
        await tapSystem(SYSTEM_BUTTONS.connect[slot]);
      } catch (error) {
        ctx.log.warn("could not attach a late player", errorText(error));
        const slots = { ...state.slots };
        delete slots[player.id];
        state.slots = slots;
      }
    }

    pad.on("join", (player) => joinMidGame(player));
    pad.on("leave", (player) => {
      if (state.slots[player.id] !== undefined) detachPlayer(player);
    });
    pad.on("players", () => {
      maybeStartPending();
      if (state.phase === "playing") for (const player of pad.players) if (player.connected) joinMidGame(player);
    });

    // ---- the phone's Home sheet --------------------------------------------

    pad.on("system", async ({ player, action }) => {
      if (state.phase !== "playing") return;
      switch (action) {
        case "resume":
          break;
        case "save_state":
          if (!player.host) return;
          await tapSystem(SYSTEM_BUTTONS.saveState);
          if (state.current) markPlayed(state.current, true);
          ctx.toast("State saved").catch(() => {});
          break;
        case "load_state":
          if (!player.host) return;
          await tapSystem(SYSTEM_BUTTONS.loadState);
          break;
        case "quit":
          if (player.host) await quit();
          else if (state.slots[player.id] !== undefined) await detachPlayer(player);
          break;
      }
    });

    // ---- phone layouts -------------------------------------------------------

    function gameItems() {
      return state.games.map((game) => {
        const saved = !state.fresh && state.history[game.id] && state.history[game.id].saved;
        const item = {
          id: game.id,
          title: game.title,
          subtitle: saved ? `${game.platformLabel} · continue` : game.platformLabel,
        };
        if (game.cover) item.image = game.cover;
        return item;
      });
    }

    pad.layout((player) => {
      const ui = pad.ui;
      if (state.phase === "playing") {
        if (state.slots[player.id] === undefined) {
          return ui.column({ gap: 12 }, [ui.text("Watching", { size: "large", align: "center" }), ui.text("Tap Home to see who is playing.", { align: "center" })]);
        }
        return ui.preset("motion-remote", { labels: MOTION_REMOTE_LABELS });
      }
      if (state.phase === "starting") {
        return ui.column({ gap: 12 }, [
          ui.text("Starting…", { size: "large", align: "center" }),
          player.host ? ui.button({ id: "cancel", label: "Cancel", variant: "ghost" }) : null,
        ]);
      }
      if (state.phase === "stopping") {
        return ui.column({ gap: 12 }, [ui.text("Saving and closing…", { size: "large", align: "center" })]);
      }
      if (!player.host) {
        return ui.column({ gap: 12 }, [ui.text("Wii Arcade", { size: "large", align: "center" }), ui.text("The host is choosing a game.", { align: "center" })]);
      }
      if (!state.loaded) return ui.text("Looking for games…", { align: "center" });
      if (!state.runtime) {
        return ui.column({ gap: 12 }, [ui.text("Not ready yet", { size: "large", align: "center" }), ui.text("The emulator has not been uploaded to this kiosk's library.", { align: "center" })]);
      }
      if (!state.games.length) {
        return ui.column({ gap: 12 }, [ui.text("No games yet", { size: "large", align: "center" }), ui.text(state.pending ? `${state.pending} still downloading.` : "Upload Wii or GameCube games to your library.", { align: "center" })]);
      }
      return ui.column({ gap: 12 }, [
        ui.text("Pick a game", { size: "large", align: "center" }),
        state.error ? ui.text(state.error, { size: "small", align: "center" }) : null,
        ui.list({ id: "games", columns: 2, items: gameItems() }),
        ui.button({ id: "fresh", label: state.fresh ? "Starting fresh: on" : "Starting fresh: off", variant: "ghost" }),
      ]);
    });

    pad.on("event", ({ player, control, kind, value }) => {
      if (!player.host) return;
      if (state.phase === "starting" && control === "cancel" && kind === "tap") {
        cancelStart();
        return;
      }
      if (state.phase !== "lobby") return;
      if (control === "fresh" && kind === "tap") {
        state.fresh = !state.fresh;
      } else if (control === "games" && kind === "tap") {
        const game = gameById(value);
        if (!game) return;
        const saved = !state.fresh && Boolean(state.history[game.id] && state.history[game.id].saved);
        start(game, { saved });
      }
    });

    // ---- the screen ------------------------------------------------------------

    function playerRows() {
      const players = pad.players;
      if (!players.length) return [ctx.ui.small("Nobody has joined yet.")];
      return players.map((player) => {
        const slot = state.slots[player.id];
        const label = state.phase === "lobby" || state.phase === "starting" ? (player.host ? "Host" : "Joined") : slot === undefined ? "Watching" : `Wii Remote ${slot + 1}`;
        return ctx.ui.row({ gap: 10, align: "center" }, [ctx.ui.text(player.name || "Player"), ctx.ui.small(`${label}${player.connected ? "" : " · reconnecting"}`)]);
      });
    }

    function gameCard(game) {
      const saved = !state.fresh && state.history[game.id] && state.history[game.id].saved;
      return ctx.ui.column({ gap: 6, width: 150 }, [
        game.cover ? ctx.ui.image({ src: game.cover, alt: game.title, width: 150, height: 150 }) : ctx.ui.card({ padding: 14, height: 150 }, [ctx.ui.small(game.platformLabel)]),
        ctx.ui.text(game.title),
        ctx.ui.small(saved ? `${game.platformLabel} · continue` : game.platformLabel),
        ctx.ui.button({ label: "Play", onClick: () => start(game, { saved: Boolean(saved) }) }),
      ]);
    }

    function emptyState() {
      if (!state.loaded) return ctx.ui.p("Looking for games…");
      const lines = [];
      if (!state.runtime) {
        lines.push(ctx.ui.empty("No emulator yet. Upload a Dolphin runtime (name dolphin, version " + DOLPHIN_RELEASE + ") to this account's library and it syncs here."));
      }
      if (!state.games.length) {
        lines.push(
          ctx.ui.empty(
            state.pending
              ? `${state.pending} game${state.pending === 1 ? " is" : "s are"} still downloading.`
              : "No Wii or GameCube games yet. Upload game files to this account's library and they appear here.",
          ),
        );
      }
      return lines.length ? ctx.ui.column({ gap: 8 }, lines) : null;
    }

    refresh();
    loadHistory();
    // A program this app started before a reload keeps running.
    ctx.native
      .status()
      .then((status) => {
        if (state.phase !== "lobby") return;
        // A program this app started before a reload: follow it.
        if (status.state === "running" || status.state === "preparing") {
          nativeSession = status.session_id || null;
          state.phase = status.state === "running" ? "playing" : "starting";
        }
      })
      .catch(() => {});

    // Wake lock while a game is starting, running or closing: a cutscene or a
    // pause can outlast the kiosk's idle timeouts with nobody touching it, and
    // no screensaver, sleep or idle tool may cover the game. Closing the app
    // releases it too.
    let wakeLock = null;
    let wakePending = false;
    async function keepAwake(wanted) {
      if (!ctx.wakeLock || wakePending) return;
      if (wanted === Boolean(wakeLock && !wakeLock.released)) return;
      wakePending = true;
      try {
        if (wanted) wakeLock = await ctx.wakeLock.request("Wii game running");
        else if (wakeLock) await wakeLock.release();
      } catch (error) {
        ctx.log.warn("wake lock", error);
      } finally {
        wakePending = false;
      }
      // The phase may have changed while the request was in flight.
      const now = state.phase !== "lobby";
      if (now !== Boolean(wakeLock && !wakeLock.released)) void keepAwake(now);
    }

    return () => {
      void keepAwake(state.phase !== "lobby");
      return (
        ctx.ui.column({ gap: 16, padding: 18 }, [
          ctx.ui.h1("Wii Arcade"),
          state.error ? ctx.ui.p(state.error) : null,
          ctx.ui.when(
            state.phase === "lobby",
            ctx.ui.column({ gap: 16 }, [
              ctx.ui.row({ gap: 24, align: "start", wrap: true }, [
                ctx.ui.column({ gap: 8 }, [ctx.ui.h3("Scan to play"), pad.joinCard({ size: 140 })]),
                ctx.ui.column({ gap: 8 }, [ctx.ui.h3("Players"), ...playerRows()]),
              ]),
              emptyState(),
              state.games.length ? ctx.ui.column({ gap: 10 }, [ctx.ui.h3("Games"), ctx.ui.p("The host can also pick from their phone."), ctx.ui.row({ gap: 14, wrap: true }, state.games.map(gameCard))]) : null,
            ]),
          ),
          ctx.ui.when(
            state.phase !== "lobby",
            ctx.ui.column({ gap: 10 }, [
              ctx.ui.h2(state.phase === "starting" ? "Starting…" : state.phase === "stopping" ? "Saving and closing…" : `Now playing: ${state.current ? state.current.title : "a game"}`),
              ...playerRows(),
              state.phase === "starting"
                ? ctx.ui.button({ label: "Cancel", variant: "ghost", onClick: () => cancelStart() })
                : ctx.ui.small("Press Home on a phone for save, load and quit."),
            ]),
          ),
        ])
      );
    };
  },
};

// <generated:dolphin-config>
// Pure helpers for Wii Arcade: Dolphin's user directory, command line, game
// and runtime selection. No ctx, no DOM. app.yui.js embeds this file between
// the "generated" markers (run `bun run sync`; a test fails when they drift),
// because an app is a single script.
//
// Config key names were checked against dolphin-emu/dolphin tag 2609:
//   Core/Config/MainSettings.cpp            Dolphin.ini sections and keys
//   Core/Config/WiimoteSettings.cpp         [WiimoteN] Source (0 none, 1 emulated)
//   Core/HW/WiimoteEmu/WiimoteEmu(.h).cpp   group and control names
//   InputCommon/ControllerEmu/ControlGroup  IMUAccelerometer, IMUGyroscope, IMUCursor
//   InputCommon/.../DualShockUDPClient.cpp  device name, input names, Entries format
//   Core/HotkeyManager.cpp                  hotkey groups and labels
//   Common/CommonPaths.h                    file names (DSUClient.ini, WiimoteNew.ini)
//   UICommon/CommandLineParse.cpp           -u, -b, -e, -s
//   Core/Config/MainSettings.cpp            [Display] Fullscreen, RenderToMain, RenderWindowAutoSize,
//                                           RenderWindowXPos/YPos/Width/Height

const DOLPHIN_RELEASE = "2609";
const DOLPHIN_RUNTIME_NAME = "dolphin";
// Inside the official Windows archive (dolphin-<release>-x64.7z).
const DOLPHIN_EXECUTABLE = "Dolphin-x64/Dolphin.exe";
// Relative to {{dataDir}}; passed to Dolphin with -u.
const DOLPHIN_USER_DIR = "User";
const DEFAULT_PLAYER_PADS = "127.0.0.1:26760";
const DEFAULT_SYSTEM_PAD = "127.0.0.1:26761/0";
const PLAYER_SLOTS = 4;
// 16:9 on the 1080x1920 kiosk, centred; same rect the controller computes.
const WINDOW_ASPECT = "16:9";
const DEFAULT_RENDER_WINDOW = { x: 0, y: 656, w: 1080, h: 608 };

// motion-remote preset labels: generic button name -> what the phone shows.
const MOTION_REMOTE_LABELS = {
	South: "A",
	R2: "B",
	West: "1",
	North: "2",
	Select: "−",
	Start: "+",
	L2: "Z",
	L1: "C",
};

// Generic pad button (contracts section 3) -> the DSU input Dolphin sees
// (names from DualShockUDPClient.cpp; Select is the DS4 Share button).
const DSU_INPUT = {
	South: "Cross",
	East: "Circle",
	West: "Square",
	North: "Triangle",
	L1: "L1",
	R1: "R1",
	L2: "L2",
	R2: "R2",
	Select: "Share",
	Start: "Options",
	L3: "L3",
	R3: "R3",
	Up: "Pad N",
	Down: "Pad S",
	Left: "Pad W",
	Right: "Pad E",
	Recenter: "Touch Button",
};

// What each system pad button does. The app presses the generic name with
// ctx.virtualPads.press; Hotkeys.ini binds the matching DSU input.
const SYSTEM_BUTTONS = {
	connect: ["Up", "Right", "Down", "Left"],
	saveState: "South",
	loadState: "East",
	pause: "North",
	stop: "Select",
	exit: "Start",
};

const WIIMOTE_BUTTONS = [
	["Buttons/A", "South"],
	["Buttons/B", "R2"],
	["Buttons/1", "West"],
	["Buttons/2", "North"],
	["Buttons/-", "Select"],
	["Buttons/+", "Start"],
	["D-Pad/Up", "Up"],
	["D-Pad/Down", "Down"],
	["D-Pad/Left", "Left"],
	["D-Pad/Right", "Right"],
];

// Motion input: DSU exposes the phone's accelerometer and gyro as these
// inputs; the Wii Remote's IMU groups take them one for one.
const WIIMOTE_MOTION = [
	["IMUAccelerometer/Up", "Accel Up"],
	["IMUAccelerometer/Down", "Accel Down"],
	["IMUAccelerometer/Left", "Accel Left"],
	["IMUAccelerometer/Right", "Accel Right"],
	["IMUAccelerometer/Forward", "Accel Forward"],
	["IMUAccelerometer/Backward", "Accel Backward"],
	["IMUGyroscope/Pitch Up", "Gyro Pitch Up"],
	["IMUGyroscope/Pitch Down", "Gyro Pitch Down"],
	["IMUGyroscope/Roll Left", "Gyro Roll Left"],
	["IMUGyroscope/Roll Right", "Gyro Roll Right"],
	["IMUGyroscope/Yaw Left", "Gyro Yaw Left"],
	["IMUGyroscope/Yaw Right", "Gyro Yaw Right"],
];

const NUNCHUK_CONTROLS = [
	["Nunchuk/Buttons/C", "L1"],
	["Nunchuk/Buttons/Z", "L2"],
	["Nunchuk/Stick/Up", "Left Y+"],
	["Nunchuk/Stick/Down", "Left Y-"],
	["Nunchuk/Stick/Left", "Left X-"],
	["Nunchuk/Stick/Right", "Left X+"],
];

/** `host:port/slot` -> { host, port, slot }, or null when it is not one. */
function splitPadAddress(address) {
	const match = /^([^:/\s;]+):(\d{1,5})\/(\d+)$/.exec(String(address ?? ""));
	if (!match) return null;
	const port = Number(match[2]);
	if (port < 1 || port > 65535) return null;
	return { host: match[1], port, slot: Number(match[3]) };
}

function pad(value, fallback) {
	const parsed = splitPadAddress(value);
	if (!parsed) throw new Error(`bad virtual pad address: ${String(value)} (${fallback})`);
	return parsed;
}

/**
 * Player pad addresses for slots 0-3. Slots nobody is attached to take the
 * host and port of the first given address (the platform serves all four
 * slots from one server), else the platform's default.
 */
function playerPads(pads) {
	const firstGiven = (pads || []).find(Boolean);
	const base = firstGiven ? pad(firstGiven, "first pad") : null;
	const out = [];
	for (let slot = 0; slot < PLAYER_SLOTS; slot++) {
		const given = pads?.[slot];
		if (given) out.push(pad(given, `slot ${slot}`));
		else if (base) out.push({ host: base.host, port: base.port, slot });
		else out.push(pad(`${DEFAULT_PLAYER_PADS}/${slot}`, "default"));
	}
	return out;
}

/** INI text: sections and keys keep the order given. */
function ini(sections) {
	const blocks = [];
	for (const [name, entries] of sections) {
		const lines = [`[${name}]`];
		for (const [key, value] of entries) lines.push(`${key} = ${value}`);
		blocks.push(lines.join("\n"));
	}
	return `${blocks.join("\n\n")}\n`;
}

const ticks = (name) => `\`${name}\``;
const bool = (value) => (value ? "True" : "False");

/**
 * The DSU servers Dolphin should read: one entry per distinct host:port, in
 * first-use order, named "Yui 1", "Yui 2"... (the name is part of the device
 * name, so it must not contain ":" or ";").
 */
function dsuServers(pads, systemPad) {
	const servers = [];
	const find = (address) => {
		let server = servers.find((s) => s.host === address.host && s.port === address.port);
		if (!server) {
			server = { host: address.host, port: address.port, description: `Yui ${servers.length + 1}` };
			servers.push(server);
		}
		return server;
	};
	const players = pads.map((address) => ({ slot: address.slot, server: find(address) }));
	const system = { slot: systemPad.slot, server: find(systemPad) };
	return { servers, players, system };
}

const deviceName = (entry) => `DSUClient/${entry.slot}/${entry.server.description}`;

/**
 * Files for ctx.native.launch (paths relative to {{dataDir}}).
 * options.window: {x, y, w, h} of Dolphin's render window (default: 16:9
 * centred on the 1080x1920 kiosk); options.pads: addresses ("host:port/slot") of the player pads by slot;
 * options.systemPad: the system pad's address; options.extension: the
 * Wii Remote extension, "None" or "Nunchuk".
 */
function dolphinFiles(options = {}) {
	const pads = playerPads(options.pads);
	const systemPad = pad(options.systemPad ?? DEFAULT_SYSTEM_PAD, "system pad");
	const extension = options.extension === "Nunchuk" ? "Nunchuk" : "None";
	const { servers, players, system } = dsuServers(pads, systemPad);
	const dir = `${DOLPHIN_USER_DIR}/Config`;
	const renderWindow = { ...DEFAULT_RENDER_WINDOW, ...options.window };

	const dolphin = ini([
		[
			"Core",
			[
				["WiimoteContinuousScanning", bool(false)],
				["WiimoteControllerInterface", bool(false)],
			],
		],
		[
			"Display",
			[
				// Windowed: the controller places the render window (ctx.native
				// `window`); these are Dolphin's own starting size and position.
				["Fullscreen", bool(false)],
				["RenderToMain", bool(false)],
				["RenderWindowAutoSize", bool(false)],
				["RenderWindowXPos", String(renderWindow.x)],
				["RenderWindowYPos", String(renderWindow.y)],
				["RenderWindowWidth", String(renderWindow.w)],
				["RenderWindowHeight", String(renderWindow.h)],
			],
		],
		[
			"Interface",
			[
				["ConfirmStop", bool(false)],
				["UsePanicHandlers", bool(false)],
				["SkipNKitWarning", bool(true)],
			],
		],
		// Hotkeys.ini is read from the system pad; by default Dolphin only acts on
		// hotkeys while its render window has focus (UISettings.cpp, "General").
		["General", [["HotkeysRequireFocus", bool(false)]]],
		["Input", [["BackgroundInput", bool(true)]]],
		[
			"Analytics",
			[
				["Enabled", bool(false)],
				["PermissionAsked", bool(true)],
			],
		],
	]);

	const dsu = ini([
		[
			"Server",
			[
				["Enabled", bool(true)],
				["Entries", servers.map((s) => `${s.description}:${s.host}:${s.port};`).join("")],
			],
		],
	]);

	const wiimotes = ini(
		players.map((entry, index) => {
			const device = deviceName(entry);
			const rows = [
				["Device", device],
				["Source", "1"],
				...WIIMOTE_BUTTONS.map(([key, button]) => [key, ticks(DSU_INPUT[button])]),
				...WIIMOTE_MOTION.map(([key, input]) => [key, ticks(input)]),
				["IMUIR/Recenter", ticks(DSU_INPUT.Recenter)],
				["Extension", extension],
				...NUNCHUK_CONTROLS.map(([key, input]) => [key, ticks(input)]),
			];
			return [`Wiimote${index + 1}`, rows];
		}),
	);

	const hotkeys = ini([
		[
			"Hotkeys",
			[
				["Device", deviceName(system)],
				...SYSTEM_BUTTONS.connect.map((button, index) => [
					`Wii/Connect Wii Remote ${index + 1}`,
					ticks(DSU_INPUT[button]),
				]),
				["General/Toggle Pause", ticks(DSU_INPUT[SYSTEM_BUTTONS.pause])],
				["General/Stop", ticks(DSU_INPUT[SYSTEM_BUTTONS.stop])],
				["General/Exit", ticks(DSU_INPUT[SYSTEM_BUTTONS.exit])],
				["Load State/Load State Slot 1", ticks(DSU_INPUT[SYSTEM_BUTTONS.loadState])],
				["Save State/Save State Slot 1", ticks(DSU_INPUT[SYSTEM_BUTTONS.saveState])],
			],
		],
	]);

	return [
		{ path: `${dir}/Dolphin.ini`, content: dolphin },
		{ path: `${dir}/DSUClient.ini`, content: dsu },
		{ path: `${dir}/WiimoteNew.ini`, content: wiimotes },
		{ path: `${dir}/Hotkeys.ini`, content: hotkeys },
	];
}

/**
 * Command line: -u user dir, -b no Qt window, -e the game (an asset
 * template), -s a save state to continue from (slot 1 of that game).
 */
/** Game saves (Wii NAND title data, GameCube memory cards) and save states, relative to {{dataDir}}. */
const DOLPHIN_SAVES = [`${DOLPHIN_USER_DIR}/Wii/title`, `${DOLPHIN_USER_DIR}/GC`, `${DOLPHIN_USER_DIR}/StateSaves`];

function dolphinArgs(options) {
	const args = ["-u", `{{dataDir}}/${DOLPHIN_USER_DIR}`, "-b"];
	if (options.stateFor) {
		args.push("-s", `{{dataDir}}/${DOLPHIN_USER_DIR}/StateSaves/${options.stateFor}.s01`);
	}
	args.push("-e", `{{asset:${options.gameAsset}}}`);
	return args;
}

/**
 * The whole ctx.native.launch spec. `stateFor` is the game's id (as Dolphin
 * reads it from the disc) when a saved state exists and should be loaded.
 */
function dolphinSpec(options) {
	if (!options.runtime) throw new Error("a runtime asset id is required");
	if (!options.gameAsset) throw new Error("a game asset id is required");
	return {
		runtime: String(options.runtime),
		executable: DOLPHIN_EXECUTABLE,
		args: dolphinArgs({ gameAsset: String(options.gameAsset), stateFor: options.stateFor }),
		files: dolphinFiles(options),
		virtualPads: [0, 1, 2, 3],
		audio: "speaker",
		// Windowed at 16:9, centred: leaves Chrome and the overlay's bars visible.
		window: { aspect: WINDOW_ASPECT, align: "center" },
		// Backed up to the account when the session stops, restored on the next kiosk.
		saves: DOLPHIN_SAVES,
	};
}

const SUPPORTED_PLATFORMS = ["wii", "gamecube"];
const PLATFORM_LABELS = { wii: "Wii", gamecube: "GameCube" };

/** Library title ids must match [A-Za-z0-9][A-Za-z0-9._:-]{0,127}. */
function libraryIdFor(assetId) {
	return `asset-${String(assetId).replace(/[^A-Za-z0-9._:-]/g, "_")}`;
}

function assetIdFromLibraryItem(item) {
	const fromResume = item?.resume?.assetId;
	if (fromResume !== undefined && fromResume !== null) return String(fromResume);
	const id = String(item?.id ?? "");
	return id.startsWith("asset-") ? id.slice("asset-".length) : "";
}

/**
 * Wii and GameCube games out of `ctx.assets("game")`. `ready` can be played
 * now; `pending` is how many are still syncing.
 */
function selectGames(assets) {
	const games = [];
	let pending = 0;
	for (const asset of Array.isArray(assets) ? assets : []) {
		const meta = asset?.metadata ?? {};
		const platform = String(meta.platform ?? "").toLowerCase();
		if (!SUPPORTED_PLATFORMS.includes(platform)) continue;
		if (asset.state !== undefined && asset.state !== "ready") {
			pending += 1;
			continue;
		}
		const cover = typeof meta.cover_url === "string" && /^https:\/\//.test(meta.cover_url) ? meta.cover_url : "";
		games.push({
			id: String(asset.id),
			title: String(asset.title || meta.disc_title || "Untitled game"),
			platform,
			platformLabel: PLATFORM_LABELS[platform],
			gameId: typeof meta.game_id === "string" ? meta.game_id : "",
			cover,
		});
	}
	games.sort((a, b) => a.title.localeCompare(b.title) || a.id.localeCompare(b.id));
	return { ready: games, pending };
}

/** The Dolphin build to run: the pinned release if uploaded, else the newest. */
function selectRuntime(assets) {
	const found = (Array.isArray(assets) ? assets : []).filter(
		(asset) =>
			String(asset?.metadata?.runtime ?? "").toLowerCase() === DOLPHIN_RUNTIME_NAME &&
			(asset.state === undefined || asset.state === "ready"),
	);
	if (!found.length) return null;
	const version = (asset) => String(asset.metadata?.version ?? "");
	found.sort((a, b) => version(b).localeCompare(version(a), "en", { numeric: true }) || String(b.id).localeCompare(String(a.id)));
	const pinned = found.find((asset) => version(asset) === DOLPHIN_RELEASE);
	const chosen = pinned ?? found[0];
	return { id: String(chosen.id), version: version(chosen), pinned: Boolean(pinned) };
}
// </generated:dolphin-config>
