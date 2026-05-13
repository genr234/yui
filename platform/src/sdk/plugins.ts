import { bridge } from "./bridge";
import { store } from "./store";

export type YuiPluginSettingSchema = {
	type: "string" | "number" | "bool" | "select" | "path" | "textarea" | "secret" | string;
	label: string;
	description?: string;
	default?: unknown;
	options?: string[];
	required?: boolean;
};

export type YuiPluginMetadata = {
	schema: "yui.starlark-plugin.v0";
	id: string;
	name: string;
	version: string;
	description?: string;
	author?: string;
	homepage?: string;
	license?: string;
	icon?: string;
	permissions?: string[];
	settings?: Record<string, YuiPluginSettingSchema>;
	schedules?: Array<{ id: string; every: string; handler: string }>;
};

export type YuiPlugin = {
	id: string;
	name: string;
	version: string;
	type: "starlark" | string;
	entry: string;
	dev: boolean;
	installed: boolean;
	sourceId?: string;
	sourceUrl?: string;
	installedAt?: string;
	plugin: YuiPluginMetadata;
	enabled: boolean;
	grantedPermissions: string[];
	administratorTrusted: boolean;
	commands?: Array<{ id: string; title: string; subtitle?: string }>;
	lastError?: string;
};

export type YuiPluginPageBlock = {
	type: "text" | "heading" | "stat" | "code" | "button" | "row" | string;
	title?: string;
	body?: string;
	label?: string;
	value?: unknown;
	command?: string;
	items?: YuiPluginPageBlock[];
};

export type YuiPluginShellPage = {
	id: string;
	pluginId: string;
	title: string;
	icon?: string;
	order?: number;
	blocks?: YuiPluginPageBlock[];
	css?: string;
};

export type YuiPluginShellAction = {
	id: string;
	pluginId: string;
	location: "home" | "tools" | string;
	title: string;
	icon?: string;
	command?: string;
};

export type YuiPluginExtensions = {
	pages: YuiPluginShellPage[];
	actions: YuiPluginShellAction[];
	css: string[];
};

export type YuiPluginSource = {
	id: string;
	url: string;
	name?: string;
	publisher?: string;
	signingKeys?: string[];
	lastRefreshed?: string;
	lastStatus: "pending" | "ok" | "error" | string;
	lastError?: string;
	discoveredPlugins?: number;
	createdAt?: string;
	updatedAt?: string;
};

export type YuiPluginCatalogEntry = {
	sourceId: string;
	sourceUrl: string;
	catalog: string;
	publisher: string;
	verified: boolean;
	updatedAt: string;
	plugin: {
		id: string;
		name: string;
		version: string;
		description?: string;
		icon?: string;
		permissions?: string[];
		sourceUrl: string;
		sourceSha256: string;
		signature: string;
	};
};

export type YuiPluginLog = {
	id: string;
	pluginId: string;
	action: string;
	permission?: string;
	at: string;
	ok: boolean;
	error?: string;
	detail?: string;
};

function dispatchPluginChanged(pluginId?: string) {
	window.dispatchEvent(
		new CustomEvent("yui:plugins-changed", { detail: { pluginId } }),
	);
}

async function mutatePlugins<T>(pluginId: string | undefined, action: Promise<T>) {
	const result = await action;
	dispatchPluginChanged(pluginId);
	return result;
}

export const plugins = {
	list: () => bridge.send<YuiPlugin[]>("plugins.installed.list"),
	enable: (id: string) => mutatePlugins(id, bridge.send<void>("plugins.enable", { id })),
	disable: (id: string) => mutatePlugins(id, bridge.send<void>("plugins.disable", { id })),
	uninstall: (id: string) => mutatePlugins(id, bridge.send<void>("plugins.uninstall", { id })),
	updatePermissions: (id: string, permissions: string[]) =>
		mutatePlugins(id, bridge.send<string[]>("plugins.permissions.update", { id, permissions })),
	updateAdministrator: (id: string, trusted: boolean) =>
		mutatePlugins(id, bridge.send<boolean>("plugins.administrator.update", { id, trusted })),
	getSettings: (id: string) =>
		bridge.send<Record<string, unknown>>("plugins.settings.get", { id }),
	updateSettings: (
		id: string,
		settings: Record<string, unknown>,
		secrets: Record<string, unknown>,
	) => mutatePlugins(
		id,
		bridge.send<Record<string, unknown>>("plugins.settings.update", { id, settings, secrets }),
	),
	logs: (id: string, limit = 50) =>
		bridge.send<YuiPluginLog[]>("plugins.logs.list", { id, limit }),
	extensions: () => bridge.send<YuiPluginExtensions>("plugins.extensions.list"),
	run: (id: string, command: string) =>
		bridge.send<unknown>("plugins.run", { id, command }),
};

export const pluginSources = {
	list: () => bridge.send<YuiPluginSource[]>("plugins.sources.list"),
	add: (url: string) => mutatePlugins(undefined, bridge.send<YuiPluginSource>("plugins.sources.add", { url })),
	remove: (id: string) => mutatePlugins(undefined, bridge.send<void>("plugins.sources.remove", { id })),
	refresh: (id: string) => mutatePlugins(undefined, bridge.send<YuiPluginSource>("plugins.sources.refresh", { id })),
};

export const pluginCatalog = {
	list: () => bridge.send<YuiPluginCatalogEntry[]>("plugins.catalog.list"),
	install: (catalogId: string) => mutatePlugins(undefined, bridge.send<YuiPlugin>("plugins.install", { catalogId })),
};

export function pluginCatalogEntryId(entry: YuiPluginCatalogEntry) {
	return `${entry.sourceId}:${entry.plugin.id}:${entry.plugin.version}`;
}

export function describePluginPermission(permission: string) {
	const copy: Record<string, { label: string; description: string }> = {
		"storage.read": { label: "Read storage", description: "Read plugin data." },
		"storage.write": { label: "Write storage", description: "Save plugin data." },
		"settings.read": { label: "Settings", description: "Read configured values." },
		"secrets.read": { label: "Secrets", description: "Read saved secrets." },
		"commands.register": { label: "Commands", description: "Add plugin actions." },
		events: { label: "Events", description: "Send and receive events." },
		"network.fetch": { label: "Network", description: "Fetch remote data." },
		"fs.read": { label: "Read files", description: "Read local files." },
		"fs.write": { label: "Write files", description: "Modify local files." },
		"fs.list": { label: "List files", description: "Browse folders." },
		"process.run": { label: "Processes", description: "Run executables." },
		"shell.run": { label: "Shell", description: "Run shell commands." },
		"shell.pages": { label: "Shell pages", description: "Add pages to Yui." },
		"shell.actions": { label: "Shell actions", description: "Add shell buttons." },
		"shell.css": { label: "Shell styling", description: "Style plugin shell pages." },
		"config.read": { label: "Config", description: "Read Yui config." },
		"system.status": { label: "System", description: "Read system status." },
	};
	return copy[permission] ?? { label: permission, description: "Allow capability." };
}

export function isAdministratorPermission(permission: string) {
	return [
		"process.run",
		"shell.run",
		"fs.write",
		"shell.pages",
		"shell.actions",
		"shell.css",
	].includes(permission);
}

const pluginStorage = store.collection<unknown>("plugin-storage");

function pluginStoragePrefix(pluginId: string) {
	return `${pluginId}:`;
}

function pluginStorageKeyFromId(pluginId: string, id: string) {
	const prefix = pluginStoragePrefix(pluginId);
	return id.startsWith(prefix) ? id.slice(prefix.length) : null;
}

export async function listPluginStorageKeys(pluginId: string) {
	const docs = await pluginStorage.list({ prefix: pluginStoragePrefix(pluginId) });
	return docs
		.map((doc) => pluginStorageKeyFromId(pluginId, doc.id))
		.filter((key): key is string => key !== null);
}

export async function clearPluginStorage(pluginId: string) {
	const docs = await pluginStorage.list({ prefix: pluginStoragePrefix(pluginId) });
	await Promise.all(docs.map((doc) => pluginStorage.delete(doc.id)));
	window.dispatchEvent(new CustomEvent("yui:plugin-storage-changed", { detail: { pluginId } }));
}
