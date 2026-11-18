import { beforeEach, describe, expect, mock, test } from "bun:test";

type Sent = { method: string; params: any };
const sent: Sent[] = [];
const listeners = new Map<string, Set<(data: unknown) => void>>();
let reply: (method: string, params: any) => unknown = () => null;

mock.module("../bridge", () => ({
	bridge: {
		send: (method: string, params: any = {}) => {
			sent.push({ method, params });
			try {
				return Promise.resolve(reply(method, params));
			} catch (error) {
				return Promise.reject(error);
			}
		},
		on: (event: string, fn: (data: unknown) => void) => {
			let set = listeners.get(event);
			if (!set) listeners.set(event, (set = new Set()));
			set.add(fn);
			return () => set?.delete(fn);
		},
	},
}));

(globalThis as any).window ??= { addEventListener() {}, dispatchEvent() {} };

const { createSurfacesHost } = await import("./surfaces");
const { handleSandboxApiCall, sandboxApiSource } = await import("./sandbox-apis");
const { nativeSpecToWire } = await import("./context");
const { yuiControllersRuntime } = await import("./controllers-runtime");
const { validateSimpleApp } = await import("./validate");
const { describePermission } = await import("./permissions");

const app: any = { id: "dev.example.screens", name: "Screens", version: "1", permissions: ["surfaces"] };
const granted = async () => {};
const denied = async () => {
	throw new Error("YUI_PERMISSION_DENIED: surfaces permission is required");
};
const grantedSync = () => {};
const status = { id: "second-screen", state: "ready", player_id: null, width: 854, height: 480, fps: 60 };

function fire(event: string, data: unknown) {
	for (const fn of [...(listeners.get(event) ?? [])]) fn(data);
}

beforeEach(() => {
	sent.length = 0;
	listeners.clear();
	reply = () => null;
});

describe("ctx.surfaces host", () => {
	test("show checks the permission before touching the bridge", async () => {
		const host = createSurfacesHost(app, denied, () => {
			throw new Error("YUI_PERMISSION_DENIED: surfaces permission is required");
		});
		await expect(host.api.show("second-screen", "p1")).rejects.toThrow(/YUI_PERMISSION_DENIED/);
		await expect(host.api.available()).rejects.toThrow(/YUI_PERMISSION_DENIED/);
		await expect(host.api.list()).rejects.toThrow(/YUI_PERMISSION_DENIED/);
		expect(() => host.api.on("status", () => {})).toThrow(/YUI_PERMISSION_DENIED/);
		expect(sent).toEqual([]);
	});

	test("calls carry the app id and map players to ids", async () => {
		const host = createSurfacesHost(app, granted, grantedSync);
		reply = (method) => (method === "pad.surfaces.available" ? { available: false, reason: "no_encoder" } : method === "pad.surfaces.list" ? null : status);
		expect(await host.api.available()).toEqual({ available: false, reason: "no_encoder" });
		expect(await host.api.list()).toEqual([]);
		await host.api.show("second-screen", { id: "p1" } as any);
		await host.api.show("second-screen", "p2");
		await host.api.show("second-screen", null);
		expect(sent.map((s) => [s.method, s.params])).toEqual([
			["pad.surfaces.available", {}],
			["pad.surfaces.list", { app_id: app.id }],
			["pad.surfaces.show", { app_id: app.id, surface: "second-screen", player_id: "p1" }],
			["pad.surfaces.show", { app_id: app.id, surface: "second-screen", player_id: "p2" }],
			["pad.surfaces.show", { app_id: app.id, surface: "second-screen", player_id: null }],
		]);
	});

	test("controller errors pass through", async () => {
		const host = createSurfacesHost(app, granted, grantedSync);
		reply = () => {
			throw new Error("YUI_SURFACES: no_video_link");
		};
		await expect(host.api.show("second-screen", "p1")).rejects.toThrow("YUI_SURFACES: no_video_link");
		await expect(host.api.show("", "p1")).rejects.toThrow("YUI_SURFACES: unknown_surface");
	});

	test("status events are filtered by app id and dispose unsubscribes", () => {
		const host = createSurfacesHost(app, granted, grantedSync);
		const seen: unknown[] = [];
		host.api.on("status", (s) => seen.push(s));
		fire("pad.surfaces", { app_id: "someone.else", surfaces: [status] });
		fire("pad.surfaces", { app_id: app.id, surfaces: [status] });
		expect(seen).toEqual([[status]]);
		host.dispose();
		fire("pad.surfaces", { app_id: app.id, surfaces: [status] });
		expect(seen).toHaveLength(1);
		expect(listeners.get("pad.surfaces")?.size).toBe(0);
	});
});

describe("sandbox bridging", () => {
	test("the sandbox ctx gets surfaces through rpc", () => {
		expect(sandboxApiSource).toContain('rpc("surfaces", "show"');
		expect(sandboxApiSource).toContain('yuiSubscribe("surfaces", event, handler)');
	});

	test("rpc calls reach the host ctx and status is pushed", async () => {
		const calls: unknown[][] = [];
		let handler: ((s: unknown) => void) | undefined;
		const ctx: any = {
			surfaces: {
				available: async () => ({ available: true }),
				list: async () => [status],
				show: async (...args: unknown[]) => (calls.push(args), status),
				on: (_event: string, fn: (s: unknown) => void) => ((handler = fn), () => {}),
			},
		};
		const pushes: unknown[][] = [];
		const push = (...args: unknown[]) => pushes.push(args);
		expect(await handleSandboxApiCall(ctx, "surfaces", "available", [], push)).toEqual({ available: true });
		expect(await handleSandboxApiCall(ctx, "surfaces", "list", [], push)).toEqual([status]);
		await handleSandboxApiCall(ctx, "surfaces", "show", ["second-screen", undefined], push);
		await handleSandboxApiCall(ctx, "surfaces", "show", ["second-screen", "p1"], push);
		expect(calls).toEqual([["second-screen", null], ["second-screen", "p1"]]);
		await handleSandboxApiCall(ctx, "surfaces", "subscribe", ["status"], push);
		handler?.([status]);
		expect(pushes).toEqual([["surfaces", "status", [status]]]);
		await expect(handleSandboxApiCall(ctx, "surfaces", "subscribe", ["nope"], push)).rejects.toThrow(/YUI_SURFACES/);
	});
});

describe("native spec wire format", () => {
	test("carries install and surfaces", () => {
		const wire: any = nativeSpecToWire("a", {
			runtime: "r",
			executable: "runtime/p.exe",
			install: "private",
			surfaces: [
				{ id: "second-screen", window: { title: "(?i)second" }, width: 854, height: 480 },
				{ id: "map", window: { title: "map" }, hidden: false },
			],
		});
		expect(wire.install).toBe("private");
		expect(wire.surfaces).toEqual([
			{ id: "second-screen", title: "(?i)second", hidden: true, width: 854, height: 480 },
			{ id: "map", title: "map", hidden: false, width: undefined, height: undefined },
		]);
	});

	test("a plain spec has neither field", () => {
		const wire = nativeSpecToWire("a", { runtime: "r", executable: "p.exe" });
		expect("install" in wire).toBe(false);
		expect("surfaces" in wire).toBe(false);
	});
});

describe("player caps and the surface builder", () => {
	test("players carry caps and relayed only when known", async () => {
		const rt = yuiControllersRuntime({
			call: async (method) =>
				method === "start"
					? {
							session: { code: "KQXM" },
							players: [
								{ id: "p1", connected: true, caps: { motion: true, haptics: false, video: true }, relayed: false },
								{ id: "p2", connected: true },
							],
						}
					: undefined,
			changed() {},
		});
		await rt.api.ready();
		const [p1, p2] = rt.api.players;
		expect(p1.caps).toEqual({ motion: true, haptics: false, video: true });
		expect(p1.relayed).toBe(false);
		expect("caps" in p2).toBe(false);
		expect("relayed" in p2).toBe(false);
		expect(rt.api.ui.surface({ id: "screen", surface: "second-screen", touch: true, flex: 3 })).toEqual({
			type: "surface",
			props: { id: "screen", surface: "second-screen", touch: true, flex: 3 },
		});
	});
});

describe("permission", () => {
	test("the manifest validator accepts it and it has plain text", () => {
		expect(() => validateSimpleApp({ schema: "yui.simple-js.v0", id: "dev.example.screens", name: "S", version: "1", permissions: ["surfaces"], mount() {} })).not.toThrow();
		expect(describePermission("surfaces").description).toBe("Show a private screen on one player's phone.");
	});
});
