/**
 * Plug-in point for the letterbox bars. A feature registers an item (in
 * ./items.ts) and LetterboxBars renders it in the bar and zone it names.
 * LetterboxBars itself never changes.
 */
import type { ComponentType, SvelteComponent } from "svelte";
import type { Rect } from "../../sdk/game-rect";
import type { PadPlayer } from "../../sdk/pad";

/** Props every bar item component receives. */
export type BarItemProps = {
	/** This bar's rectangle in viewport CSS px. */
	rect: Rect;
	/** The game's rectangle in viewport CSS px. */
	gameRect: Rect;
	/** Players in the current play session (empty when none). */
	players: PadPlayer[];
	/** Which bar this item is in. */
	bar: "top" | "bottom";
};

export type BarZone = "left" | "center" | "right" | "fill";

export type BarItem = {
	/** Unique; registering the same id again replaces the item. */
	id: string;
	bar: "top" | "bottom";
	/** `fill` stretches across the whole bar behind/around the other zones (reactions, rising bubbles). */
	zone: BarZone;
	/** Lower comes first within a zone. */
	order: number;
	component: ComponentType<SvelteComponent<BarItemProps>>;
};

const items = new Map<string, BarItem>();
const listeners = new Set<() => void>();

/** Add (or replace) an item. Returns a function that removes it. */
export function registerBarItem(item: BarItem): () => void {
	items.set(item.id, item);
	notify();
	return () => {
		if (items.get(item.id) === item) {
			items.delete(item.id);
			notify();
		}
	};
}

/** Items for one bar and zone, in `order` (ties by id). */
export function barItems(bar: "top" | "bottom", zone: BarZone): BarItem[] {
	return [...items.values()]
		.filter((item) => item.bar === bar && item.zone === zone)
		.sort((a, b) => a.order - b.order || a.id.localeCompare(b.id));
}

export function onBarItemsChanged(fn: () => void): () => void {
	listeners.add(fn);
	return () => listeners.delete(fn);
}

function notify() {
	for (const fn of listeners) fn();
}
