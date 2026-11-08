/**
 * Chooses what the Apps page's Jump back in banner and resume row show.
 *
 * Library titles (`library.recent`, published by apps through
 * `ctx.library`) come first, ranked by recency across apps. Titles whose app
 * is not installed here are skipped. When no installed app has published a
 * title, the banner falls back to the recently opened apps list
 * (recent-apps.ts), then to a starter app. Free slots in the resume row are
 * filled with apps that have no title on screen, recently opened ones first.
 */
import type { RecentApp } from "./recent-apps";
import type { RecentLibraryTitle } from "./sdk/library";

export type JumpEntry<A extends { id: string }> = {
	key: string;
	app: A;
	/** The library title, when the entry comes from ctx.library. */
	title: RecentLibraryTitle | null;
	/** Display name: the title's name or the app's. */
	name: string;
	/** Last played (titles) or opened (apps), ms since epoch; 0 = never. */
	at: number;
	/** 0–1, or null when the entry has no progress. */
	progress: number | null;
	art: string | null;
};

export type JumpBackIn<A extends { id: string }> = {
	source: "library" | "recent-apps" | "starter";
	hero: JumpEntry<A> | null;
	row: JumpEntry<A>[];
};

const ROW = 3;

function timeOf(value: string | undefined) {
	if (!value) return 0;
	const t = Date.parse(value);
	return Number.isFinite(t) ? t : 0;
}

function clampProgress(value: unknown): number | null {
	if (typeof value !== "number" || !Number.isFinite(value)) return null;
	return Math.min(1, Math.max(0, value));
}

function titleEntry<A extends { id: string }>(app: A, title: RecentLibraryTitle): JumpEntry<A> {
	return {
		key: `title:${title.app_id}/${title.id}`,
		app,
		title,
		name: title.title,
		at: timeOf(title.played_at),
		progress: clampProgress(title.progress),
		art: typeof title.art_url === "string" && title.art_url !== "" ? title.art_url : null,
	};
}

function appEntry<A extends { id: string; name?: string }>(app: A, openedAt: number): JumpEntry<A> {
	return {
		key: `app:${app.id}`,
		app,
		title: null,
		name: app.name ?? app.id,
		at: openedAt,
		progress: null,
		art: null,
	};
}

export function selectJumpBackIn<A extends { id: string; name?: string }>(options: {
	apps: A[];
	titles: RecentLibraryTitle[];
	recents: RecentApp[];
	/** The starter app when nothing has been opened yet (e.g. the first game). */
	starter?: A | null;
}): JumpBackIn<A> {
	const byId = new Map(options.apps.map((app) => [app.id, app] as const));
	const titles: JumpEntry<A>[] = [];
	for (const title of options.titles) {
		const app = byId.get(title.app_id);
		if (app) titles.push(titleEntry(app, title));
	}
	const opened = options.recents
		.map((entry) => {
			const app = byId.get(entry.id);
			return app ? appEntry(app, entry.openedAt) : null;
		})
		.filter((entry): entry is JumpEntry<A> => entry !== null);

	if (titles.length > 0) {
		const shownApps = new Set(titles.slice(0, ROW + 1).map((entry) => entry.app.id));
		const fill = [
			...opened.filter((entry) => !shownApps.has(entry.app.id)),
			...options.apps
				.filter((app) => !shownApps.has(app.id) && !opened.some((entry) => entry.app.id === app.id))
				.map((app) => appEntry(app, 0)),
		];
		const row = [...titles.slice(1, ROW + 1), ...fill].slice(0, ROW);
		return { source: "library", hero: titles[0], row };
	}

	if (opened.length > 0) {
		const hero = opened[0];
		const row = [
			...opened.slice(1),
			...options.apps
				.filter((app) => app.id !== hero.app.id && !opened.some((entry) => entry.app.id === app.id))
				.map((app) => appEntry(app, 0)),
		].slice(0, ROW);
		return { source: "recent-apps", hero, row };
	}

	const starter = options.starter ?? options.apps[0] ?? null;
	if (!starter) return { source: "starter", hero: null, row: [] };
	return {
		source: "starter",
		hero: appEntry(starter, 0),
		row: options.apps
			.filter((app) => app.id !== starter.id)
			.map((app) => appEntry(app, 0))
			.slice(0, ROW),
	};
}
