import type { LibraryTitle, LibraryTitleInput } from "../library";
import type { YuiLaunchItem } from "./launch-intent";

export const SIMPLE_APP_SCHEMA = "yui.simple-js.v0" as const;

export type YuiPermission =
	| "storage"
	| "commands"
	| "clipboard.read"
	| "clipboard.write"
	| "notifications"
	| "network.fetch"
	| "fullscreen"
	| "native"
	| "library"
	| "virtual-pads"
	| "controllers"
	| "profiles"
	| "audio.speaker"
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
	/**
	 * Phone controllers. Declaring this (even `{}`) gives the app the phones
	 * while it is open; `keymap` drives an embedded web game with no code.
	 */
	controllers?: YuiControllersManifest;
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
	native: YuiNativeApi;
	library: YuiLibraryApi;
	assets: YuiAssetsApi;
	controllers: YuiControllersApi;
	/** Only with the `profiles` permission declared. */
	profiles?: YuiProfilesApi;
	virtualPads: YuiVirtualPadsApi;
	/** What the shell opened this app for, e.g. a library title from Jump back in. */
	launch: { item: YuiLaunchItem | null };
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

export type YuiNativeConfigFile = {
	/** Relative to {{dataDir}}, forward slashes. */
	path: string;
	/** Template: {{asset:<id>}}, {{dataDir}}, {{virtualPad:<n>}}. */
	content: string;
};

export type YuiNativeSpec = {
	/** Id of a `runtime` asset (.zip or .7z). */
	runtime: string;
	/** Path of the program inside the runtime, e.g. "bin/program.exe". */
	executable: string;
	/** Templated like config files. */
	args?: string[];
	files?: YuiNativeConfigFile[];
	virtualPads?: number[];
	audio?: "none" | "speaker";
	/**
	 * Paths inside the data directory that hold save data (relative, forward
	 * slashes, no ".."). They are backed up to the account when the session
	 * ends and a newer backup is restored before launch.
	 */
	saves?: string[];
};

/** Save-file backup progress of a session that declared `saves`. */
export type YuiNativeSaves = {
	state: "restoring" | "restored" | "up_to_date" | "kept_local" | "backing_up" | "saved" | "remote_newer" | "nothing" | "offline" | "error";
	error?: string;
	saved_at?: string;
};

export type YuiNativeStatus = {
	/** "busy" means another app's session is active. */
	state: "idle" | "preparing" | "running" | "stopping" | "exited" | "crashed" | "failed" | "busy";
	session_id?: string;
	app_id?: string;
	runtime?: string;
	audio?: string;
	virtual_pads?: number[];
	started_at?: string;
	ended_at?: string;
	exit_code?: number;
	error?: string;
	saves?: YuiNativeSaves;
};

export type YuiNativeEvent = "status" | "started" | "exited" | "crashed";

export type YuiNativeApi = {
	launch(spec: YuiNativeSpec): Promise<YuiNativeStatus>;
	stop(options?: { force?: boolean }): Promise<YuiNativeStatus>;
	status(): Promise<YuiNativeStatus>;
	/** Back up the declared `saves` of this app's current or latest session now. */
	backupSaves(): Promise<YuiNativeStatus>;
	on(event: YuiNativeEvent, handler: (status: YuiNativeStatus) => void): () => void;
};

export type YuiLibraryApi = {
	publish(titles: LibraryTitleInput[], options?: { replace?: boolean }): Promise<LibraryTitle[]>;
	played(id: string, options?: { progress?: number; resume?: unknown }): Promise<LibraryTitle>;
	list(): Promise<LibraryTitle[]>;
	remove(ids: string[]): Promise<void>;
};

/** A synced asset (contracts §1). Never carries a file path. */
export type YuiAsset = {
	id: string;
	kind: string;
	title: string;
	/** What the server's analyzers or the upload form recorded, e.g. `platform`, `cover_url`, `runtime`, `version`. */
	metadata: Record<string, any>;
	state: "queued" | "downloading" | "paused" | "verifying" | "ready" | "failed" | string;
	bytes_done: number;
	byte_size: number;
};

/** `ctx.assets("game")` lists the synced assets of a kind (permission `assets:<kind>`). */
export type YuiAssetsApi = {
	(kind: string): Promise<YuiAsset[]>;
	/** Runs when the asset list or sync progress changes; re-list to read it. */
	onChange(handler: () => void): () => void;
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

export type YuiKeymapDirection = "up" | "down" | "left" | "right";

/** Key names: Enter, Space, Tab, Escape, Backspace, Delete, Home, End, PageUp, PageDown, ArrowUp/Down/Left/Right, or one letter or digit. */
export type YuiKeymapKey = string;

export type YuiKeymapControl =
	| { type: "swipe"; keys: Partial<Record<YuiKeymapDirection, YuiKeymapKey>>; tap?: YuiKeymapKey; label?: string }
	| { type: "dpad"; keys: Partial<Record<YuiKeymapDirection, YuiKeymapKey>>; label?: string }
	| { type: "button"; label: string; key?: YuiKeymapKey; mouse?: "left" }
	| { type: "trackpad"; mode: "absolute"; mouse: "drag"; label?: string }
	| { type: "trackpad"; mode?: "relative"; label?: string };

export type YuiKeymap = {
	/** "host" (default): only the host plays; "all": every phone does. */
	players?: "host" | "all";
	controls: YuiKeymapControl[];
};

export type YuiControllersManifest = {
	keymap?: YuiKeymap;
};

/** A phone in the play session. */
export type YuiPlayer = {
	id: string;
	slot: number | null;
	name: string;
	color: string;
	host: boolean;
	speaker: boolean;
	connected: boolean;
	/** Playing as a Yui account this session. Never says which account. */
	accountLinked: boolean;
};

export type YuiPlayerRef = YuiPlayer | string;

/** A pad layout node (contracts §3); ctx.ui text nodes are converted. */
export type YuiPadNode = YuiNode;

export type YuiControllerEvents = {
	/** A control event: `tap` on a button or choice, `submit` on a keyboard, ... */
	event: { player: YuiPlayer; control: string; kind: string; value?: unknown };
	/** A whole input frame (buttons bitmask, sticks, motion, pointer). */
	input: {
		player: YuiPlayer;
		buttons: number;
		lx: number;
		ly: number;
		rx: number;
		ry: number;
		accel: number[];
		gyro: number[];
		px: number;
		py: number;
		seq: number;
		tMs: number;
	};
	text: { player: YuiPlayer; op: "insert" | "backspace" | "enter"; text?: string };
	/** A Home-sheet action: resume, quit, save_state, load_state, ... */
	system: { player: YuiPlayer; action: string; value?: unknown };
	join: YuiPlayer;
	leave: YuiPlayer;
	players: YuiPlayer[];
};

export type YuiControllersApi = {
	readonly players: YuiPlayer[];
	/** The play session's join code, or null. The QR is drawn by `joinCard()`. */
	readonly session: { code: string } | null;
	ready(): Promise<void>;
	player(id: string): YuiPlayer | null;
	joinCard(props?: { size?: number }): YuiNode;
	/** Each phone's layout; re-run when ctx.state or the players change. */
	layout(fn: (player: YuiPlayer) => YuiPadNode | null | undefined): () => void;
	on<E extends keyof YuiControllerEvents>(event: E, handler: (data: YuiControllerEvents[E]) => void): () => void;
	haptic(player: YuiPlayerRef, pattern: number | number[]): Promise<unknown>;
	sound(player: YuiPlayerRef | "speaker", url: string, options?: { volume?: number }): Promise<unknown>;
	/** Pad node builders: button, choice, dpad, stick, trackpad, keyboard, list, motion, preset, text, row, column, grid, spacer. */
	ui: Record<string, (...args: any[]) => YuiPadNode>;
};

/** Rejection from `ctx.profiles`: `code` is one of these, `version` is the current one on a conflict. */
export type YuiProfileError = Error & {
	name: "YuiProfileError";
	code: "not_linked" | "not_granted" | "version_conflict" | "too_large";
	version?: number;
};

export type YuiProfile = {
	/** Asks the player on their phone to let this app keep a profile. Resolves `{ granted: false }` for a "no" or no answer in 60 s. */
	request(): Promise<{ granted: boolean }>;
	/** Rejects with `code: "not_granted"` until the player allowed the app. `data` is null and `version` 0 before the first save. */
	get(): Promise<{ profileId: string; data: any; version: number }>;
	/** `version` is the one `get` returned; rejects with `code: "version_conflict"` and the current `version` when it moved. Up to 64 KB of JSON. */
	set(data: unknown, version: number): Promise<{ version: number }>;
};

/** Present only when the manifest declares the `profiles` permission. */
export type YuiProfilesApi = {
	/** Whether the player plays as a Yui account (false for guests, and before `ctx.controllers.ready()`). */
	available(player: YuiPlayerRef): boolean;
	/** The player's profile for this app, or null for a guest. */
	for(player: YuiPlayerRef): YuiProfile | null;
};

export type YuiVirtualPadAttachment = {
	player_id: string;
	backend: "dsu";
	slot: number;
	pointer_stick?: boolean;
	/** "host:port/slot", the same value {{virtualPad:<slot>}} expands to. */
	address: string;
};

export type YuiVirtualPadsApi = {
	attach(
		player: YuiPlayerRef,
		options: { backend?: "dsu"; slot: number; pointerStick?: boolean },
	): Promise<YuiVirtualPadAttachment>;
	detach(player: YuiPlayerRef): Promise<void>;
	list(): Promise<YuiVirtualPadAttachment[]>;
	/** The pad whose buttons the app binds to its program's hotkeys ({{systemPad}}). */
	systemPad(): Promise<{ backend: "dsu"; slot: 0; address: string; buttons: string[] }>;
	/** Press, release or tap (default) a system pad button by name ("Start", "L1", ...). */
	press(button: string, action?: "press" | "release" | "tap"): Promise<void>;
};
