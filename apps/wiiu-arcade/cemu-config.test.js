import { describe, expect, test } from "bun:test";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import * as config from "./cemu-config.js";
import { withEmbedded } from "./sync.js";

const dir = dirname(fileURLToPath(import.meta.url));
const goldenDir = join(dir, "golden");

// UPDATE_GOLDEN=1 bun test rewrites the golden files.
function golden(name, actual) {
	const path = join(goldenDir, name);
	if (process.env.UPDATE_GOLDEN === "1" || !existsSync(path)) {
		mkdirSync(goldenDir, { recursive: true });
		writeFileSync(path, actual);
	}
	expect(actual).toBe(readFileSync(path, "utf8"));
}

const pads = ["127.0.0.1:26760/0", "127.0.0.1:26760/1", "127.0.0.1:26760/2", "127.0.0.1:26760/3"];

describe("cemu config golden files", () => {
	const files = config.cemuFiles({ pads });
	const byPath = Object.fromEntries(files.map((f) => [f.path, f.content]));

	test("writes settings.xml and four controller profiles under the portable folder", () => {
		expect(files.map((f) => f.path)).toEqual([
			"runtime/portable/settings.xml",
			"runtime/portable/controllerProfiles/controller0.xml",
			"runtime/portable/controllerProfiles/controller1.xml",
			"runtime/portable/controllerProfiles/controller2.xml",
			"runtime/portable/controllerProfiles/controller3.xml",
		]);
	});

	for (const file of files) {
		test(`golden ${file.path}`, () => golden(file.path.split("/").pop(), file.content));
	}

	test("golden with a Nunchuk attached", () => {
		golden("controller1.nunchuk.xml", config.cemuControllerProfile(1, { pads, extension: "Nunchuk" }));
	});

	test("slot 0 is the GamePad, 1 to 3 are Wii Remotes with MotionPlus", () => {
		expect(byPath["runtime/portable/controllerProfiles/controller0.xml"]).toContain("<type>Wii U GamePad</type>");
		for (let i = 1; i < 4; i++) {
			const text = byPath[`runtime/portable/controllerProfiles/controller${i}.xml`];
			expect(text).toContain("<type>Wiimote</type>");
			expect(text).toContain("<device_type>5</device_type>");
			expect(text).toContain(`<uuid>${i}</uuid>`);
		}
	});

	test("every profile reads DSU with motion on and its own server address", () => {
		for (let i = 0; i < 4; i++) {
			const text = byPath[`runtime/portable/controllerProfiles/controller${i}.xml`];
			expect(text).toContain("<api>DSUController</api>");
			expect(text).toContain("<motion>true</motion>");
			expect(text).toContain("<ip>127.0.0.1</ip>");
			expect(text).toContain("<port>26760</port>");
		}
		const moved = config.cemuControllerProfile(2, { pads: ["10.0.0.5:9000/0", null, "10.0.0.5:9000/2"] });
		expect(moved).toContain("<ip>10.0.0.5</ip>");
		expect(moved).toContain("<port>9000</port>");
		expect(() => config.cemuControllerProfile(4)).toThrow();
		expect(() => config.cemuControllerProfile(0, { pads: ["nope"] })).toThrow();
	});

	test("GamePad mappings: face buttons by position, A is the right face button", () => {
		const text = byPath["runtime/portable/controllerProfiles/controller0.xml"];
		// A (1) <- East (DSU 13), B (2) <- South (14), X (3) <- North (12), Y (4) <- West (15)
		expect(text).toContain("<mapping>1</mapping>\n\t\t\t\t<button>13</button>");
		expect(text).toContain("<mapping>2</mapping>\n\t\t\t\t<button>14</button>");
		expect(text).toContain("<mapping>3</mapping>\n\t\t\t\t<button>12</button>");
		expect(text).toContain("<mapping>4</mapping>\n\t\t\t\t<button>15</button>");
		expect((text.match(/<entry>/g) || []).length).toBe(24);
	});

	test("settings: no first-run dialogs, English, a 854x480 pad window, async shaders", () => {
		const text = byPath["runtime/portable/settings.xml"];
		for (const key of ["check_update>false", "vk_warning>true", "gp_download>true", "open_pad>true", "fullscreen>true"]) expect(text).toContain(key);
		expect(text).toContain("<pad_size>\n\t\t<x>854</x>\n\t\t<y>480</y>");
		expect(text).toContain("<AsyncCompile>true</AsyncCompile>");
		expect(text).toContain('<DSUC host="127.0.0.1" port="26760"/>');
	});

	test("every UNVERIFIED guess is marked in the source", () => {
		const source = readFileSync(join(dir, "cemu-config.js"), "utf8");
		expect(source.match(/UNVERIFIED until S2/g).length).toBeGreaterThanOrEqual(5);
	});
});

describe("cemu spec", () => {
	test("private install, WUA path, surface and saves", () => {
		const spec = config.cemuSpec({ runtime: "7", gameAsset: "12", pads });
		expect(spec.runtime).toBe("7");
		expect(spec.install).toBe("private");
		expect(spec.executable).toBe("runtime/Cemu.exe");
		expect(spec.args).toEqual(["-g", "{{asset:12}}", "-f"]);
		expect(spec.virtualPads).toEqual([0, 1, 2, 3]);
		expect(spec.audio).toBe("speaker");
		expect(spec.saves).toEqual(["runtime/portable/mlc01/usr/save"]);
		expect(spec.surfaces).toEqual([{ id: "second-screen", window: { title: "(?i)gamepad" }, hidden: true, width: 854, height: 480 }]);
		expect(spec.files.length).toBe(5);
		expect(() => config.cemuSpec({ gameAsset: "1" })).toThrow();
		expect(() => config.cemuSpec({ runtime: "1" })).toThrow();
	});
});

const asset = (id, kind, metadata, state = "ready", title = "") => ({ id, kind, state, title, metadata });

describe("selection", () => {
	test("selectGames keeps ready Wii U titles, counts pending and unplayable ones", () => {
		const picked = config.selectGames([
			asset("1", "game", { platform: "wiiu", format: "wua", playable: true, title_id: "0005000010101234" }, "ready", "Nintendo Land"),
			asset("2", "game", { platform: "wiiu", format: "wua" }, "downloading", "Later"),
			asset("3", "game", { platform: "wiiu", format: "wud", playable: false }, "ready", "Disc dump"),
			asset("4", "game", { platform: "wii" }, "ready", "Wii game"),
			asset("5", "game", { platform: null }, "ready", "Unknown"),
			asset("6", "game", { platform: "wiiu", format: "wua", cover_url: "https://x/c.jpg" }, "ready", "A Game"),
		]);
		expect(picked.ready.map((g) => g.title)).toEqual(["A Game", "Nintendo Land"]);
		expect(picked.ready[1].titleId).toBe("0005000010101234");
		expect(picked.ready[0].cover).toBe("https://x/c.jpg");
		expect(picked.pending).toBe(1);
		expect(picked.unplayable).toBe(1);
		expect(config.selectGames(undefined)).toEqual({ ready: [], pending: 0, unplayable: 0 });
	});

	test("selectRuntime prefers the pinned version, per runtime name", () => {
		const cemu = (id, version, state = "ready") => asset(id, "runtime", { runtime: "cemu", version }, state);
		expect(config.selectRuntime([])).toBeNull();
		expect(config.selectRuntime([asset("9", "runtime", { runtime: "ffmpeg", version: "8.0" })])).toBeNull();
		expect(config.selectRuntime([cemu("1", "2.4"), cemu("2", "2.6"), cemu("3", "2.7")])).toEqual({ id: "2", version: "2.6", pinned: true });
		expect(config.selectRuntime([cemu("1", "2.4"), cemu("3", "2.5"), cemu("4", "2.7", "downloading")])).toEqual({ id: "3", version: "2.5", pinned: false });
		expect(config.selectRuntime([asset("5", "runtime", { runtime: "ffmpeg", version: "8.0" })], "ffmpeg", "8.0")).toEqual({ id: "5", version: "8.0", pinned: true });
	});

	test("library ids", () => {
		expect(config.libraryIdFor("12")).toBe("asset-12");
		expect(config.libraryIdFor("a b")).toBe("asset-a_b");
		expect(config.assetIdFromLibraryItem({ id: "asset-12", resume: {} })).toBe("12");
		expect(config.assetIdFromLibraryItem({ id: "x", resume: { assetId: 5 } })).toBe("5");
		expect(config.assetIdFromLibraryItem({ id: "x" })).toBe("");
	});
});

const player = (id, extra = {}) => ({ id, connected: true, host: false, caps: { motion: true, haptics: false, video: true }, ...extra });

describe("roles", () => {
	test("eligibleForScreen needs a connection and caps.video", () => {
		expect(config.eligibleForScreen(player("a"))).toBe(true);
		expect(config.eligibleForScreen(player("a", { connected: false }))).toBe(false);
		expect(config.eligibleForScreen(player("a", { caps: { motion: true, haptics: false, video: false } }))).toBe(false);
		expect(config.eligibleForScreen(player("a", { caps: undefined }))).toBe(false);
		expect(config.eligibleForScreen(null)).toBe(false);
	});

	test("the host gets the GamePad, others take Wii Remotes in order, host first", () => {
		const roles = config.assignRoles([player("b"), player("h", { host: true }), player("c"), player("d")]);
		expect(roles.screen).toBe("h");
		expect(roles.remotes).toEqual({ b: 1, c: 2, d: 3 });
	});

	test("a direct link beats the host; an unknown link ranks between", () => {
		expect(config.assignRoles([player("h", { host: true, relayed: true }), player("b", { relayed: false })]).screen).toBe("b");
		expect(config.assignRoles([player("h", { host: true }), player("b", { relayed: false })]).screen).toBe("b");
		expect(config.assignRoles([player("h", { host: true, relayed: true }), player("b")]).screen).toBe("b");
		expect(config.assignRoles([player("h", { host: true, relayed: false }), player("b", { relayed: false })]).screen).toBe("h");
	});

	test("a phone without video never gets the GamePad; nobody eligible means no screen", () => {
		const noVideo = { motion: true, haptics: false, video: false };
		expect(config.assignRoles([player("h", { host: true, caps: noVideo }), player("b")]).screen).toBe("b");
		const none = config.assignRoles([player("h", { host: true, caps: noVideo })]);
		expect(none.screen).toBeNull();
		expect(none.remotes).toEqual({ h: 1 });
	});

	test("at most three Wii Remotes; the fifth is left out", () => {
		const roles = config.assignRoles([player("h", { host: true }), player("a"), player("b"), player("c"), player("d")]);
		expect(Object.keys(roles.remotes)).toEqual(["a", "b", "c"]);
	});

	test("previous roles stay put; a gone holder is replaced, slots are kept", () => {
		const players = [player("h", { host: true }), player("a"), player("b")];
		const first = config.assignRoles(players);
		expect(first).toEqual({ screen: "h", remotes: { a: 1, b: 2 } });
		const second = config.assignRoles([player("h", { host: true, connected: false }), player("a"), player("b"), player("c")], first);
		expect(second.screen).toBe("a");
		expect(second.remotes).toEqual({ b: 2, c: 1 });
		const stable = config.assignRoles([player("b"), player("h", { host: true }), player("a")], first);
		expect(stable).toEqual(first);
	});

	test("swapScreen: the old holder takes the slot the new one frees", () => {
		const roles = { screen: "h", remotes: { a: 1, b: 2 } };
		expect(config.swapScreen(roles, "b")).toEqual({ screen: "b", remotes: { a: 1, h: 2 } });
		expect(config.swapScreen(roles, "h")).toBe(roles);
		expect(config.swapScreen(roles, "")).toBe(roles);
		expect(roles).toEqual({ screen: "h", remotes: { a: 1, b: 2 } });
	});

	test("swapScreen to a watcher gives the old holder a free slot, or none when full", () => {
		expect(config.swapScreen({ screen: "h", remotes: { a: 1 } }, "w")).toEqual({ screen: "w", remotes: { a: 1, h: 2 } });
		expect(config.swapScreen({ screen: "h", remotes: { a: 1, b: 2, c: 3 } }, "w")).toEqual({ screen: "w", remotes: { a: 1, b: 2, c: 3 } });
	});

	test("screenCandidates leaves out the excluded and ineligible", () => {
		const list = [player("a"), player("b", { connected: false }), player("c")];
		expect(config.screenCandidates(list, "a").map((p) => p.id)).toEqual(["c"]);
	});
});

describe("app.yui.js", () => {
	test("embeds the current cemu-config.js (run `bun run sync` after editing it)", () => {
		const app = readFileSync(join(dir, "app.yui.js"), "utf8");
		const module = readFileSync(join(dir, "cemu-config.js"), "utf8");
		expect(withEmbedded(app, module)).toBe(app);
	});

	test("declares the permissions and controllers", () => {
		const app = readFileSync(join(dir, "app.yui.js"), "utf8");
		const match = app.match(/permissions\s*:\s*\[([\s\S]*?)\]/);
		const permissions = [...match[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
		expect(permissions.sort()).toEqual(
			["assets:game", "assets:runtime", "audio.speaker", "controllers", "library", "native", "surfaces", "virtual-pads"].sort(),
		);
		expect(app).toContain("controllers: {}");
		expect(app).toContain('id: "dev.genr.wiiu-arcade"');
	});

	test("stays generic: no special platform names outside the app folder is a plan rule, so just parse", () => {
		const app = readFileSync(join(dir, "app.yui.js"), "utf8");
		expect(() => new Function(app.replace("export default", "return"))).not.toThrow();
	});
});
