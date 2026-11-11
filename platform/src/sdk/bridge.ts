type Message = {
	id: string;
	method: string;
	params?: unknown;
};

type Response<T> = {
	id: string;
	result?: T;
	error?: string;
};

type EventFrame = {
	event: string;
	data?: unknown;
	id?: undefined;
};

export type BridgeEventListener<T = unknown> = (data: T) => void;

type Pending<T> = {
	resolve: (value: T) => void;
	reject: (reason?: unknown) => void;
};

const defaultBridgeURL = "ws://127.0.0.1:7071/ws";
const reconnectInitialMs = 500;
const reconnectMaxMs = 10_000;

export class Bridge {
	private socket?: WebSocket;
	private pending = new Map<string, Pending<unknown>>();
	private opened?: Promise<void>;
	private authToken = "";
	private listeners = new Map<string, Set<BridgeEventListener<any>>>();
	private reconnectDelay = reconnectInitialMs;
	private reconnectTimer?: number;
	private openListeners = new Set<() => void>();

	/**
	 * Called each time the socket (re)opens. Events pushed while it was down
	 * are gone, so state owners use this to fetch a fresh copy.
	 */
	onOpen(fn: () => void): () => void {
		this.openListeners.add(fn);
		return () => this.openListeners.delete(fn);
	}

	/**
	 * Subscribe to a pushed event frame `{ event, data }` from the controller.
	 * Listeners survive reconnects; while any listener exists the socket is
	 * kept connected. Returns an unsubscribe function.
	 */
	on<T = unknown>(event: string, fn: BridgeEventListener<T>): () => void {
		let set = this.listeners.get(event);
		if (!set) {
			set = new Set();
			this.listeners.set(event, set);
		}
		set.add(fn);
		this.ensureLive();

		return () => {
			const current = this.listeners.get(event);
			if (!current) return;
			current.delete(fn);
			if (current.size === 0) this.listeners.delete(event);
			if (!this.hasListeners() && this.reconnectTimer !== undefined) {
				window.clearTimeout(this.reconnectTimer);
				this.reconnectTimer = undefined;
			}
		};
	}

	private hasListeners(): boolean {
		return this.listeners.size > 0;
	}

	private ensureLive() {
		if (this.opened || this.reconnectTimer !== undefined) return;
		// Failures surface as a close, which schedules the next attempt.
		this.connect().catch(() => {});
	}

	private scheduleReconnect() {
		if (!this.hasListeners() || this.reconnectTimer !== undefined) return;
		const delay = this.reconnectDelay;
		this.reconnectDelay = Math.min(this.reconnectDelay * 2, reconnectMaxMs);
		this.reconnectTimer = window.setTimeout(() => {
			this.reconnectTimer = undefined;
			if (this.hasListeners()) this.ensureLive();
		}, delay);
	}

	private emit(frame: EventFrame) {
		const set = this.listeners.get(frame.event);
		if (!set) return;
		for (const fn of [...set]) {
			try {
				fn(frame.data);
			} catch (error) {
				console.error(`Bridge listener for ${frame.event} failed`, error);
			}
		}
	}

	send<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
		return this.connect().then(
			() =>
				new Promise<T>((resolve, reject) => {
					const id = crypto.randomUUID();
					const nextParams = { ...params };
					if (this.authToken && !method.startsWith("auth.")) {
						nextParams._auth_token = this.authToken;
					}
					const message: Message = { id, method, params: nextParams };
					this.pending.set(id, {
						resolve: resolve as (value: unknown) => void,
						reject,
					});
					this.socket?.send(JSON.stringify(message));
				}),
		);
	}

	private connect(): Promise<void> {
		if (this.opened) {
			return this.opened;
		}

		this.opened = new Promise((resolve, reject) => {
			const url = window.__YUI_BRIDGE_URL || defaultBridgeURL;
			const socket = new WebSocket(url);
			this.socket = socket;

			socket.onopen = () => {
				this.reconnectDelay = reconnectInitialMs;
				resolve();
				for (const fn of [...this.openListeners]) {
					try {
						fn();
					} catch (error) {
						console.error("Bridge open listener failed", error);
					}
				}
			};
			socket.onerror = () => reject(new Error(`Bridge unavailable at ${url}`));
			socket.onclose = () => {
				if (this.socket !== socket) return;
				this.opened = undefined;
				this.socket = undefined;
				for (const pending of this.pending.values()) {
					pending.reject(new Error("Bridge closed"));
				}
				this.pending.clear();
				this.scheduleReconnect();
			};
			socket.onmessage = (event) => {
				const frame = JSON.parse(event.data) as Response<unknown> | EventFrame;
				if ("event" in frame && typeof frame.event === "string" && frame.id == null) {
					this.emit(frame as EventFrame);
					return;
				}
				const response = frame as Response<unknown>;
				const pending = this.pending.get(response.id);
				if (!pending) return;
				this.pending.delete(response.id);
				if (response.error) {
					pending.reject(new Error(response.error));
				} else {
					pending.resolve(response.result);
				}
			};
		});

		return this.opened;
	}

	setAuthToken(token: string) {
		this.authToken = token;
	}

	clearAuthToken() {
		this.authToken = "";
	}
}

class DemoBridge {
	private values = new Map<string, unknown>();
	private listeners = new Map<string, Set<BridgeEventListener<any>>>();
	private padState: {
		session: {
			code: string;
			join_url: string;
			lobby_open?: boolean;
			wifi?: { ssid: string; password: string; qr: string };
			wifi_pending?: boolean;
		} | null;
		players: Array<Record<string, unknown>>;
	} = { session: null, players: [] };
	private wifiTimer?: number;

	/** Demo only: the kiosk hotspot "starts" a moment after the session. */
	private startFakeWifi() {
		window.clearTimeout(this.wifiTimer);
		if (!this.padState.session) return;
		this.padState = { ...this.padState, session: { ...this.padState.session, wifi_pending: true } };
		this.wifiTimer = window.setTimeout(() => {
			if (!this.padState.session) return;
			this.padState = {
				...this.padState,
				session: {
					...this.padState.session,
					wifi_pending: false,
					wifi: { ssid: "Yui KQXM", password: "k7mxq2pdva", qr: "WIFI:T:WPA;S:Yui KQXM;P:k7mxq2pdva;;" },
				},
			};
			this.emitPad();
		}, 1800);
	}
	private storeEpoch = 0;
	private wakeLocks = new Map<string, { id: string; owner: string; reason: string; since: string; expires: string }>();
	private config = {
		url: "https://demo.yui.local",
		chrome_path: "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		platform_enabled: true,
		platform_http_addr: "127.0.0.1:7072",
		platform_bridge_addr: "127.0.0.1:7071",
		platform_remote_debugging_port: 9222,
		auto_update_enabled: true,
		auto_update_repo: "yui-platform/yui",
		auto_update_interval_minutes: 60,
		event_mode: false,
		event_idle_minutes: 5,
	};

	async send<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
		await new Promise((resolve) => window.setTimeout(resolve, 45));

		const emptyAccount = {
			server_url: "https://cloud.yui.example",
			connected: true,
			needs_pairing: false,
			anonymous: false,
			syncing: false,
			last_sync_at: new Date().toISOString(),
			active_account: { id: "demo", name: "Yui Demo", kiosk_id: "lobby-display", sync_cursor: 128 },
			accounts: [{ id: "demo", name: "Yui Demo", kiosk_id: "lobby-display", sync_cursor: 128 }],
		};

		const responses: Record<string, unknown> = {
			"status.get": {
				state: "running",
				chrome_running: true,
				chrome_pid: 4821,
				restarts: 0,
				updated_at: new Date().toISOString(),
			},
			"diagnostics.get": { text: "Browser demo online\nController bridge simulated\nChrome healthy" },
			"config.get": this.config,
			"auth.status": { configured: true, locked: false, retry_after_seconds: 0 },
			"auth.verifyPin": { ok: true, token: "demo", status: { configured: true, locked: false, retry_after_seconds: 0 } },
			"auth.setPin": { ok: true, token: "demo", status: { configured: true, locked: false, retry_after_seconds: 0 } },
			"accounts.status": emptyAccount,
			"apps.installed.list": [],
			"apps.sources.list": [],
			"apps.catalog.list": [],
			"plugins.installed.list": [{
				id: "demo-health", name: "Kiosk Health", version: "1.0.0", type: "starlark",
				entry: "plugin.star", dev: false, installed: true, enabled: true,
				installedAt: new Date().toISOString(), grantedPermissions: ["system.status"],
				administratorTrusted: false,
				plugin: { schema: "yui.starlark-plugin.v0", id: "demo-health", name: "Kiosk Health", version: "1.0.0", description: "Monitors kiosk availability.", permissions: ["system.status"] },
			}],
			"plugins.sources.list": [],
			"plugins.catalog.list": [],
			"plugins.extensions.list": { pages: [], actions: [], css: [] },
			"plugins.logs.list": [],
			"plugins.settings.get": {},
			"update.check": { available: false, current_commit: "browser-demo", latest_commit: "browser-demo" },
			"store.list": [],
			"assets.status": {
				state: "syncing",
				paused_reasons: [],
				current: { id: "demo-asset", bytes_done: 620, byte_size: 1000 },
			},
			"store.keys": [],
			// Titles the demo apps would publish through ctx.library.
			"library.recent": [
				{
					app_id: "dev.genr.subway-surfers",
					id: "daily-challenge",
					title: "Daily challenge",
					players: 1,
					progress: 0.62,
					resume: { run: "daily" },
					published_at: new Date(Date.now() - 2 * 86_400_000).toISOString(),
					played_at: new Date(Date.now() - 25 * 60_000).toISOString(),
				},
				{
					app_id: "dev.genr.fruit-ninja",
					id: "zen",
					title: "Zen mode",
					players: 2,
					progress: 0.3,
					resume: { mode: "zen" },
					published_at: new Date(Date.now() - 2 * 86_400_000).toISOString(),
					played_at: new Date(Date.now() - 3 * 3_600_000).toISOString(),
				},
			],
			"native.status": { state: "idle" },
			"storage.get": null,
			"fs.list": [],
		};

		// Wake locks: kept in memory, never expire in the demo.
		if (method.startsWith("wakelock.")) return this.demoWakeLock(method, params) as T;

		// Web bundles (plan 11): the vite plugin serves them at /app-web/<app id>/.
		if (method === "webapps.origin") {
			return { origin: `${window.location.origin}/app-web/${String(params.app_id)}`, has_bundle: true } as T;
		}
		if (method === "webapps.runtime") {
			const name = String(params.name ?? "");
			const available = await fetch("/app-web/_demo-runtimes.json").then((r) => r.json()).catch(() => []);
			if (!Array.isArray(available) || !available.includes(name)) throw new Error("runtime is not available on this kiosk");
			return { url: `${window.location.origin}/app-web/${String(params.app_id)}/_runtime/${name}/`, version: "demo" } as T;
		}
		if (method === "assets.list") {
			const list = await fetch("/app-web/_demo-assets.json").then((r) => r.json()).catch(() => []);
			const kind = String(params.kind ?? "");
			return (Array.isArray(list) ? list : []).filter((asset: any) => !kind || asset.kind === kind).map(({ file: _file, ...asset }: any) => asset) as T;
		}
		if (method === "assets.request") {
			return { id: String(params.id), state: "ready" } as T;
		}
		if (method === "assets.url") {
			return {
				url: `${window.location.origin}/app-web/${String(params.app_id)}/_asset/${encodeURIComponent(String(params.asset_id))}`,
				expires_at: new Date(Date.now() + 12 * 3_600_000).toISOString(),
			} as T;
		}
		if (method === "config.update") {
			this.config = { ...this.config, ...params };
			return this.config as T;
		}
		if (method === "store.get") {
			const document = this.values.get(String(params.id)) as { value?: unknown } | undefined;
			return {
				id: params.id,
				value: document?.value ?? null,
				store_epoch: this.storeEpoch,
			} as T;
		}
		if (method === "store.put") {
			if (typeof params.store_epoch === "number" && params.store_epoch !== this.storeEpoch) {
				throw new Error("store scope changed");
			}
			const document = { ...params, updatedAt: new Date().toISOString() };
			this.values.set(String(params.id), document);
			return { ...document, store_epoch: this.storeEpoch } as T;
		}
		if (method === "store.delete") this.values.delete(String(params.id));
		if (method === "storage.set") this.values.set(String(params.key), params.value);
		if (method === "storage.delete") this.values.delete(String(params.key));
		if (method === "pad.session.end") {
			window.clearTimeout(this.wifiTimer);
			this.padState = { session: null, players: [] };
			this.emitPad();
			return this.padState as T;
		}
		if (method === "pad.session.start" && this.config.event_mode) {
			// Event mode: the lobby opens by itself and nobody is in it yet.
			this.padState = {
				session: { code: "KQXM", join_url: "https://yui.example/play/KQXM?t=OT56FOOG", lobby_open: true },
				players: [],
			};
			this.startFakeWifi();
			this.emitPad();
			return this.padState as T;
		}
		if (method === "pad.session.start") {
			this.padState = {
				session: { code: "KQXM", join_url: "https://yui.example/play/KQXM?t=OT56FOOG", lobby_open: false },
				players: [
					{ id: "p1", slot: 1, name: "Riccardo", color: "blue", host: true, speaker: true, connected: true, account_linked: true, rtt_ms: 18, input_hz: 60 },
					{ id: "p2", slot: 2, name: "Sam", color: "purple", host: false, connected: true, rtt_ms: 94, input_hz: 58.5, avatar: "🦊" },
					{ id: "p3", slot: 3, name: "Ada", color: "green", host: false, connected: false },
				],
			};
			this.startFakeWifi();
			this.emitPad();
			return this.padState as T;
		}
		if (this.padState.session && method === "pad.lobby") {
			this.padState = { ...this.padState, session: { ...this.padState.session, lobby_open: Boolean(params.open) } };
			this.emitPad();
		}
		if (method === "pad.kick") {
			this.padState = { ...this.padState, players: this.padState.players.filter((player) => player.id !== params.player_id) };
			this.emitPad();
		}
		if (method === "pad.host" || method === "pad.speaker") {
			const role = method === "pad.host" ? "host" : "speaker";
			this.padState = {
				...this.padState,
				players: this.padState.players.map((player) => ({ ...player, [role]: player.id === params.player_id })),
			};
			this.emitPad();
		}
		if (method === "pad.account.link_code") {
			return {
				code: String(100000 + Math.floor(Math.random() * 900000)),
				expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
			} as T;
		}
		if (method.startsWith("pad.profile.")) return this.demoProfile(method, params) as Promise<T>;
		if (method.startsWith("pad.") && method !== "pad.sensitive" && method !== "pad.volume") {
			return this.padState as T;
		}
		if (method === "pad.sensitive") return { active: Boolean(params.active) } as T;
		if (method === "pad.app.focus") {
			if (params.active) {
				this.focusedApp = String(params.app_id ?? "");
				const pending = this.lastLayouts.get(this.focusedApp);
				if (pending) this.simulatePhones(this.focusedApp, pending);
			} else if (!params.app_id || params.app_id === this.focusedApp) this.focusedApp = "";
			return { active: Boolean(params.active), app_id: this.focusedApp } as T;
		}
		if (method === "pad.app.layout") {
			this.simulatePhones(String(params.app_id ?? ""), (params.layouts ?? {}) as Record<string, any>);
			return null as T;
		}
		if (method === "pad.vpads.attach") {
			return { player_id: params.player_id, backend: "dsu", slot: params.slot, address: `127.0.0.1:26760/${params.slot}` } as T;
		}
		if (method === "pad.vpads.list") return [] as T;
		if (method === "pad.vpads.system_pad") return { backend: "dsu", slot: 0, address: "127.0.0.1:26761/0", buttons: [] } as T;
		if (method in responses) return responses[method] as T;

		return undefined as T;
	}

	private profileGrants = new Set<string>();
	private profileData = new Map<string, { data: unknown; version: number }>();

	/**
	 * Demo only: in-memory per-player profiles. A request is "allowed on the
	 * phone" after a short delay; the demo's linked player is p1.
	 */
	private async demoProfile(method: string, params: Record<string, unknown>): Promise<unknown> {
		const playerId = String(params.player_id ?? "");
		const key = `${playerId}\u0000${String(params.app_id ?? "")}`;
		const player = this.padState.players.find((p) => p.id === playerId);
		if (!player?.account_linked) throw new Error("not_linked: this player is not playing as a Yui account");
		if (method === "pad.profile.request") {
			if (!this.profileGrants.has(key)) await new Promise((resolve) => window.setTimeout(resolve, 900));
			this.profileGrants.add(key);
			return { granted: true };
		}
		if (!this.profileGrants.has(key)) {
			throw new Error("not_granted: this player has not allowed this game to use their profile");
		}
		const current = this.profileData.get(key) ?? { data: null, version: 0 };
		if (method === "pad.profile.get") {
			return { profile_id: `demo-${playerId}`, data: current.data, version: current.version };
		}
		if (method === "pad.profile.set") {
			if (params.version !== current.version) {
				throw new Error(`version_conflict: current version ${current.version}`);
			}
			if (JSON.stringify(params.data ?? null).length > 64 * 1024) throw new Error("too_large: a profile can hold at most 64 KB");
			this.profileData.set(key, { data: params.data ?? null, version: current.version + 1 });
			return { version: current.version + 1 };
		}
		throw new Error(`unknown method: ${method}`);
	}

	private focusedApp = "";
	private lastLayouts = new Map<string, Record<string, any>>();
	private phoneTimers = new Map<string, number>();

	/**
	 * Demo only: the two demo phones act on the layouts an app sends them.
	 * A guest picks the first option of a `choice`; the host taps its first
	 * button a little later. Lets ctx.controllers apps play without phones.
	 * The layouts are kept on `window.__yuiDemoPadLayouts` for inspection.
	 */
	private simulatePhones(appId: string, layouts: Record<string, any>) {
		(window as any).__yuiDemoPadLayouts = layouts;
		this.lastLayouts.set(appId, layouts);
		const find = (node: any, type: string): any => {
			if (!node || typeof node !== "object") return null;
			if (node.type === type && node.props?.id) return node;
			for (const child of node.children ?? []) {
				const hit = find(child, type);
				if (hit) return hit;
			}
			return null;
		};
		for (const player of this.padState.players) {
			const id = String(player.id);
			window.clearTimeout(this.phoneTimers.get(id));
			const tree = layouts[id];
			const choice = find(tree, "choice");
			const button = player.host ? find(tree, "button") : null;
			const target = choice ?? button;
			if (!target || appId !== this.focusedApp) continue;
			const value = choice ? choice.props.options?.[0]?.id : undefined;
			const delay = choice ? (player.host ? 1800 : 1200) : 2600;
			this.phoneTimers.set(
				id,
				window.setTimeout(() => {
					for (const fn of [...(this.listeners.get("pad.app.input") ?? [])]) {
						fn({ app_id: appId, player_id: id, target: target.props.id, kind: "tap", value });
					}
				}, delay),
			);
		}
	}

	on<T = unknown>(event: string, fn: BridgeEventListener<T>): () => void {
		let set = this.listeners.get(event);
		if (!set) {
			set = new Set();
			this.listeners.set(event, set);
		}
		set.add(fn as BridgeEventListener<any>);
		if (event === "pad.cursor") this.startFakeCursor();
		return () => {
			const current = this.listeners.get(event);
			if (!current) return;
			current.delete(fn as BridgeEventListener<any>);
			if (current.size === 0) this.listeners.delete(event);
		};
	}

	/** The demo bridge never drops, so there is nothing to resync. */
	onOpen(_fn: () => void): () => void {
		return () => {};
	}

	private cursorTimer?: number;

	/**
	 * Demo only: a phone cursor that glides around the page and taps every
	 * few seconds, so the overlay's cursor can be eyeballed without a phone.
	 */
	private startFakeCursor() {
		if (this.cursorTimer !== undefined) return;
		const started = performance.now();
		let lastClick = 0;
		this.cursorTimer = window.setInterval(() => {
			const set = this.listeners.get("pad.cursor");
			if (!set) {
				window.clearInterval(this.cursorTimer);
				this.cursorTimer = undefined;
				return;
			}
			const t = (performance.now() - started) / 1000;
			const w = window.innerWidth;
			const h = window.innerHeight;
			const click = t - lastClick > 2.4;
			if (click) lastClick = t;
			const cursor = {
				player_id: "p1",
				x: w / 2 + Math.sin(t * 0.7) * w * 0.32,
				y: h / 2 + Math.sin(t * 1.1) * h * 0.28,
				visible: true,
				color: "blue",
				click,
			};
			for (const fn of [...set]) fn(cursor);
		}, 50);
	}

	private demoWakeLock(method: string, params: Record<string, unknown>) {
		const view = () => {
			const holds = [...this.wakeLocks.values()];
			return { holds, active: holds.length > 0 };
		};
		const changed = () => {
			for (const fn of [...(this.listeners.get("wakelock.changed") ?? [])]) fn(view());
		};
		const id = String(params.id ?? "");
		switch (method) {
			case "wakelock.acquire": {
				const now = new Date().toISOString();
				const hold = { id: `wl_demo_${Math.random().toString(36).slice(2)}`, owner: String(params.app_id ?? ""), reason: String(params.reason ?? ""), since: now, expires: now };
				this.wakeLocks.set(hold.id, hold);
				changed();
				return { ...hold, ttl_ms: 60_000 };
			}
			case "wakelock.renew":
				return this.wakeLocks.has(id) ? { ok: true, hold: { ...this.wakeLocks.get(id), ttl_ms: 60_000 } } : { ok: false };
			case "wakelock.release": {
				const ok = this.wakeLocks.delete(id);
				if (ok) changed();
				return { ok };
			}
			default:
				return view();
		}
	}

	private emitPad() {
		const set = this.listeners.get("pad.players");
		if (!set) return;
		for (const fn of [...set]) {
			fn(this.padState);
		}
	}

	setAuthToken(_token: string) {}
	clearAuthToken() {}
}

export const bridge = import.meta.env.VITE_YUI_DEMO === "true" ? new DemoBridge() : new Bridge();
