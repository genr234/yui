/**
 * Web bundles, the embed channel, asset URLs and app saves (contracts §6,
 * plan 11). One WebHost per mounted app: it answers the app's `ctx.web`,
 * `ctx.embeds`, `ctx.assets.url` and `ctx.saves` calls and talks to the
 * app's own pages (`ctx.ui.embed({ web })`), which the node view registers
 * here by channel.
 */
import { bridge } from "../bridge";
import type { ControllersHost } from "./controllers";
import type { YuiAsset, YuiPermission, YuiSimpleApp } from "./types";

type Ensure = (permission: YuiPermission) => Promise<void>;
type Handler = (data: unknown) => void;

export const EMBED_MAX_BYTES = 8 * 1024 * 1024;
const EMBED_RATE = 200; // messages per second, each direction
export const SAVE_MAX_BYTES = 8 * 1024 * 1024;
const SAVE_KEY = /^[a-z0-9][a-z0-9._-]{0,79}$/;
const DEMO_SAVES_KEY = "yui.demo.app-saves.v1";

export type SaveEntry = { key: string; size: number; updatedAt: string };
export type SaveBlob = SaveEntry & { data: Uint8Array };

type Frame = {
	channel: string;
	window: Window;
	origin: string;
	input: boolean;
	ready: boolean;
	queue: unknown[];
	sent: RateWindow;
	received: RateWindow;
};

type RateWindow = { start: number; count: number; warned: boolean };

/** The app's `web` manifest field: a folder path, or `{ runtimes }`. */
export function manifestRuntimes(app: YuiSimpleApp): string[] {
	const web = (app as { web?: unknown }).web;
	if (!web || typeof web !== "object") return [];
	const runtimes = (web as { runtimes?: unknown }).runtimes;
	return Array.isArray(runtimes) ? runtimes.filter((name): name is string => typeof name === "string") : [];
}

export function hasWebBundle(app: YuiSimpleApp) {
	const web = (app as { web?: unknown }).web;
	return typeof web === "string" || (typeof web === "object" && web !== null);
}

/** Rough size of a structured-clone payload (strings, typed arrays, plain objects). */
export function payloadBytes(value: unknown, depth = 0): number {
	if (value === null || value === undefined || depth > 32) return 0;
	if (typeof value === "string") return value.length * 2;
	if (typeof value === "number" || typeof value === "boolean") return 8;
	if (value instanceof ArrayBuffer) return value.byteLength;
	if (ArrayBuffer.isView(value)) return value.byteLength;
	if (value instanceof Blob) return value.size;
	if (Array.isArray(value)) return value.reduce((sum: number, item) => sum + payloadBytes(item, depth + 1), 0);
	if (typeof value === "object") {
		let sum = 0;
		for (const [key, item] of Object.entries(value as Record<string, unknown>)) sum += key.length * 2 + payloadBytes(item, depth + 1);
		return sum;
	}
	return 0;
}

/** ArrayBuffers inside a payload, to transfer instead of copy. */
export function transferables(value: unknown, out: ArrayBuffer[] = [], depth = 0): ArrayBuffer[] {
	if (!value || typeof value !== "object" || depth > 8) return out;
	if (value instanceof ArrayBuffer) {
		if (!out.includes(value)) out.push(value);
	} else if (ArrayBuffer.isView(value)) {
		const buffer = value.buffer;
		if (buffer instanceof ArrayBuffer && buffer.byteLength === value.byteLength && !out.includes(buffer)) out.push(buffer);
	} else if (Array.isArray(value)) {
		for (const item of value) transferables(item, out, depth + 1);
	} else if (Object.getPrototypeOf(value) === Object.prototype) {
		for (const item of Object.values(value)) transferables(item, out, depth + 1);
	}
	return out;
}

function allow(window: RateWindow, label: string) {
	const now = Date.now();
	if (now - window.start >= 1000) {
		window.start = now;
		window.count = 0;
	}
	window.count += 1;
	if (window.count <= EMBED_RATE) return true;
	if (!window.warned) {
		window.warned = true;
		console.warn(`[yui] ${label}: more than ${EMBED_RATE} embed messages a second; dropping`);
	}
	return false;
}

export function toBytes(value: unknown): Uint8Array {
	if (value instanceof Uint8Array) return value;
	if (value instanceof ArrayBuffer) return new Uint8Array(value);
	if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
	if (typeof value === "string") return new TextEncoder().encode(value);
	throw new Error("YUI_SAVES: data must be bytes (Uint8Array or ArrayBuffer) or a string");
}

export function bytesToBase64(bytes: Uint8Array) {
	let binary = "";
	const chunk = 0x8000;
	for (let i = 0; i < bytes.length; i += chunk) {
		binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
	}
	return btoa(binary);
}

export function base64ToBytes(value: string) {
	const binary = atob(value);
	const bytes = new Uint8Array(binary.length);
	for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
	return bytes;
}

function savePlayer(options: unknown) {
	const player = (options as { player?: unknown } | undefined)?.player;
	if (player === undefined || player === null) return undefined;
	const id = typeof player === "string" ? player : (player as { id?: unknown }).id;
	if (typeof id !== "string" || !id) throw new Error("YUI_SAVES: player must be a player or a player id");
	return id;
}

function checkKey(key: unknown): string {
	if (typeof key !== "string" || !SAVE_KEY.test(key)) {
		throw new Error("invalid_key: keys are 1-80 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit");
	}
	return key;
}

/** Bridge errors start with a code ("not_granted: …"); keep it on the error like ctx.profiles does. */
function saveError(error: unknown) {
	const message = error instanceof Error ? error.message : String(error);
	const code = /^([a-z_]+):/.exec(message)?.[1];
	const wrapped = new Error(message) as Error & { code?: string; name: string };
	wrapped.name = "YuiSaveError";
	if (code) wrapped.code = code;
	return wrapped;
}

export type WebHost = ReturnType<typeof createWebHost>;

export function createWebHost(app: YuiSimpleApp, ensure: Ensure, controllers?: ControllersHost) {
	const frames = new Map<string, Frame>();
	const handlers = new Map<string, Set<Handler>>();
	let originPromise: Promise<string> | null = null;
	let inputOff: (() => void) | null = null;
	let players: Array<Record<string, unknown>> = [];
	let disposed = false;

	const demo = () => import.meta.env?.VITE_YUI_DEMO === "true";

	function origin() {
		if (!originPromise) {
			originPromise = (async () => {
				if (!hasWebBundle(app)) throw new Error("YUI_WEB: this app has no web bundle (manifest `web`)");
				const result = await bridge.send<{ origin: string }>("webapps.origin", { app_id: app.id });
				if (!result?.origin) throw new Error("YUI_WEB: the kiosk did not return an origin");
				return result.origin.replace(/\/+$/, "");
			})();
			originPromise.catch(() => {
				originPromise = null;
			});
		}
		return originPromise;
	}

	async function runtime(name: unknown) {
		if (typeof name !== "string" || !name) throw new Error("YUI_WEB: a runtime name is required");
		if (!manifestRuntimes(app).includes(name)) {
			throw new Error(`YUI_PERMISSION_DENIED: runtime "${name}" is not listed in the manifest's web.runtimes`);
		}
		await ensure("assets:runtime");
		await origin();
		const result = await bridge.send<{ url: string; version?: string }>("webapps.runtime", { app_id: app.id, name });
		if (!result?.url) throw new Error(`YUI_WEB: runtime "${name}" is not available on this kiosk`);
		return result.url;
	}

	async function assetUrl(id: unknown) {
		if (typeof id !== "string" || !id) throw new Error("YUI_ASSETS: an asset id is required");
		// The overlay checks `assets:<kind>` for the asset's kind before asking.
		const kinds = (app.permissions ?? [])
			.filter((permission) => permission.startsWith("assets:"))
			.map((permission) => permission.slice("assets:".length));
		let found = false;
		for (const kind of kinds) {
			await ensure(`assets:${kind}`);
			const list = (await bridge.send<YuiAsset[] | null>("assets.list", { kind })) ?? [];
			if (list.some((asset) => asset.id === id)) {
				found = true;
				break;
			}
		}
		if (!found) throw new Error("YUI_ASSETS: asset is not available to this app");
		await origin();
		const result = await bridge.send<{ url: string }>("assets.url", { app_id: app.id, asset_id: id });
		if (!result?.url) throw new Error("YUI_ASSETS: asset is not available on this kiosk");
		return result.url;
	}

	// --- Saves -----------------------------------------------------------

	function demoStore(): Record<string, { data: string; updated_at: string }> {
		try {
			return JSON.parse(localStorage.getItem(DEMO_SAVES_KEY) || "{}");
		} catch {
			return {};
		}
	}

	function demoPath(key: string, player?: string) {
		return `${app.id}/${player ? `player-${player}` : "kiosk"}/${key}`;
	}

	const saves = {
		async put(key: unknown, data: unknown, options?: unknown): Promise<SaveEntry> {
			await ensure("saves");
			const safeKey = checkKey(key);
			const bytes = toBytes(data);
			if (bytes.byteLength > SAVE_MAX_BYTES) throw saveError(new Error(`too_large: saves are at most ${SAVE_MAX_BYTES} bytes`));
			const player = savePlayer(options);
			if (demo()) {
				const store = demoStore();
				const updatedAt = new Date().toISOString();
				store[demoPath(safeKey, player)] = { data: bytesToBase64(bytes), updated_at: updatedAt };
				try {
					localStorage.setItem(DEMO_SAVES_KEY, JSON.stringify(store));
				} catch {
					throw saveError(new Error("too_large: the demo's browser storage is full"));
				}
				return { key: safeKey, size: bytes.byteLength, updatedAt };
			}
			try {
				const result = await bridge.send<{ key: string; size: number; updated_at: string; queued?: boolean }>("saves.put", {
					app_id: app.id,
					key: safeKey,
					data: bytesToBase64(bytes),
					...(player ? { player_id: player } : {}),
				});
				return { key: result.key, size: result.size, updatedAt: result.updated_at, ...(result.queued ? { queued: true } : {}) };
			} catch (error) {
				throw saveError(error);
			}
		},
		async get(key: unknown, options?: unknown): Promise<SaveBlob | null> {
			await ensure("saves");
			const safeKey = checkKey(key);
			const player = savePlayer(options);
			if (demo()) {
				const entry = demoStore()[demoPath(safeKey, player)];
				if (!entry) return null;
				const data = base64ToBytes(entry.data);
				return { key: safeKey, data, size: data.byteLength, updatedAt: entry.updated_at };
			}
			try {
				const result = await bridge.send<{ key: string; data: string; size: number; updated_at: string } | null>("saves.get", {
					app_id: app.id,
					key: safeKey,
					...(player ? { player_id: player } : {}),
				});
				if (!result) return null;
				return { key: result.key, data: base64ToBytes(result.data), size: result.size, updatedAt: result.updated_at };
			} catch (error) {
				throw saveError(error);
			}
		},
		async list(options?: unknown): Promise<SaveEntry[]> {
			await ensure("saves");
			const player = savePlayer(options);
			if (demo()) {
				const prefix = demoPath("", player);
				return Object.entries(demoStore())
					.filter(([path]) => path.startsWith(prefix))
					.map(([path, entry]) => ({
						key: path.slice(prefix.length),
						size: Math.floor((entry.data.length * 3) / 4),
						updatedAt: entry.updated_at,
					}));
			}
			try {
				const result = await bridge.send<Array<{ key: string; size: number; updated_at: string }> | null>("saves.list", {
					app_id: app.id,
					...(player ? { player_id: player } : {}),
				});
				return (result ?? []).map((entry) => ({ key: entry.key, size: entry.size, updatedAt: entry.updated_at }));
			} catch (error) {
				throw saveError(error);
			}
		},
		async delete(key: unknown, options?: unknown): Promise<void> {
			await ensure("saves");
			const safeKey = checkKey(key);
			const player = savePlayer(options);
			if (demo()) {
				const store = demoStore();
				delete store[demoPath(safeKey, player)];
				localStorage.setItem(DEMO_SAVES_KEY, JSON.stringify(store));
				return;
			}
			try {
				await bridge.send("saves.delete", { app_id: app.id, key: safeKey, ...(player ? { player_id: player } : {}) });
			} catch (error) {
				throw saveError(error);
			}
		},
	};

	// --- Embed channel ---------------------------------------------------

	function deliver(frame: Frame, message: Record<string, unknown>) {
		if (!frame.ready) {
			frame.queue.push(message);
			return;
		}
		const transfer = message.type === "message" ? transferables(message.data) : [];
		try {
			frame.window.postMessage({ yuiEmbed: 1, ...message }, frame.origin, transfer);
		} catch (error) {
			console.warn(`[${app.id}] embed post failed:`, error);
		}
	}

	function post(channel: unknown, data: unknown) {
		if (typeof channel !== "string" || !channel) throw new Error("YUI_EMBEDS: a channel name is required");
		const frame = frames.get(channel);
		if (!frame) throw new Error(`YUI_EMBEDS: no page is open on channel "${channel}"`);
		if (payloadBytes(data) > EMBED_MAX_BYTES) throw new Error("YUI_EMBEDS: messages are at most 8 MiB");
		if (!allow(frame.sent, `${app.id}/${channel}`)) return;
		deliver(frame, { type: "message", data });
	}

	function on(channel: unknown, handler: Handler) {
		if (typeof channel !== "string" || !channel) throw new Error("YUI_EMBEDS: a channel name is required");
		let set = handlers.get(channel);
		if (!set) {
			set = new Set();
			handlers.set(channel, set);
		}
		set.add(handler);
		return () => {
			set?.delete(handler);
		};
	}

	function slimPlayer(player: Record<string, unknown> | undefined) {
		if (!player) return null;
		return { id: player.id, slot: player.slot ?? null, name: player.name, color: player.color };
	}

	function playersMessage() {
		return players.map((player) => ({
			id: player.id,
			slot: player.slot ?? null,
			name: player.name,
			color: player.color,
			host: Boolean(player.host),
			connected: player.connected !== false,
		}));
	}

	function broadcastPlayers() {
		for (const frame of frames.values()) if (frame.input) deliver(frame, { type: "players", players: playersMessage() });
	}

	async function startInput() {
		if (inputOff || !controllers || disposed) return;
		const offPush = controllers.addPush((event, data: any) => {
			if (event === "players") {
				players = Array.isArray(data?.players) ? data.players : [];
				broadcastPlayers();
				return;
			}
			if (event !== "input" || data?.kind !== "state") return;
			const player = players.find((candidate) => candidate.id === String(data.player_id ?? ""));
			const frame = { ...(data.value ?? {}), player: slimPlayer(player) ?? { id: String(data.player_id ?? ""), slot: null } };
			for (const target of frames.values()) {
				if (target.input && target.ready) target.window.postMessage({ yuiEmbed: 1, type: "input", frame }, target.origin);
			}
		});
		inputOff = offPush;
		try {
			const state: any = await controllers.call("start", []);
			players = Array.isArray(state?.players) ? state.players : [];
			await controllers.call("frames", [true]);
			broadcastPlayers();
		} catch (error) {
			console.warn(`[${app.id}] embed input:`, error instanceof Error ? error.message : error);
		}
	}

	/** Called by the node view for `ctx.ui.embed({ web, channel, input })` frames. */
	function attach(channel: string, target: Window, frameOrigin: string, options: { input: boolean }) {
		const frame: Frame = {
			channel,
			window: target,
			origin: frameOrigin,
			input: options.input,
			ready: false,
			queue: [],
			sent: { start: 0, count: 0, warned: false },
			received: { start: 0, count: 0, warned: false },
		};
		frames.set(channel, frame);
		if (options.input) void startInput();
		return () => {
			if (frames.get(channel) === frame) frames.delete(channel);
		};
	}

	/** Messages from embed pages (window "message" events in the overlay). */
	function receive(event: MessageEvent) {
		const message = event.data;
		if (!message || message.yuiEmbed !== 1) return false;
		for (const frame of frames.values()) {
			if (event.source !== frame.window || event.origin !== frame.origin) continue;
			if (message.type === "ready") {
				frame.ready = true;
				frame.window.postMessage({ yuiEmbed: 1, type: "ready" }, frame.origin);
				if (frame.input) frame.window.postMessage({ yuiEmbed: 1, type: "players", players: playersMessage() }, frame.origin);
				const queued = frame.queue.splice(0);
				for (const item of queued) deliver(frame, item as Record<string, unknown>);
				return true;
			}
			if (message.type !== "message") return true;
			if (payloadBytes(message.data) > EMBED_MAX_BYTES) {
				console.warn(`[${app.id}] dropped an embed message over 8 MiB`);
				return true;
			}
			if (!allow(frame.received, `${app.id}/${frame.channel}`)) return true;
			for (const handler of [...(handlers.get(frame.channel) ?? [])]) {
				try {
					handler(message.data);
				} catch (error) {
					console.error(error);
				}
			}
			return true;
		}
		return false;
	}

	function dispose() {
		disposed = true;
		inputOff?.();
		inputOff = null;
		frames.clear();
		handlers.clear();
	}

	return {
		origin,
		runtime,
		assetUrl,
		saves,
		post,
		on,
		attach,
		receive,
		dispose,
		/** For tests. */
		get players() {
			return players;
		},
	};
}

const hosts = new Map<string, WebHost>();

export function registerWebHost(appId: string, host: WebHost) {
	hosts.set(appId, host);
	return () => {
		if (hosts.get(appId) === host) hosts.delete(appId);
	};
}

export function webHostFor(appId: string) {
	return hosts.get(appId);
}

let listening = false;
/** One window listener routes embed messages to whichever app's page sent them. */
export function listenForEmbeds() {
	if (listening || typeof window === "undefined") return;
	listening = true;
	window.addEventListener("message", (event) => {
		const data = event.data;
		if (!data || data.yuiEmbed !== 1) return;
		for (const host of hosts.values()) if (host.receive(event)) return;
	});
}

function webString(source: string, field: string) {
	return new RegExp(`${field}\\s*:\\s*(["'])([\\s\\S]*?)\\1`).exec(source)?.[2];
}

function webStrings(source: string, field: string) {
	const match = new RegExp(`${field}\\s*:\\s*\\[([\\s\\S]*?)\\]`).exec(source);
	if (!match) return undefined;
	return [...match[1].matchAll(/(["'])([\s\S]*?)\1/g)].map((item) => item[2]);
}

/**
 * The manifest's `web` field: `web: "./web/"` or `web: { runtimes: [...] }`.
 * Only the part before `mount(` is read, so a `web:` prop inside the app's
 * code is never mistaken for it.
 */
export function webField(source: string): string | { runtimes?: string[]; sha256?: string } | undefined {
	const head = source.split(/\bmount\s*\(/)[0] ?? source;
	const text = /(?:^|[\s,{])web\s*:\s*(["'])([\s\S]*?)\1/.exec(head);
	if (text) return text[2];
	const object = /(?:^|[\s,{])web\s*:\s*\{([\s\S]*?)\}/.exec(head);
	if (!object) return undefined;
	const runtimes = webStrings(object[1], "runtimes");
	const sha256 = webString(object[1], "sha256");
	return { ...(runtimes ? { runtimes } : {}), ...(sha256 ? { sha256 } : {}) };
}

