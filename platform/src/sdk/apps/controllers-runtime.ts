/**
 * The app-facing `ctx.controllers` (plan 07). One implementation serves both
 * sides of the app sandbox: AppHost splices `yuiControllersRuntime.toString()`
 * into the sandbox script, and the host ctx calls it directly. It must stay
 * self-contained: no imports, no references outside the function, plain
 * syntax that survives minification.
 *
 * The backend does the privileged work: `call("start")` checks the
 * `controllers` permission and returns `{ session, players }`; later
 * `push("players", state)` and `push("input", data)` deliver bridge events.
 * Layouts are recomputed after `stateChanged()` (wired to ctx.state) and
 * whenever players change, and sent only when they differ.
 */

export type ControllersBackend = {
	call(method: string, args?: unknown[]): Promise<unknown>;
	/** Players or session changed: re-render the app's view. */
	changed(): void;
};

export type ControllersRuntime = {
	api: any;
	stateChanged(): void;
	push(event: string, data: unknown): void;
	dispose(): void;
};

export function yuiControllersRuntime(backend: ControllersBackend): ControllersRuntime {
	const maxNodes = 400;
	const events = ["input", "event", "join", "leave", "text", "system", "players"];
	const textTypes: Record<string, string> = { text: "normal", p: "normal", small: "small", h1: "large", h2: "large", h3: "normal" };
	let players: any[] = [];
	let session: any = null;
	let layoutFn: ((player: any) => any) | null = null;
	// Nothing is sent until a layout function produces something.
	let lastSent = "{}";
	let scheduled = false;
	let started: Promise<void> | null = null;
	let disposed = false;
	let framesWanted = false;
	const handlers = new Map<string, Set<(data: any) => void>>();

	function copy(player: any) {
		return {
			id: String(player.id),
			slot: typeof player.slot === "number" ? player.slot : null,
			name: String(player.name ?? ""),
			color: String(player.color ?? ""),
			host: Boolean(player.host),
			speaker: Boolean(player.speaker),
			connected: player.connected !== false,
			accountLinked: Boolean(player.account_linked),
			...(player.caps && typeof player.caps === "object"
				? { caps: { motion: Boolean(player.caps.motion), haptics: Boolean(player.caps.haptics), video: Boolean(player.caps.video) } }
				: {}),
			...(typeof player.relayed === "boolean" ? { relayed: player.relayed } : {}),
		};
	}

	function emit(event: string, data: any) {
		const set = handlers.get(event);
		if (!set) return;
		for (const handler of [...set]) {
			try {
				handler(data);
			} catch (error) {
				console.error(error);
			}
		}
	}

	function setState(state: any) {
		const next = Array.isArray(state?.players) ? state.players.map(copy) : [];
		const before = new Map(players.map((p) => [p.id, p]));
		const after = new Map(next.map((p: any) => [p.id, p]));
		players = next;
		session = state?.session?.code ? { code: String(state.session.code) } : null;
		for (const p of next) if (!before.has(p.id)) emit("join", p);
		for (const p of before.values()) if (!after.has(p.id)) emit("leave", p);
		emit("players", players.slice());
		schedule();
		backend.changed();
	}

	function start() {
		if (!started) {
			started = backend.call("start").then(
				(state) => {
					if (!disposed) setState(state);
				},
				(error) => {
					started = null;
					console.error(error);
				},
			);
		}
		return started;
	}

	function playerFor(id: string) {
		return players.find((p) => p.id === id) ?? { id, slot: null, name: "", color: "", host: false, speaker: false, connected: true, accountLinked: false };
	}

	function idOf(player: any) {
		const id = typeof player === "string" ? player : player?.id;
		if (typeof id !== "string" || !id) throw new Error("YUI_CONTROLLERS: a player or player id is required");
		return id;
	}

	/** ctx.ui nodes → pad layout nodes (contracts §3). */
	function normalize(value: any, budget: { nodes: number }): any {
		if (value === null || value === undefined || value === false || value === true) return null;
		if (typeof value === "string" || typeof value === "number") {
			return { type: "text", props: { text: String(value) } };
		}
		if (Array.isArray(value)) {
			const children = flatten(value, budget);
			return children.length === 1 ? children[0] : { type: "column", props: { gap: 12 }, children };
		}
		if (typeof value !== "object" || typeof value.type !== "string") return null;
		budget.nodes += 1;
		if (budget.nodes > maxNodes) throw new Error("YUI_CONTROLLERS: layout is too large");
		const props: Record<string, unknown> = {};
		for (const [key, item] of Object.entries(value.props ?? {})) {
			if (typeof item === "function" || item === undefined) continue;
			props[key] = JSON.parse(JSON.stringify(item));
		}
		const kids = Array.isArray(value.children) ? value.children : value.children === undefined ? [] : [value.children];
		if (value.type in textTypes) {
			const text = kids.filter((k: any) => typeof k === "string" || typeof k === "number").join("");
			if (text && props.text === undefined) props.text = text;
			if (props.size === undefined) props.size = textTypes[value.type];
			return { type: "text", props };
		}
		const node: any = { type: value.type, props };
		const children = flatten(kids, budget);
		if (children.length) node.children = children;
		return node;
	}

	function flatten(items: any[], budget: { nodes: number }): any[] {
		const out: any[] = [];
		for (const item of items) {
			if (Array.isArray(item)) out.push(...flatten(item, budget));
			else {
				const node = normalize(item, budget);
				if (node) out.push(node);
			}
		}
		return out;
	}

	function compute() {
		if (disposed) return;
		const layouts: Record<string, unknown> = {};
		if (layoutFn) {
			for (const player of players) {
				try {
					const node = normalize(layoutFn(player), { nodes: 0 });
					if (node) layouts[player.id] = node;
				} catch (error) {
					console.error(error);
				}
			}
		}
		const json = JSON.stringify(layouts);
		if (json === lastSent) return;
		lastSent = json;
		backend.call("layouts", [layouts]).catch((error) => console.error(error));
	}

	function schedule() {
		if (scheduled || disposed) return;
		scheduled = true;
		queueMicrotask(() => {
			scheduled = false;
			compute();
		});
	}

	function node(type: string) {
		return (props?: Record<string, unknown>, children?: unknown[]) => {
			const out: any = { type, props: props ?? {} };
			if (children !== undefined) out.children = Array.isArray(children) ? children : [children];
			return out;
		};
	}

	const ui = {
		row: node("row"),
		column: node("column"),
		grid: node("grid"),
		spacer: node("spacer"),
		text: (text: string, props?: Record<string, unknown>) => ({ type: "text", props: { ...(props ?? {}), text } }),
		button: node("button"),
		dpad: node("dpad"),
		stick: node("stick"),
		trackpad: node("trackpad"),
		keyboard: node("keyboard"),
		choice: node("choice"),
		list: node("list"),
		motion: node("motion"),
		surface: node("surface"),
		preset: (name: string, props?: Record<string, unknown>) => ({ type: "preset", props: { ...(props ?? {}), name } }),
	};

	const api = {
		get players() {
			void start();
			return players.slice();
		},
		get session() {
			void start();
			return session;
		},
		/** Resolves once players and the session are known. */
		ready() {
			return start();
		},
		player(id: string) {
			return players.find((p) => p.id === id) ?? null;
		},
		/** A node for the app's own view: the live join code and QR. */
		joinCard(props?: Record<string, unknown>) {
			void start();
			return { type: "pad-join", props: { ...(props ?? {}) } };
		},
		layout(fn: (player: any) => any) {
			if (typeof fn !== "function") throw new Error("YUI_CONTROLLERS: layout(player => tree) needs a function");
			void start();
			layoutFn = fn;
			schedule();
			return () => {
				if (layoutFn !== fn) return;
				layoutFn = null;
				schedule();
			};
		},
		on(event: string, handler: (data: any) => void) {
			if (!events.includes(event)) throw new Error(`YUI_CONTROLLERS: unknown event ${event}`);
			void start();
			let set = handlers.get(event);
			if (!set) {
				set = new Set();
				handlers.set(event, set);
			}
			set.add(handler);
			if (event === "input" && !framesWanted) {
				framesWanted = true;
				backend.call("frames", [true]).catch((error) => console.error(error));
			}
			return () => {
				set?.delete(handler);
			};
		},
		/**
		 * Vibrate one phone or "all". `pattern` is a vocabulary name, or custom
		 * on/off ms steps (<= 16 steps, each <= 1000 ms, <= 4 s in all); a bare
		 * number is one pulse. The phone resolves names, so timings can differ
		 * per platform; iOS and phones with vibration off ignore it. The kiosk
		 * drops haptics sent to one phone less than 50 ms apart.
		 */
		haptic(player: any, pattern: string | number | number[], options?: { intensity?: number }) {
			const names = ["tap", "success", "fail", "hit", "heavy", "heartbeat", "countdown", "win"];
			const target = player === "all" ? "all" : idOf(player);
			let wire: string | number[];
			if (typeof pattern === "string") {
				if (names.indexOf(pattern) < 0) return Promise.reject(new Error(`YUI_CONTROLLERS: unknown haptic ${pattern} (${names.join(", ")})`));
				wire = pattern;
			} else {
				wire = Array.isArray(pattern) ? pattern : [pattern];
				let total = 0;
				for (const ms of wire) {
					if (typeof ms !== "number" || !(ms >= 0 && ms <= 1000)) return Promise.reject(new Error("YUI_CONTROLLERS: haptic steps are numbers 0-1000 ms"));
					total += ms;
				}
				if (wire.length < 1 || wire.length > 16 || total > 4000) return Promise.reject(new Error("YUI_CONTROLLERS: haptic patterns are 1-16 steps and at most 4 s"));
			}
			const intensity = options?.intensity;
			if (intensity !== undefined && !(typeof intensity === "number" && intensity >= 0 && intensity <= 1)) {
				return Promise.reject(new Error("YUI_CONTROLLERS: haptic intensity is 0-1"));
			}
			return backend.call("haptic", intensity === undefined ? [target, wire] : [target, wire, { intensity }]);
		},
		sound(player: any, url: string, options?: { volume?: number }) {
			const target = player === "speaker" ? "speaker" : idOf(player);
			return backend.call("sound", [target, url, options?.volume]);
		},
		ui,
	};

	return {
		api,
		stateChanged: schedule,
		push(event: string, data: any) {
			if (disposed) return;
			if (event === "players") {
				setState(data);
				return;
			}
			if (event !== "input" || !data || typeof data !== "object") return;
			const player = playerFor(String(data.player_id ?? ""));
			switch (data.kind) {
				case "state":
					emit("input", { ...(data.value ?? {}), player });
					break;
				case "text":
					emit("text", { player, op: data.value?.op, text: data.value?.text });
					break;
				case "system":
					emit("system", { player, action: data.target, value: data.value });
					break;
				default:
					emit("event", { player, control: data.target, kind: data.kind, value: data.value });
			}
		},
		dispose() {
			disposed = true;
			handlers.clear();
			layoutFn = null;
		},
	};
}
