/**
 * ctx APIs that cross the app sandbox boundary generically: `ctx.native`,
 * `ctx.library`, `ctx.assets`, `ctx.launch`, `ctx.controllers`, `ctx.profiles`
 * and `ctx.virtualPads`.
 *
 * Apps run in a sandboxed iframe (AppHost). `sandboxApiSource` is spliced
 * into the sandbox script and adds the APIs to the sandbox's ctx as RPC
 * stubs; `handleSandboxApiCall` runs those RPCs against the host ctx, which
 * checks permissions and attaches the app id. Events flow back as
 * `{ type: "push", api, event, data }` messages. New APIs can be added here
 * without touching AppHost again.
 */
import type { YuiContext, YuiNativeEvent } from "./types";
import { controllersHostFor, type ControllersHost } from "./controllers";
import { yuiControllersRuntime } from "./controllers-runtime";
import { profileCalls, profilesHostFor } from "./profiles";
import { yuiProfilesRuntime } from "./profiles-runtime";

export const sandboxApis = ["native", "library", "assets", "controllers", "virtualPads", "profiles"] as const;
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
    onChange: (handler) => yuiSubscribe("assets", "changed", handler)
  });
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
