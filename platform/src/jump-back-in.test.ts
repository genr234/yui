import { expect, test } from "bun:test";
import { selectJumpBackIn } from "./jump-back-in";
import type { RecentLibraryTitle } from "./sdk/library";

const apps = [
	{ id: "dev.genr.arcade", name: "Arcade" },
	{ id: "dev.genr.fruit-ninja", name: "Fruit Ninja" },
	{ id: "dev.genr.links", name: "Links" },
	{ id: "dev.genr.youtube", name: "YouTube" },
];

function title(app_id: string, id: string, extra: Partial<RecentLibraryTitle> = {}): RecentLibraryTitle {
	return { app_id, id, title: id.toUpperCase(), published_at: "2026-11-08T10:00:00Z", ...extra };
}

test("falls back to recent apps when nothing is published", () => {
	const result = selectJumpBackIn({
		apps,
		titles: [],
		recents: [
			{ id: "dev.genr.links", openedAt: 20 },
			{ id: "dev.genr.fruit-ninja", openedAt: 10 },
		],
	});
	expect(result.source).toBe("recent-apps");
	expect(result.hero?.app.id).toBe("dev.genr.links");
	expect(result.hero?.title).toBeNull();
	expect(result.hero?.progress).toBeNull();
	expect(result.row.map((entry) => entry.app.id)).toEqual([
		"dev.genr.fruit-ninja",
		"dev.genr.arcade",
		"dev.genr.youtube",
	]);
});

test("library titles drive the banner and its progress bar", () => {
	const result = selectJumpBackIn({
		apps,
		titles: [
			title("dev.genr.arcade", "kart", {
				played_at: "2026-11-08T11:00:00Z",
				progress: 0.42,
				art_url: "data:image/png;base64,AAAA",
			}),
			title("dev.genr.arcade", "brawl", { played_at: "2026-11-08T10:30:00Z" }),
		],
		recents: [
			{ id: "dev.genr.arcade", openedAt: 30 },
			{ id: "dev.genr.links", openedAt: 20 },
			{ id: "dev.genr.youtube", openedAt: 10 },
		],
	});
	expect(result.source).toBe("library");
	expect(result.hero?.name).toBe("KART");
	expect(result.hero?.progress).toBe(0.42);
	expect(result.hero?.art).toBe("data:image/png;base64,AAAA");
	expect(result.hero?.at).toBe(Date.parse("2026-11-08T11:00:00Z"));
	// Remaining titles first, then opened apps with no title on screen.
	expect(result.row.map((entry) => entry.key)).toEqual([
		"title:dev.genr.arcade/brawl",
		"app:dev.genr.links",
		"app:dev.genr.youtube",
	]);
});

test("the resume row is filled with other apps when few titles exist", () => {
	const result = selectJumpBackIn({
		apps,
		titles: [title("dev.genr.arcade", "kart"), title("dev.genr.fruit-ninja", "zen")],
		recents: [{ id: "dev.genr.youtube", openedAt: 5 }],
	});
	expect(result.row.map((entry) => entry.key)).toEqual([
		"title:dev.genr.fruit-ninja/zen",
		"app:dev.genr.youtube",
		"app:dev.genr.links",
	]);
});

test("titles from apps that are not installed are ignored", () => {
	const result = selectJumpBackIn({
		apps,
		titles: [title("dev.someone.gone", "x", { played_at: "2026-11-08T12:00:00Z" })],
		recents: [{ id: "dev.genr.youtube", openedAt: 5 }],
	});
	expect(result.source).toBe("recent-apps");
	expect(result.hero?.app.id).toBe("dev.genr.youtube");
});

test("progress is clamped and non-numbers are dropped", () => {
	const high = selectJumpBackIn({ apps, titles: [title("dev.genr.arcade", "a", { progress: 3 })], recents: [] });
	expect(high.hero?.progress).toBe(1);
	const junk = selectJumpBackIn({
		apps,
		titles: [title("dev.genr.arcade", "a", { progress: "half" as unknown as number })],
		recents: [],
	});
	expect(junk.hero?.progress).toBeNull();
});

test("starter app when nothing was opened or published", () => {
	const result = selectJumpBackIn({ apps, titles: [], recents: [], starter: apps[1] });
	expect(result.source).toBe("starter");
	expect(result.hero?.app.id).toBe("dev.genr.fruit-ninja");
	expect(result.row).toHaveLength(3);
	expect(selectJumpBackIn({ apps: [], titles: [], recents: [] }).hero).toBeNull();
});
