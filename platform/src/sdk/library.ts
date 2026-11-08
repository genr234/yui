/**
 * Shell-side view of the title library (contracts §4, `library.*`). Apps
 * publish through `ctx.library`; the overlay reads `library.recent` for the
 * Jump back in banner.
 */
import { bridge } from "./bridge";

export type LibraryTitleInput = {
	id: string;
	title: string;
	/** A `data:image/...;base64,` URI or an asset id. */
	art?: string;
	players?: number;
	resume?: unknown;
	/** 0 to 1. */
	progress?: number | null;
};

export type LibraryTitle = {
	app_id: string;
	id: string;
	title: string;
	art?: string;
	art_asset?: boolean;
	players?: number;
	resume?: unknown;
	progress?: number;
	published_at: string;
	played_at?: string;
};

export type RecentLibraryTitle = LibraryTitle & {
	/** Something an <img> can show: the data URI, or an asset's cover_url. */
	art_url?: string;
};

export async function loadLibraryRecent(limit = 12): Promise<RecentLibraryTitle[]> {
	try {
		const result = await bridge.send<RecentLibraryTitle[] | null>("library.recent", { limit });
		return Array.isArray(result) ? result : [];
	} catch {
		return [];
	}
}

export function onLibraryChanged(fn: (data: { app_id?: string }) => void) {
	return bridge.on<{ app_id?: string }>("library.changed", (data) => fn(data ?? {}));
}
