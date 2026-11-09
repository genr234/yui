import { describe, expect, test } from "bun:test";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import * as config from "./dolphin-config.js";
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
const systemPad = "127.0.0.1:26761/0";

describe("dolphin config golden files", () => {
	const files = config.dolphinFiles({ pads, systemPad });
	const byPath = Object.fromEntries(files.map((f) => [f.path, f.content]));

	test("writes the four config files under the user dir", () => {
		expect(files.map((f) => f.path)).toEqual([
			"User/Config/Dolphin.ini",
			"User/Config/DSUClient.ini",
			"User/Config/WiimoteNew.ini",
			"User/Config/Hotkeys.ini",
		]);
	});

	for (const file of files) {
		test(`golden ${file.path}`, () => golden(file.path.split("/").pop(), file.content));
	}

	test("golden with a Nunchuk attached", () => {
		const nunchuk = config.dolphinFiles({ pads, systemPad, extension: "Nunchuk" });
		golden("WiimoteNew.nunchuk.ini", nunchuk.find((f) => f.path.endsWith("WiimoteNew.ini")).content);
	});

	test("one DSU server entry per host:port, system pad separate", () => {
		expect(byPath["User/Config/DSUClient.ini"]).toContain("Entries = Yui 1:127.0.0.1:26760;Yui 2:127.0.0.1:26761;");
	});

	test("wiimotes bind DSUClient 0..3 with motion", () => {
		const text = byPath["User/Config/WiimoteNew.ini"];
		for (let i = 0; i < 4; i++) expect(text).toContain(`[Wiimote${i + 1}]\nDevice = DSUClient/${i}/Yui 1\nSource = 1`);
		expect(text).toContain("IMUAccelerometer/Up = `Accel Up`");
		expect(text).toContain("IMUGyroscope/Yaw Right = `Gyro Yaw Right`");
		expect(text).toContain("IMUIR/Recenter = `Touch Button`");
	});

	test("hotkeys use the system pad, including Connect Wii Remote 1-4", () => {
		const text = byPath["User/Config/Hotkeys.ini"];
		expect(text).toContain("Device = DSUClient/0/Yui 2");
		for (let i = 1; i <= 4; i++) expect(text).toContain(`Wii/Connect Wii Remote ${i} = `);
	});

	test("fullscreen, no confirm on stop, analytics off", () => {
		const text = byPath["User/Config/Dolphin.ini"];
		expect(text).toContain("Fullscreen = True");
		expect(text).toContain("ConfirmStop = False");
		expect(text).toContain("[Analytics]\nEnabled = False");
	});

	test("player pads on another host and port still split correctly", () => {
		const moved = config.dolphinFiles({ pads: ["10.0.0.5:9000/0"], systemPad: "10.0.0.5:9001/0" });
		const dsu = moved.find((f) => f.path.endsWith("DSUClient.ini")).content;
		expect(dsu).toContain("Entries = Yui 1:10.0.0.5:9000;Yui 2:10.0.0.5:9001;");
		// Missing slots reuse the first pad's server.
		expect(dsu).not.toContain("127.0.0.1");
		const defaults = config.dolphinFiles({});
		expect(defaults.find((f) => f.path.endsWith("DSUClient.ini")).content).toContain("Yui 1:127.0.0.1:26760;Yui 2:127.0.0.1:26761;");
	});

	test("rejects a bad address", () => {
		expect(() => config.dolphinFiles({ pads: ["nonsense"], systemPad })).toThrow();
		expect(config.splitPadAddress("127.0.0.1:26760/2")).toEqual({ host: "127.0.0.1", port: 26760, slot: 2 });
		expect(config.splitPadAddress("127.0.0.1:99999/0")).toBeNull();
	});
});

describe("launch spec", () => {
	test("args and spec", () => {
		const spec = config.dolphinSpec({ runtime: 7, gameAsset: 12, stateFor: "RMCE01", pads, systemPad });
		expect(spec.args).toEqual([
			"-u",
			"{{dataDir}}/User",
			"-b",
			"-s",
			"{{dataDir}}/User/StateSaves/RMCE01.s01",
			"-e",
			"{{asset:12}}",
		]);
		expect(spec.runtime).toBe("7");
		expect(spec.audio).toBe("speaker");
		expect(spec.virtualPads).toEqual([0, 1, 2, 3]);
		expect(config.dolphinSpec({ runtime: 7, gameAsset: 12 }).args).not.toContain("-s");
	});
});

describe("preset labels", () => {
	test("motion-remote labels", () => {
		expect(config.MOTION_REMOTE_LABELS).toEqual({
			South: "A",
			R2: "B",
			West: "1",
			North: "2",
			Select: "−",
			Start: "+",
			L2: "Z",
			L1: "C",
		});
	});
});

describe("game and runtime selection", () => {
	const asset = (id, kind, metadata, extra = {}) => ({ id, kind, title: `T${id}`, metadata, state: "ready", ...extra });

	test("keeps wii and gamecube games, counts unfinished ones", () => {
		const picked = config.selectGames([
			asset("1", "game", { platform: "wii", cover_url: "https://yui.example/c.png", game_id: "RMCE01" }),
			asset("2", "game", { platform: "gamecube" }),
			asset("3", "game", { platform: null }),
			asset("4", "game", { platform: "wii" }, { state: "downloading" }),
			asset("5", "game", { platform: "wii", cover_url: "javascript:alert(1)" }),
		]);
		expect(picked.ready.map((g) => g.id)).toEqual(["1", "2", "5"]);
		expect(picked.pending).toBe(1);
		expect(picked.ready[0]).toMatchObject({ cover: "https://yui.example/c.png", gameId: "RMCE01", platformLabel: "Wii" });
		expect(picked.ready[2].cover).toBe("");
	});

	test("prefers the pinned runtime, else the newest ready one", () => {
		const dolphin = (id, version, state = "ready") => asset(id, "runtime", { runtime: "dolphin", version }, { state });
		expect(config.selectRuntime([])).toBeNull();
		expect(config.selectRuntime([asset("9", "runtime", { runtime: "other", version: "1" })])).toBeNull();
		expect(config.selectRuntime([dolphin("1", "2506"), dolphin("2", config.DOLPHIN_RELEASE), dolphin("3", "2612")])).toEqual({
			id: "2",
			version: config.DOLPHIN_RELEASE,
			pinned: true,
		});
		expect(config.selectRuntime([dolphin("1", "2506"), dolphin("3", "2603"), dolphin("4", "2701", "downloading")])).toEqual({
			id: "3",
			version: "2603",
			pinned: false,
		});
	});

	test("library ids", () => {
		expect(config.libraryIdFor("12")).toBe("asset-12");
		expect(config.libraryIdFor("a b")).toBe("asset-a_b");
		expect(config.assetIdFromLibraryItem({ id: "asset-12", resume: {} })).toBe("12");
		expect(config.assetIdFromLibraryItem({ id: "x", resume: { assetId: 5 } })).toBe("5");
		expect(config.assetIdFromLibraryItem({ id: "x" })).toBe("");
	});
});

describe("app.yui.js", () => {
	test("embeds the current dolphin-config.js (run `bun run sync` after editing it)", () => {
		const app = readFileSync(join(dir, "app.yui.js"), "utf8");
		const module = readFileSync(join(dir, "dolphin-config.js"), "utf8");
		expect(withEmbedded(app, module)).toBe(app);
	});

	test("declares the permissions and controllers", () => {
		const app = readFileSync(join(dir, "app.yui.js"), "utf8");
		const match = app.match(/permissions\s*:\s*\[([\s\S]*?)\]/);
		const permissions = [...match[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
		expect(permissions.sort()).toEqual(
			["assets:game", "assets:runtime", "audio.speaker", "controllers", "library", "native", "virtual-pads"].sort(),
		);
		expect(app).toContain("controllers: {}");
	});
});
