import { describe, expect, mock, test } from "bun:test";

mock.module("./bridge", () => ({ bridge: { on: () => () => {}, onOpen: () => () => {}, send: async () => null } }));
const t = await import("./calibrate-targets");

describe("calibration targets", () => {
	test("players on an aim step are tracked; leaving it, a bad step or junk is not", () => {
		let s = new Map();
		s = t.applyCalibrate(s, { player_id: "p1", step: "aim-center" }, 0);
		s = t.applyCalibrate(s, { player_id: "p2", step: "aim-top" }, 0);
		expect([...s.values()].map((a) => [a.playerId, a.step])).toEqual([["p1", "aim-center"], ["p2", "aim-top"]]);
		s = t.applyCalibrate(s, { player_id: "p1", step: "" }, 1);
		s = t.applyCalibrate(s, { player_id: "p2", step: "nope" }, 1);
		expect(s.size).toBe(0);
		expect(t.applyCalibrate(s, null, 1)).toBe(s);
		expect(t.applyCalibrate(s, { step: "aim-top" }, 1)).toBe(s);
	});

	test("a phone that goes quiet stops showing", () => {
		const s = t.applyCalibrate(new Map(), { player_id: "p1", step: "aim-right" }, 0);
		expect(t.dropStale(s, 1000)).toBe(s);
		expect(t.dropStale(s, t.CALIBRATE_STALE_MS + 1).size).toBe(0);
	});

	test("before a game runs, the area is the 16:9 band in the middle of a portrait kiosk", () => {
		expect(t.aimArea(null, { w: 1080, h: 1920 })).toEqual({ x: 0, y: (1920 - 607.5) / 2, w: 1080, h: 607.5 });
		const game = { x: 0, y: 656, w: 1080, h: 608 };
		expect(t.aimArea(game, { w: 1080, h: 1920 })).toBe(game);
	});

	test("targets: the centre, and the game's own right and top edges (not the screen's)", () => {
		const area = { x: 0, y: 656, w: 1080, h: 608 };
		expect(t.targetFor("aim-center", area)).toMatchObject({ kind: "point", x: 540, y: 960 });
		expect(t.targetFor("aim-right", area)).toMatchObject({ kind: "edge", x: 1074, y: 656, h: 608 });
		expect(t.targetFor("aim-top", area)).toMatchObject({ kind: "edge", x: 0, y: 662, w: 1080 });
	});

	test("scan: four markers where the phone expects them, as big as fits inside the game", () => {
		const area = { x: 0, y: 656, w: 1080, h: 608 };
		const target = t.targetFor("scan", area);
		expect(target.kind).toBe("scan");
		if (target.kind !== "scan") return;
		const m = target.markers;
		// TL, TR, BR, BL at ±0.8 of the half-width, ±0.7 of the half-height (pad platform/viewpoint.ts).
		expect(m.map((k) => [k.x, k.y])).toEqual([
			[540 - 432, 960 - 212.8],
			[540 + 432, 960 - 212.8],
			[540 + 432, 960 + 212.8],
			[540 - 432, 960 + 212.8],
		].map(([x, y]) => [expect.closeTo(x, 6), expect.closeTo(y, 6)]));
		for (const k of m) {
			expect(k.x - k.size / 2).toBeGreaterThanOrEqual(area.x);
			expect(k.y - k.size / 2).toBeGreaterThanOrEqual(area.y);
			expect(k.x + k.size / 2).toBeLessThanOrEqual(area.x + area.w);
			expect(k.y + k.size / 2).toBeLessThanOrEqual(area.y + area.h);
		}
		expect(m[0].size).toBeCloseTo(1.6 * 91.2, 6);
		// A portrait game fits them too.
		for (const k of t.scanMarkers({ x: 0, y: 0, w: 1080, h: 1920 })) expect(k.x + k.size / 2).toBeLessThanOrEqual(1080);
		expect(t.applyCalibrate(new Map(), { player_id: "p1", step: "scan" }, 0).get("p1")?.step).toBe("scan");
	});
});
