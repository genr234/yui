import {
	appCatalog,
	appSources,
	catalogEntryId,
	discoverApps,
	type YuiAppSource,
	type YuiCatalogEntry,
	type YuiDevApp,
} from "./sdk/apps";
import type { YuiPermission } from "./sdk/apps/types";

export type AppCategory = {
	name: string;
	items: YuiDevApp[];
};

export type AppsLibrary = {
	apps: YuiDevApp[];
	sources: YuiAppSource[];
	catalog: YuiCatalogEntry[];
};

export function loadApps() {
	return discoverApps();
}

export async function loadAppsLibrary(): Promise<AppsLibrary> {
	const [apps, sources, catalog] = await Promise.all([
		loadApps(),
		appSources.list().catch(() => []),
		appCatalog.list().catch(() => []),
	]);

	return { apps, sources, catalog };
}

export async function addAppSource(url: string) {
	const source = await appSources.add(url);
	await appSources.refresh(source.id);
	return source;
}

export function refreshAppSource(id: string) {
	return appSources.refresh(id);
}

export function removeAppSource(id: string) {
	return appSources.remove(id);
}

export function installCatalogApp(entry: YuiCatalogEntry) {
	return appCatalog.install(catalogEntryId(entry));
}

export function uninstallApp(id: string) {
	return appCatalog.uninstall(id);
}

export function appCatalogEntryId(entry: YuiCatalogEntry) {
	return catalogEntryId(entry);
}

export function installedAppIds(apps: YuiDevApp[]) {
	return new Set(apps.filter((app) => app.installed).map((app) => app.id));
}

export function findApp(apps: YuiDevApp[], id: string) {
	return apps.find((app) => app.id === id);
}

export function firstAvailableAppId(apps: YuiDevApp[], preferredId = "") {
	return findApp(apps, preferredId)?.id ?? apps[0]?.id ?? "";
}

export function groupAppsByCategory(apps: YuiDevApp[]): AppCategory[] {
	const groups = new Map<string, YuiDevApp[]>();
	for (const app of apps) {
		const category = normalizeAppCategory(app.app.category);
		groups.set(category, [...(groups.get(category) ?? []), app]);
	}
	return [...groups.entries()].map(([name, items]) => ({ name, items }));
}

export function normalizeAppCategory(category?: string) {
	const value = category?.trim();
	if (!value) return "Utilities";
	return value.charAt(0).toUpperCase() + value.slice(1);
}

export function isImageAppIcon(icon?: string) {
	return Boolean(
		icon &&
			(/^(https?:|data:|\/|\.)/.test(icon) ||
				/\.(png|jpe?g|gif|webp|svg)$/i.test(icon)),
	);
}

export function fallbackAppIcon(app: YuiDevApp, fallback = "◇") {
	return app.app.icon ?? (app.name.slice(0, 4) || fallback);
}

export function isGameAppCategory(category: string) {
	return category.toLowerCase() === "games";
}

export function appHasPermission(app: YuiDevApp | undefined, permission: YuiPermission) {
	return Boolean(app?.app.permissions?.includes(permission));
}
