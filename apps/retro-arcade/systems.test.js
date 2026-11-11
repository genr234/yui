import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import * as s from "./web/systems.js";
import { withEmbedded } from "./sync.js";

const dir = dirname(fileURLToPath(import.meta.url));
const bit = (...names) => names.reduce((mask, name) => mask | (1 << s.PAD_BITS[name]), 0);

describe("input map", () => {
	test("positions map to RetroPad indices on every system", () => {
		for (const system of s.SYSTEMS) {
			for (const name of system.buttons) expect(s.POSITION_TO_RETROPAD[name]).toBeNumber();
			for (const name of Object.keys(system.labels)) expect(system.buttons).toContain(name);
		}
		expect(s.POSITION_TO_RETROPAD.South).toBe(0); // RetroPad B
		expect(s.POSITION_TO_RETROPAD.East).toBe(8); // RetroPad A
		expect(s.POSITION_TO_RETROPAD.West).toBe(1); // Y
		expect(s.POSITION_TO_RETROPAD.North).toBe(9); // X
	});

	test("sends only changes and releases everything", () => {
		const mapper = s.createInputMapper(s.systemFor("nes"));
		const first = mapper.update({ buttons: bit("South", "Start") });
		expect(first).toContainEqual([0, 1]);
		expect(first).toContainEqual([3, 1]);
		expect(mapper.update({ buttons: bit("South", "Start") })).toEqual([]);
		expect(mapper.update({ buttons: bit("South") })).toEqual([[3, 0]]);
		expect(mapper.release()).toEqual([[0, 0]]);
	});

	test("the stick drives the d-pad on systems without one", () => {
		const mapper = s.createInputMapper(s.systemFor("snes"));
		expect(mapper.update({ buttons: 0, lx: 32767, ly: 0 })).toEqual([[7, 1]]); // right
		expect(mapper.update({ buttons: 0, lx: 0, ly: 32767 })).toEqual(expect.arrayContaining([[7, 0], [4, 1]])); // up
		expect(mapper.update({ buttons: 0, lx: 0, ly: 1000 })).toEqual([[4, 0]]); // inside the dead zone
	});

	test("buttons a system doesn't have are ignored; rewind only when enabled", () => {
		const plain = s.createInputMapper(s.systemFor("nes"));
		expect(plain.update({ buttons: bit("North", "L2") })).toEqual([]);
		const rewinding = s.createInputMapper(s.systemFor("nes"), { rewind: true });
		expect(rewinding.update({ buttons: bit("L2") })).toEqual([[s.RETROPAD.REWIND, 1]]);
	});
});

describe("phones", () => {
	test("presets hide what a system lacks, stick included", () => {
		const preset = s.phonePreset(s.systemFor("nes"));
		expect(preset.labels).toEqual({ South: "B", East: "A" });
		expect(preset.hide).toContain("West");
		expect(preset.hide).toContain("LeftStick");
		expect(preset.hide).not.toContain("Up");
	});

	test("tiles are small SVG data URIs that escape titles", () => {
		const tile = s.tileSvg(`Tom & "Jerry" <3`, "NES");
		expect(tile.startsWith("data:image/svg+xml,")).toBe(true);
		for (const piece of ["&amp;", "&quot;JERRY&quot;", "&lt;3"]) expect(decodeURIComponent(tile)).toContain(piece);
		expect(decodeURIComponent(tile)).not.toContain("<3");
		expect(tile.length).toBeLessThan(2000);
	});
});

describe("ports", () => {
	const players = [
		{ id: "a", slot: 2, connected: true, host: false },
		{ id: "h", slot: 1, connected: true, host: true },
		{ id: "c", slot: 3, connected: true, host: false },
		{ id: "w", slot: null, connected: true, host: false },
	];

	test("host first, then seat order, up to the port count", () => {
		expect(s.assignPorts(players, s.systemFor("nes"))).toEqual({ h: 0, a: 1 });
		expect(s.assignPorts(players, s.systemFor("gb"))).toEqual({ h: 0 });
	});

	test("keeps existing ports and fills gaps", () => {
		expect(s.assignPorts(players, s.systemFor("nes"), { c: 0 })).toEqual({ c: 0, h: 1 });
		const gone = players.map((p) => (p.id === "h" ? { ...p, connected: false } : p));
		expect(s.assignPorts(gone, s.systemFor("nes"), { h: 0, a: 1 })).toEqual({ a: 1, c: 0 });
	});
});

describe("saves", () => {
	test("keys are valid and rotate through three slots", () => {
		const re = /^[a-z0-9][a-z0-9._-]{0,79}$/;
		expect(s.stateKey("42", 1)).toMatch(re);
		expect(s.sramKey("ABC/def")).toMatch(re);
		expect(s.stateKey("--x", 2)).toMatch(re);
		const entries = [
			{ key: s.stateKey("42", 1), updatedAt: "2026-11-09T10:00:00Z" },
			{ key: s.stateKey("42", 2), updatedAt: "2026-11-09T09:00:00Z" },
		];
		expect(s.nextStateSlot("42", entries)).toBe(3);
		entries.push({ key: s.stateKey("42", 3), updatedAt: "2026-11-09T11:00:00Z" });
		expect(s.nextStateSlot("42", entries)).toBe(2); // the oldest
		expect(s.statesFor("42", entries).map((e) => e.key.at(-1))).toEqual(["3", "1", "2"]);
	});
});

describe("library", () => {
	test("picks supported, ready games and counts pending ones", () => {
		const { games, pending } = s.selectGames([
			{ id: 2, title: "Zeta", state: "ready", metadata: { platform: "snes", cover_url: "https://yui.example/c.png" } },
			{ id: 1, title: "Alpha", state: "ready", metadata: { platform: "NES" } },
			{ id: 3, title: "Disc", state: "ready", metadata: { platform: "wii" } },
			{ id: 4, title: "Later", state: "downloading", metadata: { platform: "gba" } },
			{ id: 5, title: "Plain http", state: "ready", metadata: { platform: "gb", cover_url: "http://evil.example/x.png" } },
		]);
		expect(games.map((g) => g.title)).toEqual(["Alpha", "Later", "Plain http", "Zeta"]);
		expect(games[3].cover).toBe("https://yui.example/c.png");
		expect(games[2].cover).toBe("");
		expect(games[1].available).toBe(false);
		expect(pending).toBe(1);
		expect(s.fetchLabel(games[1])).toBe("Downloading…");
		expect(s.fetchLabel(games[0])).toBe("");
	});

	test("lists remote games as pickable, with progress and errors", () => {
		const { games, pending } = s.selectGames([
			{ id: 1, title: "Cloud", state: "remote", byte_size: 100, metadata: { platform: "snes" } },
			{ id: 2, title: "Half", state: "downloading", bytes_done: 50, byte_size: 100, metadata: { platform: "gb" } },
			{ id: 3, title: "Oops", state: "remote", error: "RomM rejected the login", metadata: { platform: "gb" } },
		]);
		expect(pending).toBe(1);
		expect(games.map((g) => [g.state, g.available, g.progress])).toEqual([["remote", false, 0], ["downloading", false, 0.5], ["remote", false, 0]]);
		expect(s.fetchLabel(games[0])).toBe("Downloads when picked");
		expect(s.fetchLabel(games[1])).toBe("Downloading 50%");
		expect(s.fetchLabel(games[2])).toBe("Couldn't download");
		expect(s.libraryIdFor("a/b")).toBe("game-a_b");
	});
});

test("app.yui.js embeds the current systems.js (run bun run sync)", () => {
	const app = readFileSync(join(dir, "app.yui.js"), "utf8");
	expect(withEmbedded(app, readFileSync(join(dir, "web/systems.js"), "utf8"))).toBe(app);
});

describe("embed aspect", () => {
	test("every system has a usable screen aspect, the lobby is portrait", () => {
		for (const system of s.SYSTEMS) {
			const [w, h] = system.aspect.split(":").map(Number);
			expect(w / h).toBeGreaterThan(1);
			expect(w / h).toBeLessThan(2);
		}
		expect(s.embedAspect(s.systemFor("gba"))).toBe("3:2");
		expect(s.embedAspect(s.systemFor("gbc"))).toBe("10:9");
		expect(s.embedAspect(null)).toBe(s.LOBBY_ASPECT);
	});
});
