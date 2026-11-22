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
 * The scan step's targets: one QR code in the middle of the game, and four
 * white squares near its corners. The phone (pad input/aimCodes.ts) reads
 * the code with its camera; the code's text says where everything is, so
 * nothing else in the room can pass for it:
 * `YUIAIM2:<cx>:<cy>:<mx>:<my>` means the code spans ±cx, ±cy and the
 * squares' centres sit at ±mx, ±my, in game coordinates (−1…1, +y down,
 * 3 decimals, leading 0 dropped to keep the code small). Everything is
 * drawn exactly where the rounded text says.
 *
 * Why both: a QR reader finds one code reliably (two side by side confuse
 * it), but a code's corners are close together, and from a few metres a
 * pixel's error in them is several degrees in the angle the phone works
 * out. The squares, found where the code says they must be, spread the
 * measurement across the whole game. The code's middle is the game's
 * middle, so a player aiming there while another scans aims at the code's
 * centre; error correction H shrugs off the dot drawn there.
 */
export const AIM_CODE_PREFIX = "YUIAIM2:";
export const SCAN_SQUARE_X = 0.9;
export const SCAN_SQUARE_Y = 0.85;

export type AimCode = {
	text: string;
	/** The code's symbol (its modules, no margin), viewport px: square to within rounding. */
	x: number;
	y: number;
	w: number;
	h: number;
	/** The corner squares' centres (TL, TR, BR, BL) and side, viewport px. */
	squares: { x: number; y: number }[];
	square: number;
};

/** The code and squares for a game area. */
export function aimCode(area: Rect): AimCode {
	const cx = area.x + area.w / 2;
	const cy = area.y + area.h / 2;
	const short = Math.min(area.w, area.h);
	const long = Math.max(area.w, area.h);
	const r3 = (v: number) => Math.round(v * 1000) / 1000;
	const mx = SCAN_SQUARE_X;
	const my = SCAN_SQUARE_Y;
	// Squares as big as fits between their centres and the game's edges.
	const square = Math.min(0.1 * short, 1.6 * Math.min(((1 - mx) * area.w) / 2, ((1 - my) * area.h) / 2));
	// The code plus its white margin (0.14 of it each side, 4 modules): 96% of the
	// short side, but clear of the squares along the long side (a squarish game).
	const along = area.w >= area.h ? mx * (area.w / 2) : my * (area.h / 2);
	const size = Math.min((0.96 * short) / 1.28, (along - square / 2 - 12) / 0.64);
	const x1 = r3(size / 2 / (area.w / 2));
	const y1 = r3(size / 2 / (area.h / 2));
	const fmt = (v: number) => v.toFixed(3).replace(/^0/, "");
	return {
		text: `${AIM_CODE_PREFIX}${fmt(x1)}:${fmt(y1)}:${fmt(mx)}:${fmt(my)}`,
		x: cx - (x1 * area.w) / 2,
		y: cy - (y1 * area.h) / 2,
		w: x1 * area.w,
		h: y1 * area.h,
		squares: [
			[-1, -1],
			[1, -1],
			[1, 1],
			[-1, 1],
		].map(([sx, sy]) => ({ x: cx + (sx * mx * area.w) / 2, y: cy + (sy * my * area.h) / 2 })),
		square,
	};
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
	if (step === "scan") return { kind: "scan" as const, x: cx, y: cy, code: aimCode(area) };
	if (step === "aim-center") return { kind: "point" as const, x: cx, y: cy };
	if (step === "aim-right") return { kind: "edge" as const, x: area.x + area.w - inset, y: area.y, w: 0, h: area.h, labelX: area.x + area.w - inset, labelY: cy };
	return { kind: "edge" as const, x: area.x, y: area.y + inset, w: area.w, h: 0, labelX: cx, labelY: area.y + inset };
}

/** A player on the board: a chip in their colour. */
export type BoardPlayer = { id: string; label: string; color: string };

/**
 * Who is on which target. Several phones calibrate at once, so the kiosk
 * draws each target once and lists the players on it, in slot order, rather
 * than one overlapping target per player.
 */
export type Board = {
	scan: BoardPlayer[];
	center: BoardPlayer[];
	right: BoardPlayer[];
	top: BoardPlayer[];
};

type RosterPlayer = { id: string; slot: number | null; name: string; color: string };

export function boardFor(aiming: Map<string, Aiming>, roster: readonly RosterPlayer[], swatch: Record<string, string>): Board {
	const board: Board = { scan: [], center: [], right: [], top: [] };
	const order = (id: string) => {
		const p = roster.find((r) => r.id === id);
		return p?.slot ?? 99;
	};
	const ids = [...aiming.keys()].sort((a, b) => order(a) - order(b) || a.localeCompare(b));
	for (const id of ids) {
		const step = aiming.get(id)!.step;
		const p = roster.find((r) => r.id === id);
		const chip = { id, label: p?.slot ? `P${p.slot}` : p?.name || "Player", color: (p && swatch[p.color]) || "#2f76dc" };
		const list = step === "scan" ? board.scan : step === "aim-center" ? board.center : step === "aim-right" ? board.right : board.top;
		list.push(chip);
	}
	return board;
}

/** Where the board's captions go: beside the code on a wide game, above and below it on a tall one. */
export function captionSpots(area: Rect, code: AimCode): { first: { x: number; y: number }; second: { x: number; y: number } } {
	const cx = area.x + area.w / 2;
	const cy = area.y + area.h / 2;
	const margin = 0.14 * code.w;
	const card = { left: code.x - margin, right: code.x + code.w + margin, top: code.y - margin, bottom: code.y + code.h + margin };
	if (area.w >= area.h) {
		return {
			first: { x: (area.x + card.left) / 2, y: cy },
			second: { x: (card.right + area.x + area.w) / 2, y: cy },
		};
	}
	return {
		first: { x: cx, y: (area.y + card.top) / 2 },
		second: { x: cx, y: (card.bottom + area.y + area.h) / 2 },
	};
}
