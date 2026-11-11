import { describe, expect, test } from "bun:test";
import type { PadPlayer } from "../../sdk/pad";
import { JOIN_RESERVE_PX, chipLayout, joinVisible, pickedBy, seated, spectators, swatchOf } from "./chips";

const player = (id: string, slot: number | null, extra: Partial<PadPlayer> = {}): PadPlayer => ({
	id, slot, name: id, color: "blue", host: false, connected: true, ...extra,
});
const session = (lobby_open: boolean) => ({ code: "ABCD", join_url: "https://yui.example/play/ABCD?t=1", lobby_open });

describe("chipLayout", () => {
	test("no seats, no grid", () => {
		expect(chipLayout(0, 540, 280)).toMatchObject({ cols: 0, rows: 0 });
	});

	test("two chips share one row", () => {
		const l = chipLayout(2, 540, 280, JOIN_RESERVE_PX);
		expect(l).toMatchObject({ cols: 2, rows: 1 });
		expect(l.w * 2 + l.gap).toBeLessThanOrEqual(540 - 32 - JOIN_RESERVE_PX);
	});

	test("four chips on a narrow portrait bar wrap to 2x2, and always fit the width", () => {
		const l = chipLayout(4, 540, 280, JOIN_RESERVE_PX);
		expect(l).toMatchObject({ cols: 2, rows: 2 });
		expect(l.w * l.cols + l.gap * (l.cols - 1)).toBeLessThanOrEqual(540 - 32 - JOIN_RESERVE_PX + 0.001);
		expect(l.h * 2 + l.gap).toBeLessThan(280); // room left above for balloons
	});

	test("four chips fit one row on a wide bar", () => {
		expect(chipLayout(4, 1400, 300, JOIN_RESERVE_PX)).toMatchObject({ cols: 4, rows: 1 });
	});

	test("chips are capped in width and keep a minimum height", () => {
		expect(chipLayout(1, 2000, 300).w).toBe(230);
		expect(chipLayout(4, 540, 60, JOIN_RESERVE_PX).h).toBe(48);
	});
});

describe("roster helpers", () => {
	const all = [player("c", 3), player("a", 1, { host: true, name: "Mia", color: "red" }), player("w", null), player("b", 2)];

	test("seated is in slot order, spectators are the rest", () => {
		expect(seated(all).map((p) => p.id)).toEqual(["a", "b", "c"]);
		expect(spectators(all).map((p) => p.id)).toEqual(["w"]);
	});

	test("picked by the host's name", () => {
		expect(pickedBy(all)).toBe("Mia");
		expect(pickedBy([player("x", 1)])).toBeNull();
	});

	test("colours come from the pad swatches", () => {
		expect(swatchOf("red")).toBe("#d84a4a");
		expect(swatchOf("nope")).toBe("#2f76dc");
		expect(swatchOf(undefined)).toBe("#2f76dc");
	});
});

describe("joinVisible", () => {
	const full = [player("a", 1), player("b", 2), player("c", 3), player("d", 4)];
	test("no session: hidden", () => {
		expect(joinVisible(null, [])).toBe(false);
	});
	test("lobby open: shown even with a full table (watchers can still join)", () => {
		expect(joinVisible(session(true), full)).toBe(true);
	});
	test("lobby closed: shown only while a seat is free", () => {
		expect(joinVisible(session(false), full.slice(0, 3))).toBe(true);
		expect(joinVisible(session(false), full)).toBe(false);
	});
	test("spectators do not take seats", () => {
		expect(joinVisible(session(false), [...full.slice(0, 3), player("w", null)])).toBe(true);
	});
});
