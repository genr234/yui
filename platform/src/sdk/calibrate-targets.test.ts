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

	test("scan: one code in the middle of the game, as big as fits with its margin, its text where it is", () => {
		for (const area of [
			{ x: 0, y: 656, w: 1080, h: 608 },
			{ x: 0, y: 0, w: 1080, h: 1920 },
		]) {
			const code = t.targetFor("scan", area);
			expect(code.kind).toBe("scan");
			if (code.kind !== "scan") return;
			const c = code.code;
			const m = 0.14 * c.w;
			expect(c.x - m).toBeGreaterThanOrEqual(area.x);
			expect(c.y - m).toBeGreaterThanOrEqual(area.y);
			expect(c.x + c.w + m).toBeLessThanOrEqual(area.x + area.w);
			expect(c.y + c.h + m).toBeLessThanOrEqual(area.y + area.h);
			expect(Math.abs(c.w - c.h)).toBeLessThan(1); // square, to within rounding
			// The text is the truth: its corners, back in px, are where the code is drawn.
			const [x0, y0, x1, y1] = c.text.slice(t.AIM_CODE_PREFIX.length).split(":").map(Number);
			const cx = area.x + area.w / 2;
			const cy = area.y + area.h / 2;
			expect(cx + (x0 * area.w) / 2).toBeCloseTo(c.x, 6);
			expect(cy + (y0 * area.h) / 2).toBeCloseTo(c.y, 6);
			expect(cx + (x1 * area.w) / 2).toBeCloseTo(c.x + c.w, 6);
			expect(cy + (y1 * area.h) / 2).toBeCloseTo(c.y + c.h, 6);
			// QR alphanumeric mode: digits, capitals and . - : only.
			expect(c.text).toMatch(/^[0-9A-Z.:-]+$/);
		}
		expect(t.applyCalibrate(new Map(), { player_id: "p1", step: "scan" }, 0).get("p1")?.step).toBe("scan");
	});

	test("the board: one target each, players listed in slot order with their colours", () => {
		let s = new Map();
		s = t.applyCalibrate(s, { player_id: "c", step: "scan" }, 0);
		s = t.applyCalibrate(s, { player_id: "a", step: "scan" }, 0);
		s = t.applyCalibrate(s, { player_id: "b", step: "aim-center" }, 0);
		s = t.applyCalibrate(s, { player_id: "d", step: "aim-right" }, 0);
		const roster = [
			{ id: "a", slot: 1, name: "Ada", color: "red" },
			{ id: "b", slot: 2, name: "Bo", color: "blue" },
			{ id: "c", slot: 3, name: "Cy", color: "green" },
			{ id: "d", slot: null, name: "Di", color: "pink" },
		];
		const board = t.boardFor(s, roster, { red: "#d84a4a", blue: "#2f76dc", green: "#1f9d68", pink: "#d64f95" });
		expect(board.scan.map((p) => p.label)).toEqual(["P1", "P3"]);
		expect(board.center).toEqual([{ id: "b", label: "P2", color: "#2f76dc" }]);
		expect(board.right).toEqual([{ id: "d", label: "Di", color: "#d64f95" }]);
		expect(board.top).toEqual([]);
	});

	test("captions sit beside the code on a wide game, above and below it on a tall one", () => {
		const wide = { x: 0, y: 656, w: 1080, h: 608 };
		const sw = t.captionSpots(wide, t.aimCode(wide));
		expect(sw.first.x).toBeLessThan(540);
		expect(sw.second.x).toBeGreaterThan(540);
		expect(sw.first.y).toBe(960);
		const tall = { x: 0, y: 0, w: 1080, h: 1920 };
		const st = t.captionSpots(tall, t.aimCode(tall));
		expect(st.first.y).toBeLessThan(960);
		expect(st.second.y).toBeGreaterThan(960);
	});
});
