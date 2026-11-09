// Retro Arcade's systems: cores, ports, phone labels and the input map.
// Used by the page (web/) and embedded into app.yui.js (bun run sync).
// RetroPad indices checked against EmulatorJS 4.2.3 (src/emulator.js
// defaultControllers, src/GameManager.js simulateInput).

const EMULATORJS_VERSION = "4.2.3";
const EMULATORJS_RUNTIME = "emulatorjs";

// RetroPad button indices (EmulatorJS simulateInput).
const RETROPAD = {
	B: 0, Y: 1, SELECT: 2, START: 3, UP: 4, DOWN: 5, LEFT: 6, RIGHT: 7,
	A: 8, X: 9, L: 10, R: 11, L2: 12, R2: 13, L3: 14, R3: 15,
	LX_POS: 16, LX_NEG: 17, LY_POS: 18, LY_NEG: 19,
	RX_POS: 20, RX_NEG: 21, RY_POS: 22, RY_NEG: 23,
	REWIND: 28,
};

// Yui input-frame bits (contracts §3) by name.
const PAD_BITS = {
	South: 0, East: 1, West: 2, North: 3, L1: 4, R1: 5, L2: 6, R2: 7,
	Select: 8, Start: 9, L3: 11, R3: 12, Up: 13, Down: 14, Left: 15, Right: 16,
};

// By position, like everywhere in Yui: South is the bottom face button.
const POSITION_TO_RETROPAD = {
	South: RETROPAD.B, East: RETROPAD.A, West: RETROPAD.Y, North: RETROPAD.X,
	L1: RETROPAD.L, R1: RETROPAD.R, L2: RETROPAD.L2, R2: RETROPAD.R2,
	Select: RETROPAD.SELECT, Start: RETROPAD.START, L3: RETROPAD.L3, R3: RETROPAD.R3,
	Up: RETROPAD.UP, Down: RETROPAD.DOWN, Left: RETROPAD.LEFT, Right: RETROPAD.RIGHT,
};

const ALL_BUTTONS = Object.keys(PAD_BITS);

/**
 * id: Yui's metadata.platform; core: EmulatorJS's EJS_core; cores: the
 * core files the runtime needs; ports: how many players at once; labels:
 * what the phone shows; buttons: which positions the system has (the rest
 * are hidden); analog: the left stick goes to the core as a stick (else it
 * drives the d-pad); rewindOn: the free button that holds rewind.
 */
const SYSTEMS = [
	{ id: "nes", name: "NES", core: "nes", cores: ["fceumm"], ports: 2, color: "#c8102e",
		labels: { South: "B", East: "A" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "snes", name: "Super NES", core: "snes", cores: ["snes9x"], ports: 2, color: "#6a5acd",
		labels: { South: "B", East: "A", West: "Y", North: "X", L1: "L", R1: "R" },
		buttons: ["South", "East", "West", "North", "L1", "R1", "Select", "Start"] },
	{ id: "gb", name: "Game Boy", core: "gb", cores: ["gambatte"], ports: 1, color: "#8b956d",
		labels: { South: "B", East: "A" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "gbc", name: "Game Boy Color", core: "gb", cores: ["gambatte"], ports: 1, color: "#7b3fa0",
		labels: { South: "B", East: "A" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "gba", name: "Game Boy Advance", core: "gba", cores: ["mgba"], ports: 1, color: "#4b3c9e",
		labels: { South: "B", East: "A", L1: "L", R1: "R" }, buttons: ["South", "East", "L1", "R1", "Select", "Start"] },
	{ id: "genesis", name: "Mega Drive", core: "segaMD", cores: ["genesis_plus_gx"], ports: 2, color: "#1f1f1f",
		labels: { West: "A", South: "B", East: "C", L1: "X", North: "Y", R1: "Z", Start: "Start", Select: "Mode" },
		buttons: ["West", "South", "East", "L1", "North", "R1", "Start", "Select"], rewindOn: "L2" },
	{ id: "sms", name: "Master System", core: "segaMS", cores: ["smsplus", "genesis_plus_gx"], ports: 2, color: "#d22630",
		labels: { South: "1", East: "2", Start: "Pause" }, buttons: ["South", "East", "Start"], rewindOn: "L2" },
	{ id: "gg", name: "Game Gear", core: "segaGG", cores: ["genesis_plus_gx"], ports: 1, color: "#2b2b2b",
		labels: { South: "1", East: "2", Start: "Start" }, buttons: ["South", "East", "Start"], rewindOn: "L2" },
	{ id: "pce", name: "PC Engine", core: "pce", cores: ["mednafen_pce"], ports: 1, color: "#e8e8e8",
		labels: { South: "II", East: "I", Select: "Select", Start: "Run" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "atari2600", name: "Atari 2600", core: "atari2600", cores: ["stella2014"], ports: 2, color: "#7a4a1d",
		labels: { South: "Fire", Select: "Select", Start: "Reset" }, buttons: ["South", "Select", "Start"], rewindOn: "L2" },
	{ id: "lynx", name: "Lynx", core: "lynx", cores: ["handy"], ports: 1, color: "#3a3a3a",
		labels: { South: "B", East: "A", L1: "Opt 1", R1: "Opt 2", Start: "Pause" }, buttons: ["South", "East", "L1", "R1", "Start"] },
];

const DPAD = ["Up", "Down", "Left", "Right"];

function systemFor(platform) {
	const id = String(platform ?? "").toLowerCase();
	return SYSTEMS.find((system) => system.id === id) ?? null;
}

/** The phone's gamepad preset for a system: labels and hidden buttons. */
function phonePreset(system) {
	const keep = new Set([...system.buttons, ...DPAD]);
	const hide = ALL_BUTTONS.filter((name) => !keep.has(name));
	if (!system.analog) hide.push("LeftStick");
	return { name: "gamepad", labels: { ...system.labels }, hide };
}

/**
 * Turns input frames into simulateInput calls. Keeps each port's last
 * state and returns only what changed: [[index, value], ...].
 */
function createInputMapper(system, options = {}) {
	const deadzone = options.deadzone ?? 0.5;
	const rewind = Boolean(options.rewind && system.rewindOn);
	let last = new Map();

	function wanted(frame) {
		const out = new Map();
		const buttons = Number(frame.buttons) >>> 0;
		const pressed = (name) => ((buttons >>> PAD_BITS[name]) & 1) === 1;
		for (const name of system.buttons) out.set(POSITION_TO_RETROPAD[name], pressed(name) ? 1 : 0);
		const lx = Number(frame.lx || 0) / 32767;
		const ly = Number(frame.ly || 0) / 32767; // +y is up
		if (system.analog) {
			out.set(RETROPAD.LX_POS, lx > 0 ? Math.round(lx * 0x7fff) : 0);
			out.set(RETROPAD.LX_NEG, lx < 0 ? Math.round(-lx * 0x7fff) : 0);
			out.set(RETROPAD.LY_POS, ly < 0 ? Math.round(-ly * 0x7fff) : 0); // RetroPad +y is down
			out.set(RETROPAD.LY_NEG, ly > 0 ? Math.round(ly * 0x7fff) : 0);
		}
		const stick = !system.analog;
		out.set(RETROPAD.UP, pressed("Up") || (stick && ly > deadzone) ? 1 : 0);
		out.set(RETROPAD.DOWN, pressed("Down") || (stick && ly < -deadzone) ? 1 : 0);
		out.set(RETROPAD.LEFT, pressed("Left") || (stick && lx < -deadzone) ? 1 : 0);
		out.set(RETROPAD.RIGHT, pressed("Right") || (stick && lx > deadzone) ? 1 : 0);
		if (rewind) out.set(RETROPAD.REWIND, pressed(system.rewindOn) ? 1 : 0);
		return out;
	}

	return {
		/** Changes for one frame. */
		update(frame) {
			const next = wanted(frame);
			const changes = [];
			for (const [index, value] of next) if ((last.get(index) ?? 0) !== value) changes.push([index, value]);
			last = next;
			return changes;
		},
		/** Releases everything held (port reassigned, pause, disconnect). */
		release() {
			const changes = [];
			for (const [index, value] of last) if (value !== 0) changes.push([index, 0]);
			last = new Map();
			return changes;
		},
	};
}

/** A stable hue from a title, for generated tiles. */
function titleHue(title) {
	let hash = 2166136261;
	for (const ch of String(title)) hash = Math.imul(hash ^ ch.charCodeAt(0), 16777619) >>> 0;
	return hash % 360;
}

const escapeXml = (text) => String(text).replace(/[<>&"']/g, (ch) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&#39;" })[ch]);

/** A small SVG cartridge tile as a data URI (for phones; no canvas needed). */
function tileSvg(title, systemName) {
	const hue = titleHue(title);
	const words = String(title).toUpperCase().split(/\s+/).filter(Boolean);
	const lines = [];
	for (const word of words) {
		const lastLine = lines[lines.length - 1];
		if (lastLine && (lastLine + " " + word).length <= 12) lines[lines.length - 1] = lastLine + " " + word;
		else lines.push(word.slice(0, 14));
		if (lines.length === 3) break;
	}
	const text = lines
		.map((line, i) => `<text x="80" y="${78 + i * 20 - (lines.length - 1) * 10}" font-family="monospace" font-weight="700" font-size="15" fill="#fff" text-anchor="middle">${escapeXml(line)}</text>`)
		.join("");
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160" viewBox="0 0 160 160"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${hue} 70% 52%)"/><stop offset="1" stop-color="hsl(${(hue + 40) % 360} 65% 28%)"/></linearGradient></defs><rect width="160" height="160" rx="14" fill="url(#g)"/><rect x="10" y="10" width="140" height="18" rx="5" fill="rgba(0,0,0,.28)"/><text x="80" y="23" font-family="monospace" font-size="11" fill="#fff" text-anchor="middle">${escapeXml(String(systemName).toUpperCase())}</text>${text}</svg>`;
	return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

/** Save keys: `[a-z0-9][a-z0-9._-]{0,79}` (ctx.saves). */
function saveKeyBase(assetId) {
	return String(assetId).toLowerCase().replace(/[^a-z0-9._-]/g, "_").replace(/^[^a-z0-9]+/, "g").slice(0, 60);
}

function stateKey(assetId, n) {
	return `${saveKeyBase(assetId)}.state.${n}`;
}

function sramKey(assetId) {
	return `${saveKeyBase(assetId)}.sram`;
}

/** Newest save states first, from a ctx.saves.list() result. */
function statesFor(assetId, entries) {
	const prefix = `${saveKeyBase(assetId)}.state.`;
	return (entries || [])
		.filter((entry) => entry.key.startsWith(prefix))
		.sort((a, b) => String(b.updatedAt).localeCompare(String(a.updatedAt)));
}

/** The slot to write next: an empty one of 1-3, else the oldest. */
function nextStateSlot(assetId, entries) {
	const states = statesFor(assetId, entries);
	for (let n = 1; n <= 3; n++) if (!states.some((entry) => entry.key === stateKey(assetId, n))) return n;
	const oldest = states[states.length - 1];
	return Number(oldest.key.split(".").pop()) || 1;
}

/**
 * Ports for the players: the host first, then the others in seat order,
 * keeping anyone who already has a port. Returns { playerId: port (0-based) }.
 */
function assignPorts(players, system, current = {}) {
	const connected = players.filter((player) => player.connected !== false && player.slot);
	const ports = {};
	const taken = new Set();
	for (const player of connected) {
		const port = current[player.id];
		if (port !== undefined && port < system.ports && !taken.has(port)) {
			ports[player.id] = port;
			taken.add(port);
		}
	}
	const order = [...connected].sort((a, b) => Number(b.host) - Number(a.host) || (a.slot ?? 9) - (b.slot ?? 9));
	for (const player of order) {
		if (ports[player.id] !== undefined) continue;
		let port = 0;
		while (taken.has(port)) port++;
		if (port >= system.ports) break;
		ports[player.id] = port;
		taken.add(port);
	}
	return ports;
}

/** Games out of ctx.assets("game"). */
function selectGames(assets) {
	const games = [];
	let pending = 0;
	for (const asset of Array.isArray(assets) ? assets : []) {
		const meta = asset?.metadata ?? {};
		const system = systemFor(meta.platform);
		if (!system) continue;
		if (asset.state !== undefined && asset.state !== "ready") {
			pending += 1;
			continue;
		}
		const title = String(asset.title || meta.title || "Untitled game");
		const cover = typeof meta.cover_url === "string" && /^(https:\/\/|http:\/\/(localhost|127\.0\.0\.1)[:/])/.test(meta.cover_url) ? meta.cover_url : "";
		games.push({ id: String(asset.id), title, system: system.id, systemName: system.name, cover, fromLink: Boolean(meta.source && meta.source.provider) });
	}
	games.sort((a, b) => a.title.localeCompare(b.title) || a.id.localeCompare(b.id));
	return { games, pending };
}

/** Library title ids match [A-Za-z0-9][A-Za-z0-9._:-]{0,127}. */
function libraryIdFor(assetId) {
	return `game-${String(assetId).replace(/[^A-Za-z0-9._:-]/g, "_")}`.slice(0, 128);
}

export {
	EMULATORJS_VERSION, EMULATORJS_RUNTIME, RETROPAD, PAD_BITS, POSITION_TO_RETROPAD, SYSTEMS,
	systemFor, phonePreset, createInputMapper, titleHue, tileSvg, saveKeyBase, stateKey, sramKey,
	statesFor, nextStateSlot, assignPorts, selectGames, libraryIdFor,
};
