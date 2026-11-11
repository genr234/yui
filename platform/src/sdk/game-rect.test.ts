import { describe, expect, mock, test } from "bun:test";

const listeners = new Map<string, (data: unknown) => void>();
mock.module("./bridge", () => ({
	bridge: {
		on: (event: string, fn: (data: unknown) => void) => {
			listeners.set(event, fn);
			return () => listeners.delete(event);
		},
	},
}));

const g = await import("./game-rect");
const { registerBarItem, barItems } = await import("../components/bars/registry");
const { get } = await import("svelte/store");

describe("aspect maths", () => {
	test("parses ratios", () => {
		expect(g.parseAspect("16:9")).toBeCloseTo(16 / 9);
		expect(g.parseAspect("4:3")).toBeCloseTo(4 / 3);
		expect(g.parseAspect(1.5)).toBe(1.5);
		expect(g.parseAspect("wide")).toBeUndefined();
		expect(g.parseAspect(0)).toBeUndefined();
		expect(g.parseAspect("0:9")).toBeUndefined();
	});

	test("16:9 in a 540x960 portrait area is centred with room above and below", () => {
		const fit = g.fitAspect({ x: 0, y: 0, w: 540, h: 960 }, 16 / 9);
		expect(fit.w).toBe(540);
		expect(fit.h).toBeCloseTo(303.75);
		expect(fit.y).toBeCloseTo((960 - 303.75) / 2);
	});

	test("a wide area is limited by height", () => {
		const fit = g.fitAspect({ x: 10, y: 20, w: 1000, h: 300 }, 4 / 3);
		expect(fit.h).toBe(300);
		expect(fit.w).toBe(400);
		expect(fit.x).toBe(310);
		expect(fit.y).toBe(20);
	});
});

describe("native.window", () => {
	test("physical pixels become CSS px by the width ratio; null and junk are no rect", () => {
		const screen = { w: 1080, h: 1920 };
		expect(g.nativeRectToCss({ rect: { x: 0, y: 656, w: 1080, h: 608 }, screen }, 540)).toEqual({ x: 0, y: 328, w: 540, h: 304 });
		expect(g.nativeRectToCss({ rect: null, screen }, 540)).toBeNull();
		expect(g.nativeRectToCss({ rect: { x: 0, y: 0, w: 0, h: 10 }, screen }, 540)).toBeNull();
		expect(g.nativeRectToCss({ rect: { x: 0, y: 0, w: 10, h: 10 }, screen: { w: 0, h: 0 } }, 540)).toBeNull();
	});

	test("only the open app's event counts, and fullscreen clears it", () => {
		const stop = g.watchNativeWindow();
		const screen = { w: 1080, h: 1920 };
		g.applyNativeWindow({ app_id: "a.b.c", rect: { x: 0, y: 656, w: 1080, h: 608 }, screen }, 540);
		expect(get(g.gameRect)).toBeNull();
		g.setBarsApp({ id: "a.b.c", name: "Game" });
		g.applyNativeWindow({ app_id: "x.y.z", rect: { x: 0, y: 656, w: 1080, h: 608 }, screen }, 540);
		expect(get(g.gameRect)).toBeNull();
		g.applyNativeWindow({ app_id: "a.b.c", rect: { x: 0, y: 656, w: 1080, h: 608 }, screen }, 540);
		expect(get(g.gameRect)).toEqual({ x: 0, y: 328, w: 540, h: 304 });
		g.applyNativeWindow({ app_id: "a.b.c", rect: null, screen }, 540);
		expect(get(g.gameRect)).toBeNull();
		g.applyNativeWindow({ app_id: "a.b.c", rect: { x: 0, y: 656, w: 1080, h: 608 }, screen }, 540);
		listeners.get("native.status")?.({ app_id: "a.b.c", state: "exited" });
		expect(get(g.gameRect)).toBeNull();
		stop();
		g.setBarsApp(null);
	});
});

describe("bar rectangles", () => {
	const viewport = { w: 540, h: 960 };
	test("fills exactly the space above and below", () => {
		const bars = g.barRects({ x: 0, y: 328, w: 540, h: 304 }, viewport);
		expect(bars?.top).toEqual({ x: 0, y: 0, w: 540, h: 328 });
		expect(bars?.bottom).toEqual({ x: 0, y: 632, w: 540, h: 328 });
	});
	test("drops a bar under 80px and returns null when none is left", () => {
		const one = g.barRects({ x: 0, y: 40, w: 540, h: 600 }, viewport);
		expect(one?.top).toBeNull();
		expect(one?.bottom?.h).toBe(320);
		expect(g.barRects({ x: 0, y: 40, w: 540, h: 860 }, viewport)).toBeNull();
		expect(g.barRects(null, viewport)).toBeNull();
	});
});

describe("registry", () => {
	test("items are filtered by bar and zone, ordered, and replaceable", () => {
		const component = (() => {}) as never;
		const off = registerBarItem({ id: "t-b", bar: "bottom", zone: "left", order: 2, component });
		registerBarItem({ id: "t-a", bar: "bottom", zone: "left", order: 1, component });
		registerBarItem({ id: "t-c", bar: "top", zone: "left", order: 0, component });
		expect(barItems("bottom", "left").map((i) => i.id)).toEqual(["t-a", "t-b"]);
		off();
		expect(barItems("bottom", "left").map((i) => i.id)).toEqual(["t-a"]);
		expect(barItems("top", "center")).toEqual([]);
	});
});
