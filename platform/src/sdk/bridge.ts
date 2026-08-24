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

type Pending<T> = {
	resolve: (value: T) => void;
	reject: (reason?: unknown) => void;
};

const defaultBridgeURL = "ws://127.0.0.1:7071/ws";

export class Bridge {
	private socket?: WebSocket;
	private pending = new Map<string, Pending<unknown>>();
	private opened?: Promise<void>;
	private authToken = "";

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

			socket.onopen = () => resolve();
			socket.onerror = () => reject(new Error(`Bridge unavailable at ${url}`));
			socket.onclose = () => {
				this.opened = undefined;
				this.socket = undefined;
				for (const pending of this.pending.values()) {
					pending.reject(new Error("Bridge closed"));
				}
				this.pending.clear();
			};
			socket.onmessage = (event) => {
				const response = JSON.parse(event.data) as Response<unknown>;
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
			"store.keys": [],
			"storage.get": null,
			"fs.list": [],
		};

		if (method === "config.update") {
			this.config = { ...this.config, ...params };
			return this.config as T;
		}
		if (method === "store.get") return (this.values.get(String(params.id)) ?? null) as T;
		if (method === "store.put") {
			const document = { ...params, updatedAt: new Date().toISOString() };
			this.values.set(String(params.id), document);
			return document as T;
		}
		if (method === "store.delete") this.values.delete(String(params.id));
		if (method === "storage.set") this.values.set(String(params.key), params.value);
		if (method === "storage.delete") this.values.delete(String(params.key));
		if (method in responses) return responses[method] as T;

		return undefined as T;
	}

	setAuthToken(_token: string) {}
	clearAuthToken() {}
}

export const bridge = import.meta.env.VITE_YUI_DEMO === "true" ? new DemoBridge() : new Bridge();
