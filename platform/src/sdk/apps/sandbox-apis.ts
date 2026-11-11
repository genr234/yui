/**
 * ctx APIs that cross the app sandbox boundary generically: `ctx.native`,
 * `ctx.library`, `ctx.assets`, `ctx.launch`, `ctx.controllers`, `ctx.profiles`,
 * `ctx.virtualPads` and `ctx.wakeLock`.
 *
 * Apps run in a sandboxed iframe (AppHost). `sandboxApiSource` is spliced
 * into the sandbox script and adds the APIs to the sandbox's ctx as RPC
 * stubs; `handleSandboxApiCall` runs those RPCs against the host ctx, which
 * checks permissions and attaches the app id. Events flow back as
 * `{ type: "push", api, event, data }` messages. New APIs can be added here
 * without touching AppHost again.
 */
import type { YuiContext, YuiNativeEvent, YuiWakeLock } from "./types";
import { controllersHostFor, type ControllersHost } from "./controllers";
import { yuiControllersRuntime } from "./controllers-runtime";
import { profileCalls, profilesHostFor } from "./profiles";
import { yuiProfilesRuntime } from "./profiles-runtime";

export const sandboxApis = ["native", "library", "assets", "controllers", "virtualPads", "profiles", "web", "embeds", "saves", "wakeLock"] as const;
export type SandboxApi = (typeof sandboxApis)[number];

/**
 * Runs inside the sandbox script, which defines `rpc`, `disposables` and
 * `scheduleRender`. `yuiExtendContext(ctx)` adds the APIs to a ctx object
 * and returns it; the sandbox's ctx.state calls `yuiStateChanged()` so
 * controller layouts follow state like the app's view does.
 */
export const sandboxApiSource =
	"const yuiControllersRuntime = (" +
	yuiControllersRuntime.toString() +
	");\nconst yuiProfilesRuntime = (" +
	yuiProfilesRuntime.toString() +
	");\n" +
	String.raw`
let yuiLaunchItem = null;
const yuiPushHandlers = new Map();
let yuiControllers = null;
let yuiProfiles = null;

function yuiProfilesApi() {
  if (!yuiProfiles) {
    yuiProfiles = yuiProfilesRuntime({
      call: (method, args) => rpc("profiles", method, args || []),
      players: () => yuiControllersApi().players
    });
  }
  return yuiProfiles.api;
}

function yuiStateChanged() {
  if (yuiControllers) yuiControllers.stateChanged();
}

function yuiControllersApi() {
  if (!yuiControllers) {
    yuiControllers = yuiControllersRuntime({
      call: (method, args) => rpc("controllers", method, args || []),
      changed: () => { if (typeof scheduleRender === "function") scheduleRender(); }
    });
    disposables.add(() => { yuiControllers && yuiControllers.dispose(); yuiControllers = null; });
  }
  return yuiControllers.api;
}

function yuiPlayerId(player) {
  return typeof player === "string" ? player : player && player.id;
}

window.addEventListener("message", (event) => {
  const message = event.data;
  if (!message || message.yuiAppHost !== true || message.type !== "push") return;
  if (message.api === "launch" && message.event === "item") {
    yuiLaunchItem = message.data ?? null;
    return;
  }
  if (message.api === "controllers") {
    if (yuiControllers) yuiControllers.push(message.event, message.data);
    return;
  }
  const set = yuiPushHandlers.get(message.api + ":" + message.event);
  if (!set) return;
  for (const handler of [...set]) {
    try { handler(message.data); } catch (error) { console.error(error); }
  }
});

function yuiSubscribe(api, event, handler) {
  const key = api + ":" + event;
  let set = yuiPushHandlers.get(key);
  if (!set) {
    set = new Set();
    yuiPushHandlers.set(key, set);
    rpc(api, "subscribe", [event]).catch(() => {});
  }
  set.add(handler);
  const dispose = () => set.delete(handler);
  disposables.add(dispose);
  return dispose;
}

function yuiExtendContext(ctx) {
  ctx.native = {
    launch: (spec) => rpc("native", "launch", [spec]),
    stop: (options) => rpc("native", "stop", [options]),
    status: () => rpc("native", "status"),
    backupSaves: () => rpc("native", "backupSaves"),
    on: (event, handler) => yuiSubscribe("native", event, handler)
  };
  ctx.library = {
    publish: (titles, options) => rpc("library", "publish", [titles, options]),
    played: (id, options) => rpc("library", "played", [id, options]),
    list: () => rpc("library", "list"),
    remove: (ids) => rpc("library", "remove", [ids])
  };
  ctx.assets = Object.assign((kind) => rpc("assets", "list", [kind]), {
    url: (id) => rpc("assets", "url", [id]),
    request: (id) => rpc("assets", "request", [id]),
    onChange: (handler) => yuiSubscribe("assets", "changed", handler)
  });
  ctx.web = {
    origin: () => rpc("web", "origin"),
    runtime: (name) => rpc("web", "runtime", [name])
  };
  ctx.embeds = {
    post: (channel, data) => { rpc("embeds", "post", [channel, data]).catch((error) => console.error(error)); },
    on: (channel, handler) => yuiSubscribe("embeds", "message:" + channel, handler)
  };
  const yuiSavePlayer = (options) => options && options.player !== undefined ? { player: yuiPlayerId(options.player) } : undefined;
  ctx.saves = {
    put: (key, data, options) => rpc("saves", "put", [key, data, yuiSavePlayer(options)]),
    get: (key, options) => rpc("saves", "get", [key, yuiSavePlayer(options)]),
    list: (options) => rpc("saves", "list", [yuiSavePlayer(options)]),
    delete: (key, options) => rpc("saves", "delete", [key, yuiSavePlayer(options)])
  };
  Object.defineProperty(ctx, "controllers", { get: yuiControllersApi, enumerable: true, configurable: true });
  // ctx.profiles exists only when the manifest declares the permission; the
  // host checks it again on every call.
  if (typeof app !== "undefined" && app && Array.isArray(app.permissions) && app.permissions.includes("profiles")) {
    Object.defineProperty(ctx, "profiles", { get: yuiProfilesApi, enumerable: true, configurable: true });
  }
  ctx.virtualPads = {
    attach: (player, options) => rpc("virtualPads", "attach", [yuiPlayerId(player), options]),
    detach: (player) => rpc("virtualPads", "detach", [yuiPlayerId(player)]),
    list: () => rpc("virtualPads", "list"),
    systemPad: () => rpc("virtualPads", "systemPad"),
    press: (button, action) => rpc("virtualPads", "press", [button, action || "tap"])
  };
  ctx.wakeLock = {
    request: async (reason) => {
      const held = await rpc("wakeLock", "request", [reason == null ? "" : String(reason)]);
      let released = false;
      const release = async () => {
        if (released) return;
        released = true;
        await rpc("wakeLock", "release", [held.id]).catch(() => {});
      };
      disposables.add(() => { release(); });
      return {
        reason: held.reason,
        get released() { return released; },
        release
      };
    }
  };
  ctx.launch = { item: yuiLaunchItem };
  return ctx;
}
`;

/** Returned by handleSandboxApiCall for calls it does not own. */
export const notASandboxApi = Symbol("notASandboxApi");

type Push = (api: string, event: string, data: unknown) => void;

const nativeEvents = new Set<YuiNativeEvent>(["status", "started", "exited", "crashed"]);
const controllerCalls = new Set(["layouts", "frames", "haptic", "sound"]);
const virtualPadCalls = new Set(["attach", "detach", "list", "systemPad", "press"]);
/** Wake locks a sandbox holds, by host ctx and token (the sandbox only sees the token). */
const sandboxWakeLocks = new WeakMap<YuiContext, Map<string, YuiWakeLock>>();
let wakeLockTokens = 0;
/** One sandbox per host ctx receives controller pushes; a reloaded sandbox replaces it. */
const sandboxPushes = new WeakMap<ControllersHost, () => void>();

function hostOf(ctx: YuiContext) {
	const host = controllersHostFor(ctx);
	if (!host) throw new Error("YUI_CONTROLLERS: not available");
	return host;
}

export async function handleSandboxApiCall(
	ctx: YuiContext,
	api: string,
	method: string,
	args: unknown[],
	push: Push,
): Promise<unknown> {
	const [first, second] = args as [any, any];
	switch (`${api}.${method}`) {
		case "native.launch":
			return ctx.native.launch(first);
		case "native.stop":
			return ctx.native.stop(first ?? undefined);
		case "native.status":
			return ctx.native.status();
		case "native.backupSaves":
			return ctx.native.backupSaves();
		case "native.subscribe": {
			if (!nativeEvents.has(first)) throw new Error(`unknown native event: ${String(first)}`);
			const event = first as YuiNativeEvent;
			ctx.native.on(event, (status) => push("native", event, status));
			return undefined;
		}
		case "library.publish":
			return ctx.library.publish(first, second ?? undefined);
		case "library.played":
			return ctx.library.played(first, second ?? undefined);
		case "library.list":
			return ctx.library.list();
		case "library.remove":
			return ctx.library.remove(first);
		case "assets.list":
			return ctx.assets(first);
		case "assets.subscribe": {
			if (first !== "changed") throw new Error(`unknown assets event: ${String(first)}`);
			ctx.assets.onChange(() => push("assets", "changed", null));
			return undefined;
		}
		case "assets.request":
			return ctx.assets.request(first);
		case "assets.url":
			return ctx.assets.url(first);
		case "web.origin":
			return ctx.web.origin();
		case "web.runtime":
			return ctx.web.runtime(first);
		case "embeds.post":
			ctx.embeds.post(first, second);
			return undefined;
		case "embeds.subscribe": {
			if (typeof first !== "string" || !first.startsWith("message:")) throw new Error(`unknown embeds event: ${String(first)}`);
			const channel = first.slice("message:".length);
			ctx.embeds.on(channel, (data) => push("embeds", first, data));
			return undefined;
		}
		case "wakeLock.request": {
			// The host ctx owns the lock, so it is released when the app
			// closes even if the sandbox never says so.
			const lock = await ctx.wakeLock.request(typeof first === "string" ? first : "");
			let held = sandboxWakeLocks.get(ctx);
			if (!held) {
				held = new Map();
				sandboxWakeLocks.set(ctx, held);
			}
			const id = `wl${++wakeLockTokens}`;
			held.set(id, lock);
			return { id, reason: lock.reason };
		}
		case "wakeLock.release": {
			const held = sandboxWakeLocks.get(ctx);
			const lock = typeof first === "string" ? held?.get(first) : undefined;
			if (lock) {
				held?.delete(first);
				await lock.release();
			}
			return undefined;
		}
		case "saves.put":
			return ctx.saves.put(first, second, args[2] ?? undefined);
		case "saves.get":
			return ctx.saves.get(first, second ?? undefined);
		case "saves.list":
			return ctx.saves.list(first ?? undefined);
		case "saves.delete":
			return ctx.saves.delete(first, second ?? undefined);
		case "controllers.start": {
			const host = hostOf(ctx);
			const state = await host.call("start", []);
			sandboxPushes.get(host)?.();
			sandboxPushes.set(
				host,
				host.addPush((event, data) => push("controllers", event, data)),
			);
			return state;
		}
		default:
			if (api === "controllers" && controllerCalls.has(method)) return hostOf(ctx).call(method, args);
			if (api === "virtualPads" && virtualPadCalls.has(method)) return hostOf(ctx).virtualPads(method, args);
			if (api === "profiles" && profileCalls.has(method)) {
				const profiles = profilesHostFor(ctx);
				if (!profiles) throw new Error("YUI_PERMISSION_DENIED: profiles permission is required");
				return profiles.call(method, args);
			}
			return notASandboxApi;
	}
}
