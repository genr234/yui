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

	send<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
		return this.connect().then(
			() =>
				new Promise<T>((resolve, reject) => {
					const id = crypto.randomUUID();
					const message: Message = { id, method, params };
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
}

export const bridge = new Bridge();
