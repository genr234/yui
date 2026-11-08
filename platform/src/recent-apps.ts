/**
 * Recently opened apps, newest first. Stand-in for `ctx.library` (plan D10):
 * once apps can register their own resume points, the Jump back in banner
 * reads those instead and this list only orders apps.
 *
 * Stored in the kiosk's internal store (collection `shell`, id `recent-apps`)
 * so it follows the active account and survives a browser profile reset.
 */
import { internalStore } from "./sdk/internal-store";

export type RecentApp = { id: string; openedAt: number };

const COLLECTION = "shell";
const ID = "recent-apps";
const LIMIT = 12;

const recents = () => internalStore.collection<RecentApp[]>(COLLECTION);

function parseRecentApps(value: unknown): RecentApp[] {
	if (!Array.isArray(value)) return [];
	return value.filter(
		(entry): entry is RecentApp =>
			typeof entry?.id === "string" && typeof entry?.openedAt === "number",
	);
}

export async function loadRecentApps(): Promise<RecentApp[]> {
	try {
		return parseRecentApps(await recents().get(ID));
	} catch {
		return [];
	}
}

export async function recordRecentApp(id: string, now = Date.now()): Promise<RecentApp[]> {
	const next = [{ id, openedAt: now }, ...(await loadRecentApps()).filter((entry) => entry.id !== id)].slice(0, LIMIT);
	try {
		await recents().put(ID, next);
	} catch {
		// The store can be unavailable; the caller still shows this session's list.
	}
	return next;
}

export function openedAgo(openedAt: number, now = Date.now()) {
	const minutes = Math.max(0, Math.round((now - openedAt) / 60000));
	if (minutes < 1) return "just now";
	if (minutes < 60) return `${minutes} min ago`;
	const hours = Math.round(minutes / 60);
	if (hours < 24) return `${hours} h ago`;
	const days = Math.round(hours / 24);
	return days === 1 ? "yesterday" : `${days} days ago`;
}
