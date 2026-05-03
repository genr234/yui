import { validateSimpleApp } from "./validate";
import type { YuiDevApp } from "./types";

type Manifest = {
	schema: "yui.local-app.v0";
	type: "simple-js";
	entry: string;
	dev?: boolean;
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

export async function discoverDevApps(): Promise<YuiDevApp[]> {
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
				dev: true as const,
				app,
				source,
			};
		}),
	);

	return apps.sort((a, b) => a.name.localeCompare(b.name));
}
