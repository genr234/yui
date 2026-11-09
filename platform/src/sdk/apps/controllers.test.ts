import { beforeEach, describe, expect, mock, test } from "bun:test";

type Sent = { method: string; params: any };
const sent: Sent[] = [];
const listeners = new Map<string, Set<(data: unknown) => void>>();
let padPlayers: unknown = { session: { code: "KQXM", join_url: "https://yui.example/play/KQXM?t=SECRET" }, players: [] };

mock.module("../bridge", () => ({
	bridge: {
		send: (method: string, params: any = {}) => {
			sent.push({ method, params });
			if (method === "pad.players") return Promise.resolve(padPlayers);
			return Promise.resolve(null);
		},
		on: (event: string, fn: (data: unknown) => void) => {
			let set = listeners.get(event);
			if (!set) listeners.set(event, (set = new Set()));
			set.add(fn);
			return () => set?.delete(fn);
		},
	},
}));

const { yuiControllersRuntime } = await import("./controllers-runtime");
const { createControllersHost } = await import("./controllers");
const { sandboxApiSource } = await import("./sandbox-apis");

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

function fire(event: string, data: unknown) {
	for (const fn of [...(listeners.get(event) ?? [])]) fn(data);
}

const p1 = { id: "p1", slot: 1, name: "Ada", color: "blue", host: true, connected: true };
const p2 = { id: "p2", slot: 2, name: "Bo", color: "red", host: false, connected: true };

/** A backend that records layouts and serves `start`. */
function fakeBackend() {
	const layouts: Array<Record<string, any>> = [];
	const calls: string[] = [];
	let changed = 0;
	return {
		layouts,
		calls,
		changes: () => changed,
		backend: {
			call: async (method: string, args: unknown[] = []) => {
				calls.push(method);
				if (method === "start") return { session: { code: "KQXM" }, players: [p1, p2] };
				if (method === "layouts") layouts.push(args[0] as Record<string, any>);
				return undefined;
			},
			changed: () => {
				changed += 1;
			},
		},
	};
}

/** ctx.state as the sandbox builds it: every write schedules a relayout. */
function reactive<T extends object>(initial: T, changed: () => void): T {
	return new Proxy(initial, {
		set(target, key, value) {
			Reflect.set(target, key, value);
			changed();
			return true;
		},
	});
}

beforeEach(() => {
	sent.length = 0;
	listeners.clear();
});

describe("ctx.controllers runtime", () => {
	test("layouts are per player and follow ctx.state", async () => {
		const fake = fakeBackend();
		const rt = yuiControllersRuntime(fake.backend);
		const state = reactive({ round: 1 }, rt.stateChanged);
		await rt.api.ready();
		expect(rt.api.players.map((p: any) => p.id)).toEqual(["p1", "p2"]);
		expect(rt.api.session).toEqual({ code: "KQXM" });
		expect(fake.layouts).toEqual([]); // nothing until a layout function exists

		rt.api.layout((player: any) =>
			rt.api.ui.column({ gap: 8 }, [
				rt.api.ui.text(`Round ${state.round}`),
				player.host ? rt.api.ui.button({ id: "next", label: "Next", onTap: () => {} }) : null,
				[{ type: "h1", props: {}, children: ["Hi ", player.name] }, false],
			]),
		);
		await tick();
		expect(fake.layouts).toHaveLength(1);
		const first = fake.layouts[0];
		expect(Object.keys(first).sort()).toEqual(["p1", "p2"]);
		// Functions are dropped, ctx.ui text nodes become pad text nodes.
		expect(first.p1.children[1]).toEqual({ type: "button", props: { id: "next", label: "Next" } });
		expect(first.p1.children[2]).toEqual({ type: "text", props: { text: "Hi Ada", size: "large" } });
		expect(first.p2.children).toHaveLength(2);

		// Several writes in one turn: one recompute.
		state.round = 2;
		state.round = 3;
		await tick();
		expect(fake.layouts).toHaveLength(2);
		expect(fake.layouts[1].p1.children[0].props.text).toBe("Round 3");

		// A write that changes nothing sends nothing.
		state.round = 3;
		await tick();
		expect(fake.layouts).toHaveLength(2);
	});

	test("join, leave and input events", async () => {
		const fake = fakeBackend();
		const rt = yuiControllersRuntime(fake.backend);
		const seen: string[] = [];
		rt.api.on("join", (p: any) => seen.push(`join:${p.id}`));
		rt.api.on("leave", (p: any) => seen.push(`leave:${p.id}`));
		rt.api.on("event", (e: any) => seen.push(`event:${e.player.name}:${e.control}:${e.kind}:${e.value}`));
		rt.api.on("system", (e: any) => seen.push(`system:${e.action}`));
		rt.api.on("input", (e: any) => seen.push(`input:${e.player.id}:${e.buttons}`));
		rt.api.layout((p: any) => ({ type: "text", props: { text: p.name } }));
		await rt.api.ready();
		expect(fake.calls).toContain("frames"); // "input" asks for whole frames

		rt.push("players", { session: { code: "KQXM" }, players: [p2, { id: "p3", name: "Cy" }] });
		rt.push("input", { app_id: "a", player_id: "p2", target: "answer", kind: "tap", value: "2" });
		rt.push("input", { player_id: "p2", target: "quit", kind: "system" });
		rt.push("input", { player_id: "p3", target: "state", kind: "state", value: { buttons: 5 } });
		await tick();
		expect(seen).toEqual([
			"join:p1",
			"join:p2",
			"join:p3",
			"leave:p1",
			"event:Bo:answer:tap:2",
			"system:quit",
			"input:p3:5",
		]);
		// Players changing re-runs the layout and re-renders the app.
		expect(Object.keys(fake.layouts.at(-1) ?? {}).sort()).toEqual(["p2", "p3"]);
		expect(fake.changes()).toBeGreaterThan(0);
		expect(() => rt.api.on("nope", () => {})).toThrow();
	});

	test("removing the layout function clears the phones' app layouts", async () => {
		const fake = fakeBackend();
		const rt = yuiControllersRuntime(fake.backend);
		const off = rt.api.layout(() => ({ type: "text", props: { text: "x" } }));
		await rt.api.ready();
		await tick();
		off();
		await tick();
		expect(fake.layouts.at(-1)).toEqual({});
	});

	test("the copy spliced into the app sandbox works the same", async () => {
		const rpcs: Array<[string, string, unknown[]]> = [];
		let messageHandler: ((event: { data: unknown }) => void) | undefined;
		const fakeWindow = {
			addEventListener: (type: string, fn: any) => {
				if (type === "message") messageHandler = fn;
			},
		};
		const rpc = async (api: string, method: string, args: unknown[] = []) => {
			rpcs.push([api, method, args]);
			if (api === "controllers" && method === "start") return { session: { code: "KQXM" }, players: [p1] };
			return null;
		};
		let renders = 0;
		const build = new Function(
			"rpc",
			"disposables",
			"window",
			"scheduleRender",
			`${sandboxApiSource}\nreturn { yuiExtendContext, yuiStateChanged };`,
		);
		const sandbox = build(rpc, new Set(), fakeWindow, () => (renders += 1));
		const ctx = sandbox.yuiExtendContext({});
		const state = reactive({ n: 1 }, sandbox.yuiStateChanged);
		ctx.controllers.layout((p: any) => ctx.controllers.ui.text(`${p.name} ${state.n}`));
		await ctx.controllers.ready();
		await tick();
		state.n = 2;
		await tick();
		const layouts = rpcs.filter(([api, method]) => api === "controllers" && method === "layouts").map(([, , args]) => args[0]);
		expect(layouts).toEqual([{ p1: { type: "text", props: { text: "Ada 1", size: "normal" } } }, { p1: { type: "text", props: { text: "Ada 2", size: "normal" } } }]);
		expect(renders).toBeGreaterThan(0);

		// Pushes from the host reach the sandbox's runtime.
		const taps: string[] = [];
		ctx.controllers.on("event", (e: any) => taps.push(e.control));
		messageHandler?.({ data: { yuiAppHost: true, type: "push", api: "controllers", event: "input", data: { player_id: "p1", target: "go", kind: "tap" } } });
		expect(taps).toEqual(["go"]);

		await ctx.virtualPads.attach(p1, { slot: 1 });
		await ctx.controllers.haptic("p1", 30);
		expect(rpcs).toContainEqual(["virtualPads", "attach", ["p1", { slot: 1 }]]);
		expect(rpcs).toContainEqual(["controllers", "haptic", ["p1", [30]]]);

		await ctx.controllers.haptic("p1", "win");
		await ctx.controllers.haptic("all", "hit", { intensity: 0.25 });
		await ctx.controllers.haptic(p1, [10, 20, 10]);
		expect(rpcs).toContainEqual(["controllers", "haptic", ["p1", "win"]]);
		expect(rpcs).toContainEqual(["controllers", "haptic", ["all", "hit", { intensity: 0.25 }]]);
		expect(rpcs).toContainEqual(["controllers", "haptic", ["p1", [10, 20, 10]]]);

		// Bad input never reaches the kiosk.
		const before = rpcs.length;
		await expect(ctx.controllers.haptic("p1", "zap" as any)).rejects.toThrow(/unknown haptic/);
		await expect(ctx.controllers.haptic("p1", [1001])).rejects.toThrow(/0-1000/);
		await expect(ctx.controllers.haptic("p1", Array(17).fill(1))).rejects.toThrow(/16 steps/);
		await expect(ctx.controllers.haptic("p1", [1000, 1000, 1000, 1000, 1])).rejects.toThrow(/4 s/);
		await expect(ctx.controllers.haptic("p1", "tap", { intensity: 2 })).rejects.toThrow(/intensity/);
		expect(rpcs.length).toBe(before);
	});
});

describe("controllers host", () => {
	const app = {
		schema: "yui.simple-js.v0" as const,
		id: "dev.example.quiz",
		name: "Quiz",
		version: "1",
		permissions: ["controllers", "virtual-pads"],
		mount() {},
	};
	const allow = async () => {};

	test("declaring controllers focuses the app; dispose blurs it", async () => {
		const host = createControllersHost(app, allow);
		expect(await host.declare({ keymap: { controls: [{ type: "trackpad", mode: "absolute", mouse: "drag" }] } })).toBe(true);
		const methods = sent.map((s) => `${s.method}:${s.params.active}`);
		// The one-time reset of a stale app layer lands before the focus.
		expect(methods.indexOf("pad.app.focus:false")).toBeLessThan(methods.indexOf("pad.app.focus:true"));
		const focus = sent.find((s) => s.method === "pad.app.focus" && s.params.active)!;
		expect(focus.params).toMatchObject({ app_id: "dev.example.quiz", name: "Quiz", frames: false });
		expect(focus.params.keymap.controls[0].mode).toBe("absolute");

		sent.length = 0;
		host.dispose();
		expect(sent).toEqual([{ method: "pad.app.focus", params: { app_id: "dev.example.quiz", active: false } }]);
	});

	test("an invalid manifest is rejected before anything is sent", async () => {
		const host = createControllersHost(app, allow);
		await expect(host.declare({ keymap: { controls: [{ type: "dpad", keys: { up: "F13" } }] } })).rejects.toThrow(/YUI_INVALID_APP/);
		expect(sent.filter((s) => s.method === "pad.app.focus" && s.params.active)).toEqual([]);
	});

	test("start hides the join URL and filters input to the app", async () => {
		const host = createControllersHost(app, allow);
		const pushed: Array<[string, any]> = [];
		host.addPush((event, data) => pushed.push([event, data]));
		const state: any = await host.call("start", []);
		expect(state.session).toEqual({ code: "KQXM" });
		expect(JSON.stringify(state)).not.toContain("SECRET");

		fire("pad.app.input", { app_id: "dev.other.app", player_id: "p1", kind: "tap" });
		fire("pad.app.input", { app_id: "dev.example.quiz", player_id: "p1", kind: "tap" });
		fire("pad.players", { session: { code: "ABCD", join_url: "https://x/?t=SECRET" }, players: [p1] });
		expect(pushed.map(([event]) => event)).toEqual(["input", "players"]);
		expect(JSON.stringify(pushed)).not.toContain("SECRET");

		await host.call("layouts", [{ p1: { type: "text" } }]);
		expect(sent.at(-1)).toEqual({ method: "pad.app.layout", params: { app_id: "dev.example.quiz", layouts: { p1: { type: "text" } } } });
		host.dispose();
	});

	test("haptic sends a name, a custom pattern or intensity to the bridge", async () => {
		const host = createControllersHost(app, allow);
		await host.call("haptic", ["p1", "success", { intensity: 0.5 }]);
		expect(sent.at(-1)).toEqual({ method: "pad.app.haptic", params: { app_id: "dev.example.quiz", player_id: "p1", name: "success", intensity: 0.5 } });
		await host.call("haptic", ["all", [40, 60, 40]]);
		expect(sent.at(-1)).toEqual({ method: "pad.app.haptic", params: { app_id: "dev.example.quiz", player_id: "all", pattern: [40, 60, 40] } });
	});

	test("sound to the speaker resolves the speaker phone", async () => {
		padPlayers = { session: null, players: [p1, { ...p2, speaker: true }] };
		const host = createControllersHost(app, allow);
		await host.call("sound", ["speaker", "https://yui.example/ding.mp3", 0.5]);
		expect(sent.at(-1)).toEqual({
			method: "pad.app.sound",
			params: { app_id: "dev.example.quiz", player_id: "p2", url: "https://yui.example/ding.mp3", volume: 0.5 },
		});
		await host.virtualPads("attach", ["p1", { slot: 2, pointerStick: true }]);
		expect(sent.at(-1)).toEqual({
			method: "pad.vpads.attach",
			params: { app_id: "dev.example.quiz", player_id: "p1", backend: "dsu", slot: 2, pointer_stick: true },
		});
	});

	test("permission is checked before any call", async () => {
		const host = createControllersHost(app, async () => {
			throw new Error("YUI_PERMISSION_DENIED: controllers permission is required");
		});
		await expect(host.call("start", [])).rejects.toThrow(/PERMISSION_DENIED/);
		await expect(host.call("haptic", ["p1", [20]])).rejects.toThrow(/PERMISSION_DENIED/);
		expect(sent.filter((s) => s.method.startsWith("pad.app."))).toEqual([]);
	});
});
