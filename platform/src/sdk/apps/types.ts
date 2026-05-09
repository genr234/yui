export const SIMPLE_APP_SCHEMA = "yui.simple-js.v0" as const;

export type YuiPermission =
	| "storage"
	| "commands"
	| "clipboard.read"
	| "clipboard.write"
	| "notifications"
	| "network.fetch"
	| "fullscreen"
	| string;

export type YuiChildren =
	| YuiNode
	| string
	| number
	| boolean
	| null
	| undefined
	| YuiChildren[];

export type YuiNode = {
	type: string;
	props?: Record<string, unknown>;
	children?: YuiChildren[];
};

export type YuiRenderFunction = () => YuiNode | string | number | boolean | null | undefined;

export type YuiSimpleApp = {
	schema: typeof SIMPLE_APP_SCHEMA;
	id: string;
	name: string;
	version: string;
	description?: string;
	author?: string;
	homepage?: string;
	license?: string;
	icon?: string;
	category?: string;
	tags?: string[];
	permissions?: YuiPermission[];
	runtime?: string;
	mount(ctx: YuiContext): YuiRenderFunction | Promise<YuiRenderFunction | void> | void;
	install?(ctx: YuiContext): void | Promise<void>;
	activate?(ctx: YuiContext): void | Promise<void>;
	suspend?(ctx: YuiContext): void | Promise<void>;
	unmount?(ctx: YuiContext): void | Promise<void>;
	uninstall?(ctx: YuiContext): void | Promise<void>;
};

export type YuiAppInfo = {
	id: string;
	name: string;
	version: string;
};

export type YuiContext = {
	app: YuiAppInfo;
	env: {
		runtimeVersion: string;
		platform: "windows" | "linux" | "darwin" | "web" | "unknown";
		mode: "dev" | "installed";
		theme: "light" | "dark" | "system";
	};
	ui: YuiUiApi;
	storage: YuiStorageApi;
	commands: YuiCommandApi;
	events: YuiEventApi;
	clipboard: YuiClipboardApi;
	notifications: YuiNotificationApi;
	network: YuiNetworkApi;
	fullscreen: YuiFullscreenApi;
	log: Console;
	state<T extends object>(initial: T): T;
	toast(message: string, options?: { kind?: string; durationMs?: number }): Promise<void>;
	open(target: string, options?: { where?: "shell" | "external" | "new-view" }): Promise<void>;
	dispose(): void;
};

export type YuiUiApi = Record<string, (...args: any[]) => YuiNode | YuiNode[] | null>;

export type YuiStorageApi = {
	get<T = unknown>(key: string): Promise<T | null>;
	set(key: string, value: unknown): Promise<void>;
	delete(key: string): Promise<void>;
	keys(): Promise<string[]>;
	clear(): Promise<void>;
};

export type YuiCommandApi = {
	register(command: {
		id: string;
		title: string;
		subtitle?: string;
		icon?: string;
		shortcut?: string;
		run: () => void | Promise<void>;
	}): () => void;
};

export type YuiEventApi = {
	on(event: string, handler: (data: unknown) => void | Promise<void>): () => void;
	emit(event: string, data?: unknown): Promise<void>;
};

export type YuiClipboardApi = {
	readText(): Promise<string>;
	writeText(text: string): Promise<void>;
};

export type YuiNotificationApi = {
	send(notification: { title: string; body?: string; icon?: string }): Promise<void>;
};

export type YuiNetworkApi = {
	fetch(url: string, options?: RequestInit): Promise<Response>;
};

export type YuiFullscreenApi = {
	enter(): Promise<void>;
	exit(): Promise<void>;
	toggle(): Promise<void>;
	isActive(): boolean;
};

export type YuiDevApp = {
	id: string;
	name: string;
	version: string;
	type: "simple-js";
	entry: string;
	dev: boolean;
	installed?: boolean;
	sourceId?: string;
	sourceUrl?: string;
	installedAt?: string;
	app: YuiSimpleApp;
	source: string;
};
