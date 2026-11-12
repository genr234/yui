// Generated block: apps/wiiu-arcade/cemu-config.js is embedded below by `bun run sync`.
// Do not edit between the markers; edit cemu-config.js and sync.

// <generated:cemu-config>
// Pure helpers for Wii U Arcade: Cemu's portable settings and controller
// profiles, the launch spec, game and runtime selection, and the role logic
// (who holds the GamePad, who holds which Wii Remote). No ctx, no DOM.
// app.yui.js embeds this file between the "generated" markers (run
// `bun run sync`; a test fails when they drift), because an app is a single
// script.
//
// Checked against cemu-project/Cemu tag v2.6 (read with `gh api`):
//   src/input/InputManager.cpp            controllerProfiles/controllerN.xml load/save: element names
//                                         (emulated_controller > type, profile, controller > api, uuid,
//                                         display_name, motion, rumble, axis, rotation, trigger,
//                                         mappings > entry > mapping, button)
//   src/input/emulated/EmulatedController.cpp  type strings "Wii U GamePad" and "Wiimote"
//   src/input/emulated/VPADController.h   GamePad mapping ids (enum ButtonId order), <toggle_display>
//   src/input/emulated/WiimoteController.h/.cpp  Wiimote mapping ids, <device_type>, MotionPlus types
//   src/input/emulated/WPADController.h   WPADDeviceType (0 core, 1 freestyle, 5 MPLS, 6 MPLS freestyle)
//   src/input/api/InputAPI.h              API string "DSUController"
//   src/input/api/DSU/DSUController.cpp   uuid = DSU slot index, <ip>/<port> per controller, button
//                                         ids kButton0..16 (DSU button bit order), touch point 1 is the
//                                         position (x/1920, y/942), has_motion, tpad1/tpad2
//   src/input/api/Controller.h            Buttons2 enum: kButton0..31 = 0..31, kButtonZL 32 ... kAxisXP 38
//   src/config/CemuConfig.cpp / .h        settings.xml root <content> and every key below
//   src/gui/PadViewFrame.cpp              the GamePad window title "GamePad View"
//
// Values the source does not settle are marked "UNVERIFIED until S2": the plan's spike S2 binds the
// controllers in Cemu 2.6 itself and the reference files land in golden/reference/. Until then
// these are read from source only.

const CEMU_RELEASE = "2.6";
const CEMU_RUNTIME_NAME = "cemu";
// Flat runtime zip: Cemu.exe at the root (see RUNTIME.md). Private install, so under runtime/.
const CEMU_EXECUTABLE = "runtime/Cemu.exe";
const CEMU_PORTABLE_DIR = "runtime/portable";
const SURFACE_ID = "second-screen";
// The pad window's title is "GamePad View" (PadViewFrame.cpp); the language is forced to English
// in settings.xml so the title is stable.
const SURFACE_TITLE = "(?i)gamepad";
const SURFACE_WIDTH = 854;
const SURFACE_HEIGHT = 480;
const DEFAULT_PLAYER_PADS = "127.0.0.1:26760";
const PLAYER_SLOTS = 4;
// DSU slot 0 is the GamePad; slots 1 to 3 are Wii Remotes 1 to 3.
const SCREEN_SLOT = 0;
const REMOTE_SLOTS = [1, 2, 3];

// Button labels on the phone: generic position name -> what the phone shows.
const GAMEPAD_LABELS = {
	East: "A",
	South: "B",
	North: "X",
	West: "Y",
	L1: "L",
	R1: "R",
	L2: "ZL",
	R2: "ZR",
	Start: "+",
	Select: "−",
};
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

// DSU button ids as Cemu numbers them (kButtonN, N = bit position of the DSU pad's two button bytes).
// 0 Share, 1 L3, 2 R3, 3 Options, 4 Up, 5 Right, 6 Down, 7 Left, 8 L2, 9 R2, 10 L1, 11 R1,
// 12 top face, 13 right face, 14 bottom face, 15 left face, 16 touch click.
const DSU_BUTTON = {
	Select: 0,
	L3: 1,
	R3: 2,
	Start: 3,
	Up: 4,
	Right: 5,
	Down: 6,
	Left: 7,
	L2: 8,
	R2: 9,
	L1: 10,
	R1: 11,
	North: 12,
	East: 13,
	South: 14,
	West: 15,
};

// DSU axes as Cemu numbers them (Buttons2 in Controller.h): positive/negative halves. The phone's
// stick bytes are 255 = up (our DSU server), which Cemu reads as +1, like XInput.
// UNVERIFIED until S2: sign of Y on the DSU API.
const AXIS = {
	LeftXP: 38,
	LeftYP: 39,
	RightXP: 40,
	RightYP: 41,
	LeftXN: 44,
	LeftYN: 45,
	RightXN: 46,
	RightYN: 47,
};

// VPADController::ButtonId, in enum order starting at 1 (kButtonId_None is 0).
const VPAD_ID = {
	A: 1,
	B: 2,
	X: 3,
	Y: 4,
	L: 5,
	R: 6,
	ZL: 7,
	ZR: 8,
	Plus: 9,
	Minus: 10,
	Up: 11,
	Down: 12,
	Left: 13,
	Right: 14,
	StickL: 15,
	StickR: 16,
	StickL_Up: 17,
	StickL_Down: 18,
	StickL_Left: 19,
	StickL_Right: 20,
	StickR_Up: 21,
	StickR_Down: 22,
	StickR_Left: 23,
	StickR_Right: 24,
};

// WiimoteController::ButtonId, in enum order starting at 1.
const WIIMOTE_ID = {
	A: 1,
	B: 2,
	One: 3,
	Two: 4,
	Nunchuck_Z: 5,
	Nunchuck_C: 6,
	Plus: 7,
	Minus: 8,
	Up: 9,
	Down: 10,
	Left: 11,
	Right: 12,
	Nunchuck_Up: 13,
	Nunchuck_Down: 14,
	Nunchuck_Left: 15,
	Nunchuck_Right: 16,
};

// [Cemu mapping id, DSU button or axis id], in file order.
const GAMEPAD_MAPPINGS = [
	[VPAD_ID.A, DSU_BUTTON.East],
	[VPAD_ID.B, DSU_BUTTON.South],
	[VPAD_ID.X, DSU_BUTTON.North],
	[VPAD_ID.Y, DSU_BUTTON.West],
	[VPAD_ID.L, DSU_BUTTON.L1],
	[VPAD_ID.R, DSU_BUTTON.R1],
	[VPAD_ID.ZL, DSU_BUTTON.L2],
	[VPAD_ID.ZR, DSU_BUTTON.R2],
	[VPAD_ID.Plus, DSU_BUTTON.Start],
	[VPAD_ID.Minus, DSU_BUTTON.Select],
	[VPAD_ID.Up, DSU_BUTTON.Up],
	[VPAD_ID.Down, DSU_BUTTON.Down],
	[VPAD_ID.Left, DSU_BUTTON.Left],
	[VPAD_ID.Right, DSU_BUTTON.Right],
	[VPAD_ID.StickL, DSU_BUTTON.L3],
	[VPAD_ID.StickR, DSU_BUTTON.R3],
	[VPAD_ID.StickL_Up, AXIS.LeftYP],
	[VPAD_ID.StickL_Down, AXIS.LeftYN],
	[VPAD_ID.StickL_Left, AXIS.LeftXN],
	[VPAD_ID.StickL_Right, AXIS.LeftXP],
	[VPAD_ID.StickR_Up, AXIS.RightYP],
	[VPAD_ID.StickR_Down, AXIS.RightYN],
	[VPAD_ID.StickR_Left, AXIS.RightXN],
	[VPAD_ID.StickR_Right, AXIS.RightXP],
];

// Same buttons as the motion-remote preset (MOTION_REMOTE_LABELS).
const WIIMOTE_MAPPINGS = [
	[WIIMOTE_ID.A, DSU_BUTTON.South],
	[WIIMOTE_ID.B, DSU_BUTTON.R2],
	[WIIMOTE_ID.One, DSU_BUTTON.West],
	[WIIMOTE_ID.Two, DSU_BUTTON.North],
	[WIIMOTE_ID.Plus, DSU_BUTTON.Start],
	[WIIMOTE_ID.Minus, DSU_BUTTON.Select],
	[WIIMOTE_ID.Up, DSU_BUTTON.Up],
	[WIIMOTE_ID.Down, DSU_BUTTON.Down],
	[WIIMOTE_ID.Left, DSU_BUTTON.Left],
	[WIIMOTE_ID.Right, DSU_BUTTON.Right],
];
const NUNCHUK_MAPPINGS = [
	[WIIMOTE_ID.Nunchuck_Z, DSU_BUTTON.L2],
	[WIIMOTE_ID.Nunchuck_C, DSU_BUTTON.L1],
	[WIIMOTE_ID.Nunchuck_Up, AXIS.LeftYP],
	[WIIMOTE_ID.Nunchuck_Down, AXIS.LeftYN],
	[WIIMOTE_ID.Nunchuck_Left, AXIS.LeftXN],
	[WIIMOTE_ID.Nunchuck_Right, AXIS.LeftXP],
];

// WPADDeviceType: a Wii Remote with MotionPlus (Wii Remote Plus). Nintendo Land asks for it.
// UNVERIFIED until S2: whether Cemu needs 5 (MPLS) here or reads motion with 0.
const DEVICE_MOTIONPLUS = 5;
const DEVICE_MOTIONPLUS_NUNCHUK = 6;

/** `host:port/slot` -> { host, port, slot }, or null when it is not one. */
function splitPadAddress(address) {
	const match = /^([^:/\s;]+):(\d{1,5})\/(\d+)$/.exec(String(address ?? ""));
	if (!match) return null;
	const port = Number(match[2]);
	if (port < 1 || port > 65535) return null;
	return { host: match[1], port, slot: Number(match[3]) };
}

function pad(value, label) {
	const parsed = splitPadAddress(value);
	if (!parsed) throw new Error(`bad virtual pad address: ${String(value)} (${label})`);
	return parsed;
}

/**
 * Player pad addresses for slots 0-3. A slot nobody is attached to takes the host and port of the
 * first given address (the platform serves all four slots from one server), else the default.
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

const escapeXml = (text) => String(text).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

/** Elements as [name, value | children[] | null, attributes?] triples, tab-indented like pugixml. */
function xml(nodes, depth = 0) {
	const indent = "\t".repeat(depth);
	return nodes
		.map(([name, value, attributes]) => {
			const attrs = attributes ? Object.entries(attributes).map(([k, v]) => ` ${k}="${escapeXml(v)}"`).join("") : "";
			if (Array.isArray(value)) return `${indent}<${name}${attrs}>\n${xml(value, depth + 1)}\n${indent}</${name}>`;
			if (value === null) return `${indent}<${name}${attrs}/>`;
			return `${indent}<${name}${attrs}>${escapeXml(value)}</${name}>`;
		})
		.join("\n");
}

const xmlDocument = (root, children) => `<?xml version="1.0" encoding="UTF-8"?>\n${xml([[root, children]])}\n`;

/**
 * settings.xml for the portable folder. Written so no first-run dialog shows (Cemu shows
 * its setup dialog only when the file is missing) and so the GamePad View is its own
 * 854x480 window the video service can find.
 */
function cemuSettings(options = {}) {
	const first = playerPads(options.pads)[0];
	return xmlDocument("content", [
		// wxLANGUAGE_ENGLISH. Keeps the window title "GamePad View". UNVERIFIED until S2: the numeric value.
		["language", "58"],
		["check_update", "false"],
		["receive_untested_updates", "false"],
		["use_discord_presence", "false"],
		["save_screenshot", "false"],
		["vk_warning", "true"],
		["gp_download", "true"],
		["macos_disclaimer", "true"],
		["fullscreen", "true"],
		["play_boot_sound", "false"],
		// CafeConsoleLanguage::EN. UNVERIFIED until S2: the numeric value (JA is 0).
		["console_language", "1"],
		["open_pad", "true"],
		["pad_position", [["x", "-1"], ["y", "-1"]]],
		["pad_size", [["x", String(SURFACE_WIDTH)], ["y", String(SURFACE_HEIGHT)]]],
		["pad_maximized", "false"],
		// 1 is Vulkan (GraphicAPI::kVulkan).
		["Graphic", [["api", "1"], ["VSync", "1"], ["AsyncCompile", "true"]]],
		// TV carries the sound (the platform captures it); the GamePad's own output stays silent.
		// UNVERIFIED until S2: whether an empty PadDevice is accepted.
		["Audio", [["TVDevice", "default"], ["PadDevice", ""], ["TVVolume", "50"], ["PadVolume", "0"]]],
		// Fallback server for controllers whose profile has no <ip>/<port> (ours always have them).
		["Input", [["DSUC", null, { host: first.host, port: String(first.port) }]]],
	]);
}

function mappingNodes(mappings) {
	return mappings.map(([mapping, button]) => ["entry", [["mapping", String(mapping)], ["button", String(button)]]]);
}

/**
 * controllerN.xml. Index 0 is the Wii U GamePad, 1 to 3 are Wii Remotes. Every one reads the DSU
 * pad whose slot equals its own index (the uuid is the DSU slot; ip and port are per profile).
 */
function cemuControllerProfile(index, options = {}) {
	if (!Number.isInteger(index) || index < 0 || index >= PLAYER_SLOTS) throw new Error(`bad controller index: ${String(index)}`);
	const address = playerPads(options.pads)[index];
	const gamepad = index === SCREEN_SLOT;
	const nunchuk = options.extension === "Nunchuk";
	const type = gamepad ? "Wii U GamePad" : "Wiimote";
	const custom = gamepad
		? [["toggle_display", "0"]]
		: [["device_type", String(nunchuk ? DEVICE_MOTIONPLUS_NUNCHUK : DEVICE_MOTIONPLUS)]];
	const mappings = gamepad ? GAMEPAD_MAPPINGS : [...WIIMOTE_MAPPINGS, ...(nunchuk ? NUNCHUK_MAPPINGS : [])];
	return xmlDocument("emulated_controller", [
		["type", type],
		...custom,
		[
			"controller",
			[
				["api", "DSUController"],
				["uuid", String(address.slot)],
				["display_name", `Controller ${address.slot + 1}`],
				// DSU motion drives the GamePad gyro and the Wii Remote MotionPlus.
				["motion", "true"],
				["axis", [["deadzone", "0.1"], ["range", "1"]]],
				["rotation", [["deadzone", "0.1"], ["range", "1"]]],
				["trigger", [["deadzone", "0.1"], ["range", "1"]]],
				["ip", address.host],
				["port", String(address.port)],
				["mappings", mappingNodes(mappings)],
			],
		],
	]);
}

/**
 * Files for ctx.native.launch (paths relative to {{dataDir}}, under the private runtime's
 * portable folder). options.pads: addresses ("host:port/slot") by slot; options.extension:
 * "None" or "Nunchuk" for the Wii Remotes.
 */
function cemuFiles(options = {}) {
	const files = [{ path: `${CEMU_PORTABLE_DIR}/settings.xml`, content: cemuSettings(options) }];
	for (let index = 0; index < PLAYER_SLOTS; index++) {
		files.push({ path: `${CEMU_PORTABLE_DIR}/controllerProfiles/controller${index}.xml`, content: cemuControllerProfile(index, options) });
	}
	return files;
}

// Game saves (MLC user saves), relative to {{dataDir}}.
const CEMU_SAVES = [`${CEMU_PORTABLE_DIR}/mlc01/usr/save`];

/** The whole ctx.native.launch spec. */
function cemuSpec(options) {
	if (!options.runtime) throw new Error("a runtime asset id is required");
	if (!options.gameAsset) throw new Error("a game asset id is required");
	return {
		runtime: String(options.runtime),
		install: "private",
		executable: CEMU_EXECUTABLE,
		// -g game (a WUA path), -f fullscreen (LaunchSettings.cpp).
		args: ["-g", `{{asset:${options.gameAsset}}}`, "-f"],
		files: cemuFiles(options),
		virtualPads: [0, 1, 2, 3],
		audio: "speaker",
		saves: CEMU_SAVES,
		surfaces: [{ id: SURFACE_ID, window: { title: SURFACE_TITLE }, hidden: true, width: SURFACE_WIDTH, height: SURFACE_HEIGHT }],
	};
}

const PLATFORM = "wiiu";

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
 * Wii U games out of `ctx.assets("game")`: platform "wiiu" and not marked unplayable. `ready` can
 * be played now; `pending` is how many are still syncing; `unplayable` is how many are Wii U
 * dumps in a format this app cannot play (they are shown as "convert to WUA").
 */
function selectGames(assets) {
	const games = [];
	let pending = 0;
	let unplayable = 0;
	for (const asset of Array.isArray(assets) ? assets : []) {
		const meta = asset?.metadata ?? {};
		if (String(meta.platform ?? "").toLowerCase() !== PLATFORM) continue;
		if (meta.playable === false) {
			unplayable += 1;
			continue;
		}
		if (asset.state !== undefined && asset.state !== "ready") {
			pending += 1;
			continue;
		}
		const cover = typeof meta.cover_url === "string" && /^https:\/\//.test(meta.cover_url) ? meta.cover_url : "";
		games.push({
			id: String(asset.id),
			title: String(asset.title || meta.title || "Untitled game"),
			titleId: typeof meta.title_id === "string" ? meta.title_id : "",
			cover,
			coverColor: typeof meta.cover_color === "string" ? meta.cover_color : "",
		});
	}
	games.sort((a, b) => a.title.localeCompare(b.title) || a.id.localeCompare(b.id));
	return { ready: games, pending, unplayable };
}

/** The runtime build to run: the pinned version if uploaded, else the newest. */
function selectRuntime(assets, name = CEMU_RUNTIME_NAME, pinned = CEMU_RELEASE) {
	const found = (Array.isArray(assets) ? assets : []).filter(
		(asset) => String(asset?.metadata?.runtime ?? "").toLowerCase() === name && (asset.state === undefined || asset.state === "ready"),
	);
	if (!found.length) return null;
	const version = (asset) => String(asset.metadata?.version ?? "");
	found.sort((a, b) => version(b).localeCompare(version(a), "en", { numeric: true }) || String(b.id).localeCompare(String(a.id)));
	const exact = found.find((asset) => version(asset) === pinned);
	const chosen = exact ?? found[0];
	return { id: String(chosen.id), version: version(chosen), pinned: Boolean(exact) };
}

// ---- roles ---------------------------------------------------------------------------------

/** Who can hold the GamePad: connected, and the phone said it can show video. */
function eligibleForScreen(player) {
	return Boolean(player && player.connected && player.caps && player.caps.video);
}

const linkRank = (player) => (player.relayed === false ? 0 : player.relayed === true ? 2 : 1);

/**
 * Roles for the connected players. `previous` is the last result: a holder who is still eligible
 * keeps the GamePad, and Wii Remote slots stay with the same people.
 * Returns { screen: playerId | null, remotes: { [playerId]: 1 | 2 | 3 } }.
 * Without a previous holder the GamePad goes to an eligible player on a direct link first
 * (relayed === false; an unknown link ranks after it), then the host, then join order.
 */
function assignRoles(players, previous) {
	const list = (Array.isArray(players) ? players : []).filter((player) => player && player.connected);
	const prev = previous ?? { screen: null, remotes: {} };
	const holder = list.find((player) => player.id === prev.screen && eligibleForScreen(player));
	let screen = holder ? holder.id : null;
	if (!screen) {
		const candidates = list
			.map((player, order) => ({ player, order }))
			.filter(({ player }) => eligibleForScreen(player))
			.sort((a, b) => linkRank(a.player) - linkRank(b.player) || Number(Boolean(b.player.host)) - Number(Boolean(a.player.host)) || a.order - b.order);
		screen = candidates.length ? candidates[0].player.id : null;
	}
	const remotes = {};
	const used = new Set();
	const rest = list.filter((player) => player.id !== screen);
	for (const player of rest) {
		const slot = prev.remotes?.[player.id];
		if (REMOTE_SLOTS.includes(slot) && !used.has(slot)) {
			remotes[player.id] = slot;
			used.add(slot);
		}
	}
	const ordered = rest
		.map((player, order) => ({ player, order }))
		.sort((a, b) => Number(Boolean(b.player.host)) - Number(Boolean(a.player.host)) || a.order - b.order);
	for (const { player } of ordered) {
		if (remotes[player.id] !== undefined) continue;
		const slot = REMOTE_SLOTS.find((candidate) => !used.has(candidate));
		if (slot === undefined) break;
		remotes[player.id] = slot;
		used.add(slot);
	}
	return { screen, remotes };
}

/**
 * Hand the GamePad to `toPlayerId`. The previous holder takes the Wii Remote slot the new holder
 * leaves (or a free one). Returns a new roles object; the same one when there is nothing to swap.
 */
function swapScreen(roles, toPlayerId) {
	const target = String(toPlayerId ?? "");
	if (!target || roles.screen === target) return roles;
	const remotes = { ...roles.remotes };
	const freed = remotes[target];
	delete remotes[target];
	if (roles.screen) {
		const slot = freed ?? REMOTE_SLOTS.find((candidate) => !Object.values(remotes).includes(candidate));
		if (slot !== undefined) remotes[roles.screen] = slot;
	}
	return { screen: target, remotes };
}

/** Players who could take the GamePad now, other than `excludeId`. */
function screenCandidates(players, excludeId) {
	return (Array.isArray(players) ? players : []).filter((player) => eligibleForScreen(player) && player.id !== excludeId);
}
// </generated:cemu-config>

export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.wiiu-arcade",
  name: "Wii U Arcade",
  version: "0.1.0",
  icon: "🕹️",
  category: "Games",
  description: "Wii U games from your library. One phone is the GamePad with a private screen, the others are Wii Remotes.",
  permissions: ["controllers", "virtual-pads", "surfaces", "assets:game", "assets:runtime", "native", "audio.speaker", "library"],

  // Declaring controllers gives this app the phones while it is open.
  controllers: {},

  mount(ctx) {
    const pad = ctx.controllers;
    const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
    const errorText = (error) => String((error && error.message) || error || "Something went wrong").replace(/^YUI_[A-Z_]+:\s*/, "");
    const SURFACE_READY_TIMEOUT_MS = 30000;
    const HOLDER_GRACE_MS = 10000;

    const state = ctx.state({
      loaded: false,
      games: [],
      pending: 0,
      unplayable: 0,
      runtime: null, // the Cemu runtime
      encoder: null, // the ffmpeg runtime
      surfaces: null, // { available, reason } from ctx.surfaces.available()
      phase: "lobby", // lobby | starting | playing | stopping
      current: null, // the game being played
      history: {}, // asset id → true once played
      error: "",
      roles: { screen: null, remotes: {} }, // while playing
      preferredScreen: null, // the host's pick in the lobby
      swapMenu: null, // id of the player looking at the "give the GamePad to" list
    });

    // Player pads by slot, as the platform reports them from attach().
    const padAddresses = [];
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
        state.unplayable = picked.unplayable;
        state.runtime = selectRuntime(runtimes, CEMU_RUNTIME_NAME, CEMU_RELEASE);
        state.encoder = selectRuntime(runtimes, "ffmpeg", "8.0");
        if (state.error.startsWith("Could not read")) state.error = "";
        if (!watching) {
          watching = true;
          ctx.assets.onChange(() => refresh());
        }
      } catch (error) {
        state.error = `Could not read the asset library: ${errorText(error)}`;
      }
      try {
        state.surfaces = await ctx.surfaces.available();
        if (state.surfaces?.available) listenSurfaces();
      } catch (error) {
        state.surfaces = { available: false, reason: "unsupported" };
        ctx.log.warn("surfaces unavailable", errorText(error));
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
          if (assetId) history[assetId] = true;
        }
        state.history = history;
      } catch (error) {
        ctx.log.warn("library unavailable", errorText(error));
      }
    }

    // What is missing before a game can start, as a sentence; "" when nothing is.
    function missingText() {
      if (!state.runtime) return "The Cemu runtime has not been uploaded to this account's library (name cemu, version " + CEMU_RELEASE + ").";
      if (!state.encoder) return "The ffmpeg runtime has not been uploaded to this account's library (name ffmpeg, version 8.0).";
      if (state.surfaces && !state.surfaces.available) {
        return state.surfaces.reason === "no_encoder"
          ? "This kiosk could not find a video encoder in the ffmpeg runtime."
          : "This kiosk cannot show a private screen on a phone.";
      }
      return "";
    }

    // ---- helpers ---------------------------------------------------------

    const gameById = (id) => state.games.find((game) => game.id === String(id)) || null;
    const hostPlayer = () => pad.players.find((player) => player.host && player.connected) || null;
    const playerById = (id) => pad.players.find((player) => player.id === id) || null;
    const roleOf = (roles, id) => (roles.screen === id ? "screen" : roles.remotes[id] !== undefined ? "remote" : "watch");

    function lobbyRoles() {
      return assignRoles(pad.players, { screen: state.preferredScreen, remotes: {} });
    }

    function markPlayed(game) {
      const entry = { id: libraryIdFor(game.id), title: game.title, players: 4, resume: { assetId: game.id } };
      if (game.cover) entry.art = game.id;
      ctx.library
        .publish([entry])
        .then(() => ctx.library.played(entry.id, { resume: entry.resume }))
        .catch((error) => ctx.log.warn("library update failed", errorText(error)));
      state.history = { ...state.history, [game.id]: true };
    }

    function toast(message) {
      ctx.toast(message).catch(() => {});
    }

    // ---- roles ------------------------------------------------------------

    async function attachRole(id, roles) {
      const player = playerById(id);
      if (!player) return;
      const screen = roles.screen === id;
      const slot = screen ? SCREEN_SLOT : roles.remotes[id];
      if (slot === undefined) return;
      const attachment = await ctx.virtualPads.attach(player, { slot, touch: screen ? "surface" : "pointer" });
      padAddresses[slot] = attachment.address;
    }

    async function detachQuietly(id) {
      try {
        await ctx.virtualPads.detach(id);
      } catch (error) {
        ctx.log.warn("detach failed", errorText(error));
      }
    }

    async function attachAll(roles) {
      padAddresses.length = 0;
      const ids = [roles.screen, ...Object.keys(roles.remotes)].filter(Boolean);
      for (const id of ids) await attachRole(id, roles);
    }

    // Move to `next` while a game runs (W10): show the surface first (it fails fast when the
    // phone has no video link), then re-attach whoever's slot changed. The program never restarts.
    async function applyRoles(next) {
      const prev = state.roles;
      if (next.screen && next.screen !== prev.screen) await ctx.surfaces.show(SURFACE_ID, next.screen);
      const changed = new Set();
      const slotOf = (roles, id) => (roles.screen === id ? SCREEN_SLOT : roles.remotes[id]);
      const ids = new Set([prev.screen, next.screen, ...Object.keys(prev.remotes), ...Object.keys(next.remotes)].filter(Boolean));
      for (const id of ids) if (slotOf(prev, id) !== slotOf(next, id) || (prev.screen === id) !== (next.screen === id)) changed.add(id);
      for (const id of changed) await detachQuietly(id);
      state.roles = next;
      for (const id of changed) {
        if (slotOf(next, id) !== undefined && playerById(id) && playerById(id).connected) await attachRole(id, next);
      }
    }

    async function resetPlayers() {
      const ids = [state.roles.screen, ...Object.keys(state.roles.remotes)].filter(Boolean);
      state.roles = { screen: null, remotes: {} };
      state.swapMenu = null;
      for (const id of ids) await detachQuietly(id);
    }

    // ---- starting and stopping a game -------------------------------------

    // Each start (and each cancel) bumps the attempt, so a launch that comes back late, or a
    // status event from an earlier program, cannot move a newer attempt along. `nativeSession`
    // is the program this app is following; status events from any other session are ignored.
    let attempt = 0;
    let nativeSession = null;

    // Resolves with the surface status once it can be shown; rejects on failure or timeout.
    function waitForSurface(mine) {
      return new Promise((resolve, reject) => {
        let done = false;
        let off = () => {};
        let timer = null;
        const finish = (fn, value) => {
          if (done) return;
          done = true;
          off();
          if (timer) clearTimeout(timer);
          fn(value);
        };
        const check = (list) => {
          if (mine !== attempt) return finish(reject, new Error("cancelled"));
          const found = (list || []).find((item) => item.id === SURFACE_ID);
          if (!found) return;
          if (found.state === "ready" || found.state === "streaming") finish(resolve, found);
          else if (found.state === "failed" || found.state === "closed") finish(reject, new Error(found.error || "The GamePad screen did not appear."));
        };
        off = ctx.surfaces.on("status", check);
        timer = setTimeout(() => finish(reject, new Error("The GamePad screen did not appear in time.")), SURFACE_READY_TIMEOUT_MS);
        ctx.surfaces.list().then(check, (error) => finish(reject, error));
      });
    }

    // Show the surface to the roles' screen holder; when that phone has no video link, hand it to
    // the next eligible player and re-attach. Returns the roles that worked, or throws.
    async function showSurface(roles) {
      let current = roles;
      const tried = new Set();
      while (current.screen) {
        try {
          await ctx.surfaces.show(SURFACE_ID, current.screen);
          return current;
        } catch (error) {
          const text = errorText(error);
          if (!/no_video/.test(text) && !/no_video_link/.test(text)) throw error;
          tried.add(current.screen);
          const next = screenCandidates(pad.players, null).find((player) => !tried.has(player.id));
          if (!next) throw new Error("No phone can show the GamePad screen right now.");
          current = swapScreen(current, next.id);
        }
      }
      throw new Error("No phone can show the GamePad screen right now.");
    }

    async function start(game) {
      if (state.phase !== "lobby") return;
      const missing = missingText();
      if (missing) {
        state.error = missing;
        return;
      }
      const mine = ++attempt;
      nativeSession = null;
      const roles = lobbyRoles();
      if (!roles.screen) {
        state.error = "No connected phone can show the GamePad screen. Join with a phone that supports video.";
        return;
      }
      state.phase = "starting";
      state.current = game;
      state.error = "";
      pendingStart = null;
      try {
        // Pads are attached before the launch so their addresses are in the config files.
        // The attached roles are final unless the GamePad has to move (no video link).
        state.roles = roles;
        await attachAll(roles);
        if (mine !== attempt) return;
        const spec = cemuSpec({ runtime: state.runtime.id, gameAsset: game.id, pads: padAddresses });
        const status = await ctx.native.launch(spec);
        if (mine !== attempt) {
          if (status.state === "running" || status.state === "preparing") ctx.native.stop({ force: true }).catch(() => {});
          return;
        }
        if (status.state === "busy") throw new Error("Another app is running a program right now.");
        if (status.state === "failed" || status.state === "crashed") throw new Error(status.error || "The emulator did not start.");
        if (status.state === "exited") throw new Error("The emulator closed right away.");
        if (status.session_id) nativeSession = status.session_id;
        await waitForSurface(mine);
        if (mine !== attempt) return;
        const shown = await showSurface(roles);
        if (mine !== attempt) return;
        if (shown.screen !== roles.screen) await applyRoles(shown);
        if (mine !== attempt) return;
        state.phase = "playing";
        markPlayed(game);
      } catch (error) {
        if (mine !== attempt) return;
        attempt++;
        nativeSession = null;
        try {
          await ctx.native.stop({ force: true });
        } catch (stopError) {
          ctx.log.warn("stop after failed start", errorText(stopError));
        }
        await resetPlayers();
        state.phase = "lobby";
        state.current = null;
        state.error = errorText(error);
      }
    }

    // The host gave up waiting. Whatever the launch got to is stopped; a program still being
    // prepared never starts.
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

    // The program ended (a quit, a crash, or the platform's own watchdog).
    async function ended(status) {
      if (state.phase === "lobby") return;
      attempt++;
      nativeSession = null;
      clearHolderTimer();
      await resetPlayers();
      state.phase = "lobby";
      state.current = null;
      if (status && status.state === "crashed") state.error = status.error || "The emulator stopped unexpectedly.";
      else if (status && status.state === "failed") state.error = status.error || "The emulator did not start.";
    }

    ctx.native.on("status", (status) => {
      const id = status.session_id || null;
      if (status.state === "preparing" || status.state === "running") {
        if (state.phase === "starting" || state.phase === "playing") nativeSession = id;
        return;
      }
      if (status.state !== "exited" && status.state !== "crashed" && status.state !== "failed") return;
      // An earlier program's end must not end the game that is starting or running now.
      if (state.phase === "starting" && nativeSession === null) return;
      if (nativeSession !== null && id !== null && id !== nativeSession) return;
      ended(status);
    });

    // Close the window, then kill after the platform's grace period. Cemu has no save states.
    async function quit() {
      if (state.phase !== "playing") return;
      state.phase = "stopping";
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
      if (!pendingStart || state.phase !== "lobby" || !state.loaded) return;
      if (!hostPlayer()) return;
      const game = gameById(pendingStart.assetId);
      if (!game) {
        if (state.pending === 0) {
          pendingStart = null;
          state.error = "That game is not in the library anymore.";
        }
        return;
      }
      if (missingText()) {
        pendingStart = null;
        state.error = missingText();
        return;
      }
      start(game);
    }

    // Opened from Jump back in: resume that game.
    if (launchItem) {
      const assetId = assetIdFromLibraryItem(launchItem);
      if (assetId) pendingStart = { assetId };
    }

    // ---- the GamePad holder -------------------------------------------------

    let holderTimer = null;
    function clearHolderTimer() {
      if (holderTimer) clearTimeout(holderTimer);
      holderTimer = null;
    }

    // Give the GamePad to `toId` (a swap, or a handoff when the holder is gone).
    async function giveScreenTo(toId, why) {
      if (state.phase !== "playing" || state.roles.screen === toId) return;
      const target = playerById(toId);
      if (!target || !eligibleForScreen(target)) return;
      const old = state.roles.screen;
      let next = swapScreen(state.roles, toId);
      const oldPlayer = old ? playerById(old) : null;
      if (old && (!oldPlayer || !oldPlayer.connected)) {
        const remotes = { ...next.remotes };
        delete remotes[old];
        next = { screen: next.screen, remotes };
      }
      try {
        await applyRoles(next);
        if (why) toast(`${target.name || "A player"} now has the GamePad. ${why}`);
      } catch (error) {
        ctx.log.warn("could not move the GamePad", errorText(error));
        toast(`Could not move the GamePad: ${errorText(error)}`);
      }
    }

    // The holder is gone (disconnected, video link lost): after a grace period the GamePad goes to
    // the host, or the next eligible player (a direct link first).
    function holderLost() {
      const holder = playerById(state.roles.screen);
      if (!holder || !holder.connected || !eligibleForScreen(holder)) return true;
      return false;
    }

    function watchHolder() {
      if (state.phase !== "playing" || !state.roles.screen) return clearHolderTimer();
      if (!holderLost()) return clearHolderTimer();
      if (holderTimer) return;
      holderTimer = setTimeout(() => {
        holderTimer = null;
        if (state.phase !== "playing" || !holderLost()) return;
        const holderId = state.roles.screen;
        const options = screenCandidates(pad.players, holderId);
        options.sort((a, b) => Number(b.host) - Number(a.host) || Number(a.relayed === true) - Number(b.relayed === true));
        if (!options.length) {
          toast("Nobody can show the GamePad screen right now.");
          return;
        }
        giveScreenTo(options[0].id, "The old holder dropped out.");
      }, HOLDER_GRACE_MS);
    }

    // The platform put the surface back to "ready" (the holder's link dropped to cable fallback or
    // their peer closed): same grace rule as a disconnect, shortened.
    // Registered once surfaces.available() has resolved: on() needs the permission already granted.
    let surfaceListener = null;
    function listenSurfaces() {
      if (surfaceListener) return;
      surfaceListener = ctx.surfaces.on("status", (list) => {
        if (state.phase !== "playing") return;
        const found = (list || []).find((item) => item.id === SURFACE_ID);
        if (!found || !state.roles.screen) return;
        if (found.state === "ready" && found.player_id === null && !holderTimer) {
          holderTimer = setTimeout(() => {
            holderTimer = null;
            if (state.phase !== "playing") return;
            const holder = playerById(state.roles.screen);
            // They reconnected in the meantime: show it to them again.
            if (holder && eligibleForScreen(holder)) {
              ctx.surfaces.show(SURFACE_ID, holder.id).catch(() => {});
              return;
            }
            watchHolder();
          }, 3000);
        }
      });
    }

    // ---- players ----------------------------------------------------------

    // A phone that joins while a game runs takes the next free Wii Remote.
    async function joinMidGame(player) {
      if (state.phase !== "playing" || !player.connected) return;
      if (roleOf(state.roles, player.id) !== "watch") return;
      const slot = REMOTE_SLOTS.find((candidate) => !Object.values(state.roles.remotes).includes(candidate));
      if (slot === undefined) return;
      const next = { screen: state.roles.screen, remotes: { ...state.roles.remotes, [player.id]: slot } };
      try {
        state.roles = next;
        await attachRole(player.id, next);
      } catch (error) {
        ctx.log.warn("could not attach a late player", errorText(error));
        const remotes = { ...state.roles.remotes };
        delete remotes[player.id];
        state.roles = { screen: state.roles.screen, remotes };
      }
    }

    async function leaveRole(player) {
      if (roleOf(state.roles, player.id) === "watch") return;
      const remotes = { ...state.roles.remotes };
      delete remotes[player.id];
      state.roles = { screen: state.roles.screen === player.id ? null : state.roles.screen, remotes };
      await detachQuietly(player.id);
      if (state.swapMenu === player.id) state.swapMenu = null;
    }

    pad.on("join", (player) => joinMidGame(player));
    pad.on("leave", (player) => {
      if (state.phase === "playing" && player.id !== state.roles.screen) leaveRole(player);
      if (state.preferredScreen === player.id) state.preferredScreen = null;
      watchHolder();
    });
    pad.on("players", () => {
      maybeStartPending();
      if (state.preferredScreen) {
        const chosen = playerById(state.preferredScreen);
        if (!chosen || !eligibleForScreen(chosen)) state.preferredScreen = null;
      }
      if (state.phase === "playing") {
        for (const player of pad.players) if (player.connected) joinMidGame(player);
        watchHolder();
      }
    });

    // ---- the phone's Home sheet --------------------------------------------

    pad.on("system", async ({ player, action }) => {
      if (state.phase !== "playing") return;
      switch (action) {
        case "resume":
          break;
        case "quit":
          if (player.host) await quit();
          else await leaveRole(player);
          break;
        default:
          // Cemu has no save states: save_state and load_state do nothing.
          break;
      }
    });

    // ---- phone layouts -------------------------------------------------------

    function gameItems() {
      return state.games.map((game) => {
        const item = { id: game.id, title: game.title, subtitle: state.history[game.id] ? "Wii U · played" : "Wii U" };
        if (game.cover) item.image = game.cover;
        return item;
      });
    }

    const centered = (ui, title, body, extra) =>
      ui.column({ gap: 12 }, [ui.text(title, { size: "large", align: "center" }), body ? ui.text(body, { align: "center" }) : null, extra || null]);

    function roleCard(ui, player, roles) {
      const role = roleOf(roles, player.id);
      if (role === "screen") return centered(ui, "You have the GamePad", "Your phone shows the second screen. Hold it sideways.");
      if (role === "remote") return centered(ui, `You are Wii Remote ${roles.remotes[player.id]}`, "Hold your phone like a Wii Remote.");
      return centered(ui, "Watching", "There are no more Wii Remotes free.");
    }

    // Who holds the GamePad: the host picks from the phones that can show it.
    function screenChoice(ui, roles) {
      const options = screenCandidates(pad.players, null).map((player) => ({ id: player.id, label: player.name || "Player" }));
      if (options.length < 2) return null;
      return ui.column({ gap: 8 }, [
        ui.text("Who holds the GamePad?", { size: "small", align: "center" }),
        ui.choice({ id: "screen_holder", options, selected: roles.screen || undefined }),
      ]);
    }

    function swapMenu(ui, player) {
      const options = screenCandidates(pad.players, state.roles.screen).map((candidate) => ({
        id: candidate.id,
        label: `${candidate.name || "Player"}${candidate.relayed === true ? " (mobile data)" : ""}`,
      }));
      return ui.column({ gap: 12 }, [
        ui.text("Give the GamePad to", { size: "large", align: "center" }),
        options.length ? ui.choice({ id: "swap_to", options }) : ui.text("Nobody else can show it.", { align: "center" }),
        ui.button({ id: "swap_cancel", label: "Back", variant: "ghost" }),
      ]);
    }

    function screenLayout(ui) {
      const btn = (name, bit, extra) => ({ id: name, label: GAMEPAD_LABELS[name] || name, bit, variant: "secondary", ...extra });
      const left = ui.column({ gap: 8, flex: 1 }, [
        ui.row({ gap: 8 }, [ui.button(btn("L2", 6, { shape: "pill", size: "small" })), ui.button(btn("L1", 4, { shape: "pill", size: "small" }))]),
        ui.stick({ id: "ls", side: "left", click_bit: 11 }),
        ui.dpad({ id: "dp", mode: "bits" }),
      ]);
      const face = ui.grid({ columns: 3, gap: 6, square: true }, [
        ui.spacer({ flex: 1 }),
        ui.button(btn("North", 3, { shape: "round", variant: "primary" })),
        ui.spacer({ flex: 1 }),
        ui.button(btn("West", 2, { shape: "round", variant: "primary" })),
        ui.spacer({ flex: 1 }),
        ui.button(btn("East", 1, { shape: "round", variant: "primary" })),
        ui.spacer({ flex: 1 }),
        ui.button(btn("South", 0, { shape: "round", variant: "primary" })),
        ui.spacer({ flex: 1 }),
      ]);
      const right = ui.column({ gap: 8, flex: 1 }, [
        ui.row({ gap: 8 }, [ui.button(btn("R1", 5, { shape: "pill", size: "small" })), ui.button(btn("R2", 7, { shape: "pill", size: "small" }))]),
        face,
        ui.stick({ id: "rs", side: "right", click_bit: 12 }),
        ui.row({ gap: 8 }, [ui.button(btn("Select", 8, { shape: "pill", size: "small" })), ui.button(btn("Start", 9, { shape: "pill", size: "small" }))]),
      ]);
      return ui.column({ gap: 6, flex: 1 }, [
        ui.row({ gap: 10, flex: 1 }, [left, ui.surface({ id: "screen", surface: SURFACE_ID, touch: true, flex: 3 }), right]),
        ui.motion({ id: "motion", frame: "screen" }),
        ui.button({ id: "swap", label: "Swap", variant: "ghost", size: "small" }),
      ]);
    }

    pad.layout((player) => {
      const ui = pad.ui;
      if (state.phase === "playing") {
        const role = roleOf(state.roles, player.id);
        if (state.swapMenu === player.id && (role === "screen" || player.host)) return swapMenu(ui, player);
        if (role === "screen") return screenLayout(ui);
        if (role === "remote") {
          const remote = ui.preset("motion-remote", { labels: MOTION_REMOTE_LABELS });
          return player.host ? ui.column({ gap: 6, flex: 1 }, [ui.button({ id: "swap", label: "Swap GamePad", variant: "ghost", size: "small" }), remote]) : remote;
        }
        return centered(ui, "Watching", "Tap Home to see who is playing.");
      }
      if (state.phase === "starting") {
        return ui.column({ gap: 12 }, [
          ui.text("Starting…", { size: "large", align: "center" }),
          ui.text("The GamePad screen can take a moment to appear.", { align: "center", size: "small" }),
          player.host ? ui.button({ id: "cancel", label: "Cancel", variant: "ghost" }) : null,
        ]);
      }
      if (state.phase === "stopping") return centered(ui, "Closing…");
      const roles = lobbyRoles();
      if (!player.host) {
        return ui.column({ gap: 12 }, [
          ui.text("Wii U Arcade", { size: "large", align: "center" }),
          ui.text("The host is choosing a game.", { align: "center" }),
          roleCard(ui, player, roles),
        ]);
      }
      if (!state.loaded) return ui.text("Looking for games…", { align: "center" });
      const missing = missingText();
      if (missing) return centered(ui, "Not ready yet", missing);
      if (!state.games.length) {
        return centered(
          ui,
          "No games yet",
          state.pending
            ? `${state.pending} still downloading.`
            : state.unplayable
              ? "Your Wii U dumps need converting to WUA first (see the kiosk screen)."
              : "Upload Wii U games (.wua) to your library.",
        );
      }
      return ui.column({ gap: 12 }, [
        ui.text("Pick a game", { size: "large", align: "center" }),
        state.error ? ui.text(state.error, { size: "small", align: "center" }) : null,
        roleCard(ui, player, roles),
        screenChoice(ui, roles),
        ui.list({ id: "games", columns: 2, items: gameItems() }),
      ]);
    });

    pad.on("event", ({ player, control, kind, value }) => {
      if (state.phase === "playing") {
        if (control === "swap" && kind === "tap" && (state.roles.screen === player.id || player.host)) state.swapMenu = player.id;
        else if (control === "swap_cancel" && kind === "tap") state.swapMenu = null;
        else if (control === "swap_to" && kind === "tap" && state.swapMenu === player.id) {
          state.swapMenu = null;
          giveScreenTo(String(value), "");
        }
        return;
      }
      if (!player.host) return;
      if (state.phase === "starting" && control === "cancel" && kind === "tap") {
        cancelStart();
        return;
      }
      if (state.phase !== "lobby") return;
      if (control === "screen_holder" && kind === "tap") {
        const chosen = playerById(String(value));
        if (chosen && eligibleForScreen(chosen)) state.preferredScreen = chosen.id;
      } else if (control === "games" && kind === "tap") {
        const game = gameById(value);
        if (game) start(game);
      }
    });

    // ---- the screen ------------------------------------------------------------

    function roleLabel(player) {
      if (state.phase === "lobby" || state.phase === "starting") {
        const roles = state.phase === "lobby" ? lobbyRoles() : state.roles;
        const role = roleOf(roles, player.id);
        const base = role === "screen" ? "GamePad" : role === "remote" ? `Wii Remote ${roles.remotes[player.id]}` : "Watching";
        return player.host ? `${base} · host` : base;
      }
      const role = roleOf(state.roles, player.id);
      return role === "screen" ? "GamePad" : role === "remote" ? `Wii Remote ${state.roles.remotes[player.id]}` : "Watching";
    }

    function playerRows() {
      const players = pad.players;
      if (!players.length) return [ctx.ui.small("Nobody has joined yet.")];
      return players.map((player) =>
        ctx.ui.row({ gap: 10, align: "center" }, [
          ctx.ui.text(player.name || "Player"),
          ctx.ui.small(`${roleLabel(player)}${player.connected ? "" : " · reconnecting"}${player.caps && !player.caps.video ? " · no video" : ""}${player.relayed === true ? " · mobile data" : ""}`),
        ]),
      );
    }

    function gameCard(game) {
      return ctx.ui.column({ gap: 6, width: 150 }, [
        game.cover
          ? ctx.ui.image({ src: game.cover, alt: game.title, width: 150, height: 150 })
          : ctx.ui.card({ padding: 14, height: 150, background: game.coverColor || undefined }, [ctx.ui.small("Wii U")]),
        ctx.ui.text(game.title),
        ctx.ui.small(state.history[game.id] ? "Wii U · played" : "Wii U"),
        ctx.ui.button({ label: "Play", onClick: () => start(game) }),
      ]);
    }

    function emptyState() {
      if (!state.loaded) return ctx.ui.p("Looking for games…");
      const lines = [];
      const missing = missingText();
      if (missing) lines.push(ctx.ui.empty(missing));
      if (!state.games.length) {
        lines.push(
          ctx.ui.empty(
            state.pending
              ? `${state.pending} game${state.pending === 1 ? " is" : "s are"} still downloading.`
              : "No Wii U games yet. Upload a .wua file to this account's library and it appears here.",
          ),
        );
      }
      if (state.unplayable) {
        lines.push(
          ctx.ui.empty(
            `${state.unplayable} Wii U dump${state.unplayable === 1 ? " is" : "s are"} in a format this app cannot play (.wud or .wux). Convert ${state.unplayable === 1 ? "it" : "them"} to WUA with Cemu's title manager.`,
          ),
        );
      }
      return lines.length ? ctx.ui.column({ gap: 8 }, lines) : null;
    }

    refresh();
    loadHistory();
    // A program this app started before a reload keeps running: follow it and put the roles back.
    ctx.native
      .status()
      .then((status) => {
        if (state.phase !== "lobby") return;
        if (status.state === "running" || status.state === "preparing") {
          nativeSession = status.session_id || null;
          if (status.state === "preparing") {
            state.phase = "starting";
            return;
          }
          const roles = assignRoles(pad.players, null);
          state.phase = "playing";
          state.roles = { screen: null, remotes: {} };
          applyRoles(roles).catch((error) => ctx.log.warn("could not restore roles", errorText(error)));
        }
      })
      .catch(() => {});

    // Wake lock while a game is starting, running or closing: no screensaver, sleep or idle tool
    // may cover the game. Closing the app releases it too.
    let wakeLock = null;
    let wakePending = false;
    async function keepAwake(wanted) {
      if (!ctx.wakeLock || wakePending) return;
      if (wanted === Boolean(wakeLock && !wakeLock.released)) return;
      wakePending = true;
      try {
        if (wanted) wakeLock = await ctx.wakeLock.request("Wii U game running");
        else if (wakeLock) await wakeLock.release();
      } catch (error) {
        ctx.log.warn("wake lock", error);
      } finally {
        wakePending = false;
      }
      const now = state.phase !== "lobby";
      if (now !== Boolean(wakeLock && !wakeLock.released)) void keepAwake(now);
    }

    return () => {
      void keepAwake(state.phase !== "lobby");
      return ctx.ui.column({ gap: 16, padding: 18 }, [
        ctx.ui.h1("Wii U Arcade"),
        state.error ? ctx.ui.p(state.error) : null,
        ctx.ui.when(
          state.phase === "lobby",
          ctx.ui.column({ gap: 16 }, [
            ctx.ui.row({ gap: 24, align: "start", wrap: true }, [
              ctx.ui.column({ gap: 8 }, [ctx.ui.h3("Scan to play"), pad.joinCard({ size: 140 })]),
              ctx.ui.column({ gap: 8 }, [ctx.ui.h3("Players"), ...playerRows()]),
            ]),
            emptyState(),
            state.games.length && !missingText()
              ? ctx.ui.column({ gap: 10 }, [
                  ctx.ui.h3("Games"),
                  ctx.ui.p("One phone is the GamePad with the second screen; the others are Wii Remotes. The host can also pick from their phone."),
                  ctx.ui.row({ gap: 14, wrap: true }, state.games.map(gameCard)),
                ])
              : null,
          ]),
        ),
        ctx.ui.when(
          state.phase !== "lobby",
          ctx.ui.column({ gap: 10 }, [
            ctx.ui.h2(state.phase === "starting" ? "Starting…" : state.phase === "stopping" ? "Closing…" : `Now playing: ${state.current ? state.current.title : "a game"}`),
            ...playerRows(),
            state.phase === "starting"
              ? ctx.ui.button({ label: "Cancel", variant: "ghost", onClick: () => cancelStart() })
              : ctx.ui.small("Press Home on a phone to quit. Use Swap on the GamePad phone to hand it to someone else."),
          ]),
        ),
      ]);
    };
  },
};
