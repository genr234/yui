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
		session: { code: string; join_url: string; lobby_open?: boolean } | null;
		players: Array<Record<string, unknown>>;
	} = { session: null, players: [] };
	private storeEpoch = 0;
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
			this.padState = { session: null, players: [] };
			this.emitPad();
			return this.padState as T;
		}
		if (method === "pad.session.start") {
			this.padState = {
				session: { code: "KQXM", join_url: "https://yui.example/play/KQXM?t=OT56FOOG", lobby_open: false },
				players: [
					{ id: "p1", slot: 1, name: "Riccardo", color: "blue", host: true, speaker: true, connected: true },
					{ id: "p2", slot: 2, name: "Sam", color: "purple", host: false, connected: true },
					{ id: "p3", slot: 3, name: "Ada", color: "green", host: false, connected: false },
				],
			};
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
		if (method.startsWith("pad.") && method !== "pad.sensitive" && method !== "pad.volume") {
			return this.padState as T;
		}
		if (method === "pad.sensitive") return { active: Boolean(params.active) } as T;
		if (method in responses) return responses[method] as T;

		return undefined as T;
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
