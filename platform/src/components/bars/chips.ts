/** Layout and display helpers for the seat chips (pure, tested). */
import { playerSwatch, type PadPlayer, type PadState } from "../../sdk/pad";

export const MAX_SEATS = 4;
/** Width kept free at the bar's right edge for the join corner. */
export const JOIN_RESERVE_PX = 156;

export type ChipLayout = { cols: number; rows: number; w: number; h: number; gap: number };

/**
 * Chip grid for `n` seated players in a bar `barW` wide and `barH` tall with
 * `reserve` px kept free on the right. One row while a chip stays at least
 * 132 px wide, otherwise two columns; chips shrink in height with more rows
 * so the bar keeps room above them for rising balloons.
 */
export function chipLayout(n: number, barW: number, barH: number, reserve = 0): ChipLayout {
	const count = Math.max(0, Math.floor(n));
	const gap = 10;
	const pad = 16;
	const avail = Math.max(0, barW - pad * 2 - reserve);
	if (count === 0) return { cols: 0, rows: 0, w: 0, h: 0, gap };
	let cols = count;
	if (count > 2 && (avail - gap * (count - 1)) / count < 132) cols = 2;
	const rows = Math.ceil(count / cols);
	const w = Math.min(230, Math.max(0, (avail - gap * (cols - 1)) / cols));
	const hMax = rows === 1 ? 112 : 84;
	// Leave at least a third of the bar free above the chips for balloons.
	const h = Math.max(48, Math.min(hMax, (barH * 0.62 - gap * (rows - 1)) / rows));
	return { cols, rows, w, h, gap };
}

/** Seated players in slot order. */
export function seated(players: PadPlayer[]): PadPlayer[] {
	return players.filter((p) => p.slot).sort((a, b) => (a.slot ?? 0) - (b.slot ?? 0));
}

export function spectators(players: PadPlayer[]): PadPlayer[] {
	return players.filter((p) => !p.slot);
}

export function swatchOf(color: string | undefined): string {
	return (color && playerSwatch[color]) || "#2f76dc";
}

/** Who set the game up: the host phone's name (the overlay is not told who picked the title). */
export function pickedBy(players: PadPlayer[]): string | null {
	return players.find((p) => p.host)?.name ?? null;
}

/**
 * The join corner shows whenever a session exists: anyone with the code can
 * join, taking a free seat or watching when the table is full.
 */
export function joinVisible(session: PadState["session"] | undefined): boolean {
	return Boolean(session?.join_url);
}
