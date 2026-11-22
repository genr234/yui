/**
 * Where a phone calibrating its motion should point (the pad's aim steps).
 * The phone sends `{t:"system", action:"calibrate", value: step}` and the
 * controller pushes `pad.calibrate {player_id, step}`; the overlay draws the
 * target in the game's area: the running game's window, or, before a game
 * starts, the 16:9 band it will take (the same rect the controller places
 * native windows in), so "the middle of the screen" on a tall portrait kiosk
 * means the game, not the whole totem.
 */
import { fitAspect, type Rect } from "./game-rect";

export type AimStep = "scan" | "aim-center" | "aim-right" | "aim-top";
export const AIM_STEPS: readonly AimStep[] = ["scan", "aim-center", "aim-right", "aim-top"];

/**
 * The scan step's markers. The phone finds them with its camera
 * (pad input/markers.ts) and works out its angle to the game from where
 * they appear, so their centres must be exactly where it expects them: at
 * ±U, ±V of the game's half-width and half-height from its middle (pad
 * platform/viewpoint.ts, SCAN_MARKER_U/V). Each is a white square with a
 * black square hole half its size, on black.
 */
export const SCAN_MARKER_U = 0.8;
export const SCAN_MARKER_V = 0.7;

export type ScanMarker = { x: number; y: number; size: number };

/** The four markers' centres (TL, TR, BR, BL) and side, in viewport px: as big as fits between each centre and the game's edges. */
export function scanMarkers(area: Rect): ScanMarker[] {
	const insetX = ((1 - SCAN_MARKER_U) * area.w) / 2;
	const insetY = ((1 - SCAN_MARKER_V) * area.h) / 2;
	const size = 1.6 * Math.min(insetX, insetY);
	const cx = area.x + area.w / 2;
	const cy = area.y + area.h / 2;
	return [
		[-1, -1],
		[1, -1],
		[1, 1],
		[-1, 1],
	].map(([sx, sy]) => ({ x: cx + (sx * SCAN_MARKER_U * area.w) / 2, y: cy + (sy * SCAN_MARKER_V * area.h) / 2, size }));
}

/** A player who stops sending (phone locked, left) stops showing after this. */
export const CALIBRATE_STALE_MS = 90_000;
export const GAME_ASPECT = 16 / 9;

export type Aiming = { playerId: string; step: AimStep; at: number };

/** Players currently on an aim step, by player id. */
export function applyCalibrate(state: Map<string, Aiming>, data: unknown, now: number): Map<string, Aiming> {
	const d = data as { player_id?: unknown; step?: unknown } | null;
	if (!d || typeof d.player_id !== "string" || !d.player_id) return state;
	const next = new Map(state);
	if (typeof d.step === "string" && (AIM_STEPS as readonly string[]).includes(d.step)) {
		next.set(d.player_id, { playerId: d.player_id, step: d.step as AimStep, at: now });
	} else {
		next.delete(d.player_id);
	}
	return next;
}

export function dropStale(state: Map<string, Aiming>, now: number): Map<string, Aiming> {
	let changed = false;
	const next = new Map(state);
	for (const [id, a] of state) {
		if (now - a.at > CALIBRATE_STALE_MS) {
			next.delete(id);
			changed = true;
		}
	}
	return changed ? next : state;
}

/** The area to aim inside: the game's window, else a centred 16:9 band. */
export function aimArea(game: Rect | null, viewport: { w: number; h: number }): Rect {
	if (game && game.w > 0 && game.h > 0) return game;
	return fitAspect({ x: 0, y: 0, w: viewport.w, h: viewport.h }, GAME_ASPECT);
}

/**
 * What to draw for a step, in viewport px. The centre is a point; an edge is
 * the line itself (the pointer's full range ends exactly there), drawn just
 * inside so a game filling the screen's width still shows it.
 */
export function targetFor(step: AimStep, area: Rect, inset = 6) {
	const cx = area.x + area.w / 2;
	const cy = area.y + area.h / 2;
	if (step === "scan") return { kind: "scan" as const, x: cx, y: cy, markers: scanMarkers(area) };
	if (step === "aim-center") return { kind: "point" as const, x: cx, y: cy };
	if (step === "aim-right") return { kind: "edge" as const, x: area.x + area.w - inset, y: area.y, w: 0, h: area.h, labelX: area.x + area.w - inset, labelY: cy };
	return { kind: "edge" as const, x: area.x, y: area.y + inset, w: area.w, h: 0, labelX: cx, labelY: area.y + inset };
}
