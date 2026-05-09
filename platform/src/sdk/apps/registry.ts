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

	return apps.sort((a, b) => a.name.localeCompare(b.name));
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

export async function listInstalledApps(): Promise<YuiDevApp[]> {
	const records = await bridge.send<InstalledAppRecord[]>("apps.installed.list");
	return records.map(installedRecordToApp);
}

export async function discoverApps(): Promise<YuiDevApp[]> {
	const [localApps, installedApps] = await Promise.all([
		discoverLocalDevApps(),
		listInstalledApps().catch(() => []),
	]);
	const byID = new Map<string, YuiDevApp>();
	for (const app of localApps) byID.set(app.id, app);
	for (const app of installedApps) byID.set(app.id, app);
	return [...byID.values()].sort((a, b) => a.name.localeCompare(b.name));
}

export async function discoverDevApps(): Promise<YuiDevApp[]> {
	return discoverApps();
}

export const appSources = {
	list: () => bridge.send<YuiAppSource[]>("apps.sources.list"),
	add: (url: string) => bridge.send<YuiAppSource>("apps.sources.add", { url }),
	remove: (id: string) => bridge.send<void>("apps.sources.remove", { id }),
	refresh: (id: string) => bridge.send<YuiAppSource>("apps.sources.refresh", { id }),
};

export const appCatalog = {
	list: () => bridge.send<YuiCatalogEntry[]>("apps.catalog.list"),
	install: (catalogId: string) => bridge.send<InstalledAppRecord>("apps.install", { catalogId }),
	uninstall: (id: string) => bridge.send<void>("apps.uninstall", { id }),
};

export function catalogEntryId(entry: YuiCatalogEntry) {
	return `${entry.sourceId}:${entry.app.id}:${entry.app.version}`;
}
