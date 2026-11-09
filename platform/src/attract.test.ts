import { expect, test } from "bun:test";
import {
	ATTRACT_IDLE_MS,
	ATTRACT_RETRY_MS,
	createAttract,
	nextSlide,
	pickShowcase,
	stepAttract,
	touched,
	type AttractInput,
} from "./attract";
import type { RecentLibraryTitle } from "./sdk/library";

const base: AttractInput = { now: 0, eventMode: true, hasSession: false, connectedPlayers: 0, busy: false };

test("stays hidden until the kiosk has been untouched long enough", () => {
	const s0 = createAttract(1000);
	const early = stepAttract(s0, { ...base, now: 1000 + ATTRACT_IDLE_MS - 1 });
	expect(early.state.shown).toBe(false);
	expect(early.startSession).toBe(false);
	const late = stepAttract(s0, { ...base, now: 1000 + ATTRACT_IDLE_MS });
	expect(late.state.shown).toBe(true);
});

test("asks for a session when it appears, then retries only after a pause", () => {
	const shown = stepAttract(createAttract(0), { ...base, now: ATTRACT_IDLE_MS });
	expect(shown.startSession).toBe(true);
	const again = stepAttract(shown.state, { ...base, now: ATTRACT_IDLE_MS + 1000 });
	expect(again.startSession).toBe(false);
	const retry = stepAttract(shown.state, { ...base, now: ATTRACT_IDLE_MS + ATTRACT_RETRY_MS });
	expect(retry.startSession).toBe(true);
});

test("does not start a session when one already exists", () => {
	const shown = stepAttract(createAttract(0), { ...base, now: ATTRACT_IDLE_MS, hasSession: true });
	expect(shown.state.shown).toBe(true);
	expect(shown.startSession).toBe(false);
});

test("a touch hides it and restarts the idle timer", () => {
	const shown = stepAttract(createAttract(0), { ...base, now: ATTRACT_IDLE_MS }).state;
	const hidden = touched(shown, ATTRACT_IDLE_MS + 500);
	expect(hidden.shown).toBe(false);
	expect(stepAttract(hidden, { ...base, now: ATTRACT_IDLE_MS + 500 + ATTRACT_IDLE_MS - 1 }).state.shown).toBe(false);
	expect(stepAttract(hidden, { ...base, now: ATTRACT_IDLE_MS + 500 + ATTRACT_IDLE_MS }).state.shown).toBe(true);
});

test("a phone joining dismisses it, and the timer restarts when they leave", () => {
	const shown = stepAttract(createAttract(0), { ...base, now: ATTRACT_IDLE_MS, hasSession: true }).state;
	const joined = stepAttract(shown, { ...base, now: ATTRACT_IDLE_MS + 10, hasSession: true, connectedPlayers: 1 });
	expect(joined.state.shown).toBe(false);
	const left = stepAttract(joined.state, { ...base, now: ATTRACT_IDLE_MS + 20, hasSession: true });
	expect(left.state.shown).toBe(false);
});

test("never shows with event mode off or while the menu is open", () => {
	const off = stepAttract(createAttract(0), { ...base, now: 10 * ATTRACT_IDLE_MS, eventMode: false });
	expect(off.state.shown).toBe(false);
	expect(off.startSession).toBe(false);
	const busy = stepAttract(createAttract(0), { ...base, now: 10 * ATTRACT_IDLE_MS, busy: true });
	expect(busy.state.shown).toBe(false);
	// Closing the menu starts a fresh idle period.
	const after = stepAttract(busy.state, { ...base, now: 10 * ATTRACT_IDLE_MS + 1 });
	expect(after.state.shown).toBe(false);
});

function title(app_id: string, id: string, art_url?: string): RecentLibraryTitle {
	return { app_id, id, title: id.toUpperCase(), published_at: "2026-11-08T10:00:00Z", art_url };
}

test("showcase puts titles with art first, then the remaining apps", () => {
	const apps = [
		{ id: "a", name: "Alpha" },
		{ id: "b", name: "Beta" },
		{ id: "c", name: "Gamma" },
	];
	const list = pickShowcase(
		[title("b", "level1", "data:image/png;base64,x"), title("c", "noart"), title("zz", "gone", "data:image/png;base64,y")],
		apps,
	);
	expect(list.map((e) => e.key)).toEqual(["title:b:level1", "app:a", "app:c"]);
	expect(list[0].art).toBe("data:image/png;base64,x");
	expect(list[1].art).toBeNull();
	expect(pickShowcase([], apps, 2)).toHaveLength(2);
	expect(pickShowcase([], [])).toEqual([]);
});

test("slides wrap and handle short lists", () => {
	expect(nextSlide(0, 3)).toBe(1);
	expect(nextSlide(2, 3)).toBe(0);
	expect(nextSlide(0, 1)).toBe(0);
	expect(nextSlide(5, 0)).toBe(0);
});
