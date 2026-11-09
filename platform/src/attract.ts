/**
 * The attract screen's state machine (event mode, plan 08 item 8).
 *
 * Pure: the overlay feeds it the clock and what it can see, and acts on what
 * it returns. The screen appears once the kiosk has been untouched for
 * `ATTRACT_IDLE_MS` with no phone connected, asks for a play session so the
 * QR is real, and goes away on any touch, key, or phone join.
 */
import type { RecentLibraryTitle } from "./sdk/library";

/** Untouched time before the attract screen appears. */
export const ATTRACT_IDLE_MS = 60_000;
/** Wait before asking again for a session that did not start. */
export const ATTRACT_RETRY_MS = 15_000;

export type AttractState = {
	shown: boolean;
	/** Last touch, key, phone join, or moment the screen was not allowed. */
	lastTouch: number;
	/** When a session was last requested for the screen; 0 = never. */
	lastStart: number;
};

export type AttractInput = {
	now: number;
	eventMode: boolean;
	/** A play session exists (it may have no one in it). */
	hasSession: boolean;
	connectedPlayers: number;
	/** The PIN pad or a permission prompt is open. */
	busy: boolean;
};

export type AttractStep = {
	state: AttractState;
	/** The screen needs a join QR but there is no session: start one. */
	startSession: boolean;
};

export function createAttract(now: number): AttractState {
	return { shown: false, lastTouch: now, lastStart: 0 };
}

/** Any touch, pointer move, or key: hide the screen and restart the idle timer. */
export function touched(state: AttractState, now: number): AttractState {
	return { ...state, shown: false, lastTouch: now };
}

export function stepAttract(state: AttractState, input: AttractInput): AttractStep {
	const { now } = input;
	// Event mode off, the menu in use, or someone playing: nothing to show,
	// and the idle clock starts over from now.
	if (!input.eventMode || input.busy || input.connectedPlayers > 0) {
		return { state: { ...state, shown: false, lastTouch: now, lastStart: 0 }, startSession: false };
	}
	let next = state;
	if (!next.shown) {
		if (now - next.lastTouch < ATTRACT_IDLE_MS) return { state: next, startSession: false };
		next = { ...next, shown: true };
	}
	if (input.hasSession) return { state: { ...next, lastStart: 0 }, startSession: false };
	const due = next.lastStart === 0 || now - next.lastStart >= ATTRACT_RETRY_MS;
	if (!due) return { state: next, startSession: false };
	return { state: { ...next, lastStart: now }, startSession: true };
}

/** One slide of the cycling showcase. */
export type ShowcaseEntry<A extends { id: string }> = {
	key: string;
	name: string;
	/** Something an <img> can show, or null to draw the app's icon. */
	art: string | null;
	app: A;
};

/**
 * What the attract screen cycles through: library titles with art first
 * (most recent first), then installed apps. No app is favoured: whatever
 * `ctx.library` published and whatever is installed. Titles of apps that
 * are not installed here are skipped.
 */
export function pickShowcase<A extends { id: string; name?: string }>(
	titles: RecentLibraryTitle[],
	apps: A[],
	limit = 8,
): ShowcaseEntry<A>[] {
	const byId = new Map(apps.map((app) => [app.id, app] as const));
	const out: ShowcaseEntry<A>[] = [];
	const covered = new Set<string>();
	for (const title of titles) {
		const app = byId.get(title.app_id);
		if (!app || !title.art_url) continue;
		out.push({ key: `title:${title.app_id}:${title.id}`, name: title.title, art: title.art_url, app });
		covered.add(app.id);
	}
	for (const app of apps) {
		if (covered.has(app.id)) continue;
		out.push({ key: `app:${app.id}`, name: app.name ?? app.id, art: null, app });
	}
	return out.slice(0, limit);
}

/** The next slide index, wrapping; 0 for an empty or single-slide list. */
export function nextSlide(index: number, count: number): number {
	return count > 1 ? (index + 1) % count : 0;
}
