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

export {
	DEFAULT_PLAYER_PADS,
	DEFAULT_SYSTEM_PAD,
	DOLPHIN_EXECUTABLE,
	DOLPHIN_RELEASE,
	DOLPHIN_RUNTIME_NAME,
	DOLPHIN_USER_DIR,
	DSU_INPUT,
	MOTION_REMOTE_LABELS,
	PLATFORM_LABELS,
	PLAYER_SLOTS,
	SYSTEM_BUTTONS,
	assetIdFromLibraryItem,
	dolphinArgs,
	dolphinFiles,
	dolphinSpec,
	libraryIdFor,
	selectGames,
	selectRuntime,
	splitPadAddress,
};
