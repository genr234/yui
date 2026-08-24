import { validateSimpleApp } from "./validate";
import type { YuiDevApp, YuiSimpleApp } from "./types";
import { bridge } from "../bridge";

type Manifest = {
	schema: "yui.local-app.v0";
	type: "simple-js";
	entry: string;
	dev?: boolean;
};

export type YuiAppSource = {
	id: string;
	url: string;
	name?: string;
	publisher?: string;
	dev?: boolean;
	signingKeys?: string[];
	lastRefreshed?: string;
	lastStatus: "pending" | "ok" | "error" | string;
	lastError?: string;
	discoveredApps?: number;
	createdAt?: string;
	updatedAt?: string;
};

export type YuiCatalogEntry = {
	sourceId: string;
	sourceUrl: string;
	catalog: string;
	publisher: string;
	verified: boolean;
	updatedAt: string;
	app: {
		id: string;
		name: string;
		version: string;
		description?: string;
		icon?: string;
		category?: string;
		permissions?: string[];
		sourceUrl: string;
		sourceSha256: string;
		signature: string;
	};
};

type InstalledAppRecord = YuiDevApp & {
	installed: true;
	sourceId: string;
	sourceUrl: string;
	installedAt: string;
	app: Omit<YuiSimpleApp, "mount">;
};

const DEV_APP_SOURCE_ID = "dev-apps";
const DEV_APP_SOURCE_URL = "yui://dev/apps";

const manifests = import.meta.glob("../../../../apps/*/yui.app.json", {
	query: "?raw",
	import: "default",
	eager: true,
}) as Record<string, string>;

const appModules = import.meta.glob("../../../../apps/*/*.yui.js", {
	query: "?raw",
	import: "default",
	eager: true,
}) as Record<string, string>;

let localDevAppsCache: YuiDevApp[] | undefined;

function appDirectory(manifestPath: string) {
	return manifestPath.slice(0, manifestPath.lastIndexOf("/") + 1);
}

function normalizeEntry(manifestPath: string, entry: string) {
	return `${appDirectory(manifestPath)}${entry.replace(/^\.\//, "")}`;
}

function stringField(source: string, field: string) {
	const match = source.match(new RegExp(`${field}\\s*:\\s*(["'])([\\s\\S]*?)\\1`));
	return match?.[2];
}

function stringArrayField(source: string, field: string) {
	const match = source.match(new RegExp(`${field}\\s*:\\s*\\[([\\s\\S]*?)\\]`));
	if (!match) return undefined;
	return [...match[1].matchAll(/(["'])([\s\S]*?)\1/g)].map((item) => item[2]);
}

function metadataFromSource(source: string) {
	const app = {
		schema: stringField(source, "schema"),
		id: stringField(source, "id"),
		name: stringField(source, "name"),
		version: stringField(source, "version"),
		description: stringField(source, "description"),
		author: stringField(source, "author"),
		homepage: stringField(source, "homepage"),
		license: stringField(source, "license"),
		icon: stringField(source, "icon"),
		category: stringField(source, "category"),
		tags: stringArrayField(source, "tags"),
		permissions: stringArrayField(source, "permissions"),
		mount() {},
	};
	validateSimpleApp(app);
	return app;
}

export async function discoverLocalDevApps(): Promise<YuiDevApp[]> {
	if (localDevAppsCache) return localDevAppsCache.map(cloneDevApp);

	const apps = await Promise.all(
		Object.entries(manifests).map(async ([path, raw]) => {
			const manifest = JSON.parse(raw) as Manifest;
			if (manifest.schema !== "yui.local-app.v0" || manifest.type !== "simple-js") {
				throw new Error(`Unsupported app manifest: ${path}`);
			}

			const entry = normalizeEntry(path, manifest.entry);
			const source = appModules[entry];
			if (!source) {
				throw new Error(`Missing app entry: ${entry}`);
			}
			const app = metadataFromSource(source);

			return {
				id: app.id,
				name: app.name,
				version: app.version,
				type: "simple-js" as const,
				entry,
				dev: true,
				app,
				source,
			};
		}),
	);

	localDevAppsCache = apps.sort((a, b) => a.name.localeCompare(b.name));
	return localDevAppsCache.map(cloneDevApp);
}

function cloneDevApp(app: YuiDevApp): YuiDevApp {
	return { ...app, app: { ...app.app } };
}

function localAppToCatalogEntry(app: YuiDevApp): YuiCatalogEntry {
	return {
		sourceId: DEV_APP_SOURCE_ID,
		sourceUrl: DEV_APP_SOURCE_URL,
		catalog: "Dev apps",
		publisher: "Local workspace",
		verified: true,
		updatedAt: "",
		app: {
			id: app.id,
			name: app.name,
			version: app.version,
			description: app.app.description,
			icon: app.app.icon,
			category: app.app.category,
			permissions: app.app.permissions,
			sourceUrl: app.entry,
			sourceSha256: "",
			signature: "",
		},
	};
}

export async function listDevAppSources(): Promise<YuiAppSource[]> {
	const apps = await discoverLocalDevApps();
	if (apps.length === 0) return [];
	return [
		{
			id: DEV_APP_SOURCE_ID,
			url: DEV_APP_SOURCE_URL,
			name: "Dev apps",
			publisher: "Local workspace",
			dev: true,
			lastStatus: "ok",
			discoveredApps: apps.length,
		},
	];
}

export async function listDevAppCatalog(): Promise<YuiCatalogEntry[]> {
	const apps = await discoverLocalDevApps();
	return apps.map(localAppToCatalogEntry);
}

function installedRecordToApp(record: InstalledAppRecord): YuiDevApp {
	const app = {
		...record.app,
		mount() {},
	} as YuiSimpleApp;
	validateSimpleApp(app);
	return {
		id: record.id,
		name: record.name,
		version: record.version,
		type: "simple-js",
		entry: record.entry,
		dev: false,
		installed: true,
		sourceId: record.sourceId,
		sourceUrl: record.sourceUrl,
		installedAt: record.installedAt,
		app,
		source: record.source,
	};
}

function dispatchAppsChanged(appId?: string) {
	window.dispatchEvent(new CustomEvent("yui:apps-changed", { detail: { appId } }));
}

async function mutateApps<T>(appId: string | undefined, action: Promise<T>) {
	const result = await action;
	dispatchAppsChanged(appId);
	return result;
}

export async function listInstalledApps(): Promise<YuiDevApp[]> {
	const records = await bridge.send<InstalledAppRecord[]>("apps.installed.list");
	return records.map(installedRecordToApp);
}

export async function discoverApps(): Promise<YuiDevApp[]> {
	const [installedApps, localApps] = await Promise.all([
		listInstalledApps().catch(() => []),
		discoverLocalDevApps().catch(() => []),
	]);
	const localByID = new Map(localApps.map((app) => [app.id, app]));
	if (import.meta.env.VITE_YUI_DEMO === "true" && installedApps.length === 0) {
		return localApps.map((app) => ({ ...app, installed: true }));
	}
	for (const app of installedApps) {
		if (app.sourceId !== DEV_APP_SOURCE_ID) continue;
		const local = localByID.get(app.id);
		if (!local) continue;
		app.name = local.name;
		app.version = local.version;
		app.entry = local.entry;
		app.app = local.app;
		app.source = local.source;
		app.sourceUrl = local.entry;
	}
	return installedApps.sort((a, b) => a.name.localeCompare(b.name));
}

export async function discoverDevApps(): Promise<YuiDevApp[]> {
	return discoverApps();
}

export const appSources = {
	list: () => bridge.send<YuiAppSource[]>("apps.sources.list"),
	add: (url: string) => mutateApps(undefined, bridge.send<YuiAppSource>("apps.sources.add", { url })),
	remove: (id: string) => mutateApps(undefined, bridge.send<void>("apps.sources.remove", { id })),
	refresh: (id: string) => mutateApps(undefined, bridge.send<YuiAppSource>("apps.sources.refresh", { id })),
};

export const appCatalog = {
	list: () => bridge.send<YuiCatalogEntry[]>("apps.catalog.list"),
	install: (catalogId: string) => mutateApps(undefined, bridge.send<InstalledAppRecord>("apps.install", { catalogId })),
	installDev: async (entry: YuiCatalogEntry) => {
		const localApps = await discoverLocalDevApps();
		const app = localApps.find(
			(candidate) =>
				candidate.id === entry.app.id &&
				candidate.version === entry.app.version &&
				candidate.entry === entry.app.sourceUrl,
		);
		if (!app) {
			throw new Error(`Dev app not found: ${entry.app.id}`);
		}
		return mutateApps(
			app.id,
			bridge.send<InstalledAppRecord>("apps.dev.install", {
				entry: app.entry,
				source: app.source,
			}),
		);
	},
	uninstall: (id: string) => mutateApps(id, bridge.send<void>("apps.uninstall", { id })),
};

export function catalogEntryId(entry: YuiCatalogEntry) {
	return `${entry.sourceId}:${entry.app.id}:${entry.app.version}`;
}
