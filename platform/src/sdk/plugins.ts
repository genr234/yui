import { bridge } from "./bridge";

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
	commands?: Array<{ id: string; title: string; subtitle?: string }>;
	lastError?: string;
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

export const plugins = {
	list: () => bridge.send<YuiPlugin[]>("plugins.installed.list"),
	enable: (id: string) => bridge.send<void>("plugins.enable", { id }),
	disable: (id: string) => bridge.send<void>("plugins.disable", { id }),
	uninstall: (id: string) => bridge.send<void>("plugins.uninstall", { id }),
	updatePermissions: (id: string, permissions: string[]) =>
		bridge.send<string[]>("plugins.permissions.update", { id, permissions }),
	getSettings: (id: string) =>
		bridge.send<Record<string, unknown>>("plugins.settings.get", { id }),
	updateSettings: (
		id: string,
		settings: Record<string, unknown>,
		secrets: Record<string, unknown>,
	) => bridge.send<Record<string, unknown>>("plugins.settings.update", { id, settings, secrets }),
	logs: (id: string, limit = 50) =>
		bridge.send<YuiPluginLog[]>("plugins.logs.list", { id, limit }),
	run: (id: string, command: string) =>
		bridge.send<unknown>("plugins.run", { id, command }),
};

export const pluginSources = {
	list: () => bridge.send<YuiPluginSource[]>("plugins.sources.list"),
	add: (url: string) => bridge.send<YuiPluginSource>("plugins.sources.add", { url }),
	remove: (id: string) => bridge.send<void>("plugins.sources.remove", { id }),
	refresh: (id: string) => bridge.send<YuiPluginSource>("plugins.sources.refresh", { id }),
};

export const pluginCatalog = {
	list: () => bridge.send<YuiPluginCatalogEntry[]>("plugins.catalog.list"),
	install: (catalogId: string) => bridge.send<YuiPlugin>("plugins.install", { catalogId }),
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
		"config.read": { label: "Config", description: "Read Yui config." },
		"system.status": { label: "System", description: "Read system status." },
	};
	return copy[permission] ?? { label: permission, description: "Allow capability." };
}
