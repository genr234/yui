/**
 * ctx APIs that cross the app sandbox boundary generically: `ctx.native`,
 * `ctx.library`, and `ctx.launch`.
 *
 * Apps run in a sandboxed iframe (AppHost). `sandboxApiSource` is spliced
 * into the sandbox script and adds the APIs to the sandbox's ctx as RPC
 * stubs; `handleSandboxApiCall` runs those RPCs against the host ctx, which
 * checks permissions and attaches the app id. Events flow back as
 * `{ type: "push", api, event, data }` messages. New APIs can be added here
 * without touching AppHost again.
 */
import type { YuiContext, YuiNativeEvent } from "./types";

export const sandboxApis = ["native", "library"] as const;
export type SandboxApi = (typeof sandboxApis)[number];

/**
 * Runs inside the sandbox script, which defines `rpc` and `disposables`.
 * `yuiExtendContext(ctx)` adds the APIs to a ctx object and returns it.
 */
export const sandboxApiSource = String.raw`
let yuiLaunchItem = null;
const yuiPushHandlers = new Map();

window.addEventListener("message", (event) => {
  const message = event.data;
  if (!message || message.yuiAppHost !== true || message.type !== "push") return;
  if (message.api === "launch" && message.event === "item") {
    yuiLaunchItem = message.data ?? null;
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
    on: (event, handler) => yuiSubscribe("native", event, handler)
  };
  ctx.library = {
    publish: (titles, options) => rpc("library", "publish", [titles, options]),
    played: (id, options) => rpc("library", "played", [id, options]),
    list: () => rpc("library", "list"),
    remove: (ids) => rpc("library", "remove", [ids])
  };
  ctx.launch = { item: yuiLaunchItem };
  return ctx;
}
`;

/** Returned by handleSandboxApiCall for calls it does not own. */
export const notASandboxApi = Symbol("notASandboxApi");

type Push = (api: string, event: string, data: unknown) => void;

const nativeEvents = new Set<YuiNativeEvent>(["status", "started", "exited", "crashed"]);

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
		default:
			return notASandboxApi;
	}
}
