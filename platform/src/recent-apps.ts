/**
 * Recently opened apps, newest first. Stand-in for `ctx.library` (plan D10):
 * once apps can register their own resume points, the Jump back in banner
 * reads those instead and this list only orders apps.
 */
export type RecentApp = { id: string; openedAt: number };

const KEY = "yui:recent-apps";
const LIMIT = 12;

export function loadRecentApps(): RecentApp[] {
	try {
		const parsed = JSON.parse(localStorage.getItem(KEY) ?? "[]");
		if (!Array.isArray(parsed)) return [];
		return parsed.filter(
			(entry): entry is RecentApp =>
				typeof entry?.id === "string" && typeof entry?.openedAt === "number",
		);
	} catch {
		return [];
	}
}

export function recordRecentApp(id: string, now = Date.now()): RecentApp[] {
	const next = [{ id, openedAt: now }, ...loadRecentApps().filter((entry) => entry.id !== id)].slice(0, LIMIT);
	try {
		localStorage.setItem(KEY, JSON.stringify(next));
	} catch {
		// Storage can be unavailable; the list just won't persist.
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
