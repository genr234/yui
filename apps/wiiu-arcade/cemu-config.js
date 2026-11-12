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

export {
	AXIS,
	CEMU_EXECUTABLE,
	CEMU_PORTABLE_DIR,
	CEMU_RELEASE,
	CEMU_RUNTIME_NAME,
	CEMU_SAVES,
	DEFAULT_PLAYER_PADS,
	DSU_BUTTON,
	GAMEPAD_LABELS,
	MOTION_REMOTE_LABELS,
	PLAYER_SLOTS,
	REMOTE_SLOTS,
	SCREEN_SLOT,
	SURFACE_HEIGHT,
	SURFACE_ID,
	SURFACE_TITLE,
	SURFACE_WIDTH,
	assetIdFromLibraryItem,
	assignRoles,
	cemuControllerProfile,
	cemuFiles,
	cemuSettings,
	cemuSpec,
	eligibleForScreen,
	libraryIdFor,
	screenCandidates,
	selectGames,
	selectRuntime,
	splitPadAddress,
	swapScreen,
};
