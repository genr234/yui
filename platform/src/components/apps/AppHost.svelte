<script lang="ts">
  import { onDestroy } from "svelte";
  import {
    createYuiContext,
    type YuiChildren,
    type YuiDevApp,
  } from "../../sdk/apps";
  import YuiNodeView from "./YuiNodeView.svelte";

  export let devApp: YuiDevApp;
  export let instanceKey = 0;

  type SandboxMessage =
    | { type: "ready" }
    | { type: "render"; node: YuiChildren }
    | { type: "error"; error: string }
    | {
        type: "call";
        id: string;
        api:
          | "storage"
          | "network"
          | "clipboard"
          | "notifications"
          | "fullscreen"
          | "open"
          | "toast"
          | "log";
        method: string;
        args: unknown[];
      };

  let iframe: HTMLIFrameElement | undefined;
  let sandboxElement: HTMLDivElement | undefined;
  let node: YuiChildren;
  let error = "";
  let loading = true;
  let mountedId = "";
  let ctx: ReturnType<typeof createYuiContext> | undefined;
  let sandboxReady = false;
  let mountSent = false;
  let readyTimer: number | undefined;
  let mountTimer: number | undefined;
  let loadAttempt = 0;

  const sandboxHtml = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; base-uri 'none'; form-action 'none'; connect-src 'none'; img-src 'none'; media-src 'none'; object-src 'none'; style-src 'none'; worker-src 'none'; child-src 'none'; frame-src 'none'; script-src 'unsafe-inline' blob:;">
</head>
<body>
<script type="module">
const appInfo = ${JSON.stringify({ runtimeVersion: "0.1.0" })};
let app;
let render;
let scheduled = false;
let handlerId = 0;
const handlers = new Map();
const disposables = new Set();
const eventHandlers = new Map();
const maxSerializedNodes = 5000;

Object.defineProperty(globalThis, "fetch", {
  configurable: false,
  writable: false,
  value() {
    return Promise.reject(new Error("YUI_NETWORK_ERROR: direct fetch is blocked; use ctx.network.fetch"));
  }
});

function blockedApi(name) {
  return function() {
    throw new Error("YUI_SANDBOX_ERROR: direct " + name + " access is blocked; use ctx");
  };
}

for (const name of ["XMLHttpRequest", "WebSocket", "EventSource", "Worker", "SharedWorker"]) {
  Object.defineProperty(globalThis, name, {
    configurable: false,
    writable: false,
    value: blockedApi(name)
  });
}

for (const name of ["localStorage", "sessionStorage", "indexedDB", "caches"]) {
  Object.defineProperty(globalThis, name, {
    configurable: false,
    get: blockedApi(name)
  });
}

try {
  Object.defineProperty(navigator, "serviceWorker", {
    configurable: false,
    get: blockedApi("serviceWorker")
  });
} catch {}

function post(message) {
  parent.postMessage({ yuiSimpleAppSandbox: true, ...message }, "*");
}

function rpc(api, method, args = []) {
  const id = crypto.randomUUID();
  parent.postMessage({ yuiSimpleAppSandbox: true, type: "call", id, api, method, args }, "*");
  return new Promise((resolve, reject) => {
    const onMessage = (event) => {
      const message = event.data;
      if (!message || message.yuiAppHost !== true || message.type !== "result" || message.id !== id) return;
      window.removeEventListener("message", onMessage);
      if (message.ok) resolve(message.value);
      else reject(new Error(message.error || "YUI_APP_RPC_ERROR"));
    };
    window.addEventListener("message", onMessage);
  });
}

function makeNode(type, props = {}, children = []) {
  return { type, props: props || {}, children: children === undefined ? [] : Array.isArray(children) ? children : [children] };
}

const textElements = new Set(["h1", "h2", "h3", "p", "small", "code", "pre", "text"]);
const childElements = new Set(["div", "span", "row", "column", "grid", "card", "panel", "list", "item"]);

function isProps(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value) && !("type" in value);
}

function element(type, ...args) {
  if (textElements.has(type)) return makeNode(type, {}, args.length > 1 ? args : args[0]);
  if (args.length === 0) return makeNode(type);
  if (isProps(args[0])) return makeNode(type, args[0], args[1]);
  return makeNode(type, {}, args[0]);
}

function button(...args) {
  if (typeof args[0] === "string") return makeNode("button", { label: args[0], onClick: args[1] });
  return makeNode("button", args[0] || {});
}

function createUiApi() {
  const ui = {
    text: (...args) => element("text", ...args),
    button,
    input: (props) => makeNode("input", props || {}),
    textarea: (props) => makeNode("textarea", props || {}),
    checkbox: (props) => makeNode("checkbox", props || {}),
    select: (props) => makeNode("select", props || {}),
    slider: (props) => makeNode("slider", props || {}),
    spacer: (props) => makeNode("spacer", props || {}),
    divider: () => makeNode("divider"),
    icon: (name, props) => makeNode("icon", { ...(props || {}), name }),
    image: (props) => makeNode("image", props || {}),
    embed: (props) => makeNode("embed", props || {}),
    empty: (message = "nothing here yet") => makeNode("empty", { message }),
    when: (condition, node) => condition ? node : null,
    for: (items, render) => Array.isArray(items) ? items.map(render) : [],
    css: (styles) => makeNode("css", { styles })
  };
  for (const type of [...textElements, ...childElements]) ui[type] ||= (...args) => element(type, ...args);
  return ui;
}

function scheduleRender() {
  if (scheduled || !render) return;
  scheduled = true;
  requestAnimationFrame(() => {
    scheduled = false;
    try {
      handlers.clear();
      post({ type: "render", node: serialize(render(), { nodes: 0 }) });
    } catch (error) {
      post({ type: "error", error: String(error?.message || error) });
    }
  });
}

function state(initial) {
  return new Proxy(initial, {
    set(target, key, value) {
      target[key] = value;
      scheduleRender();
      return true;
    },
    deleteProperty(target, key) {
      delete target[key];
      scheduleRender();
      return true;
    }
  });
}

function serialize(value, budget) {
  if (value === null || value === undefined || typeof value === "string" || typeof value === "number" || typeof value === "boolean") return value;
  budget.nodes += 1;
  if (budget.nodes > maxSerializedNodes) throw new Error("YUI_RENDER_ERROR: render tree is too large");
  if (typeof value === "function") {
    const id = "h" + ++handlerId;
    handlers.set(id, value);
    return { __yuiHandler: id };
  }
  if (Array.isArray(value)) return value.map((item) => serialize(item, budget));
  if (typeof value === "object") {
    const result = {};
    for (const [key, item] of Object.entries(value)) result[key] = serialize(item, budget);
    return result;
  }
  return null;
}

function responseFromValue(value) {
  return new Response(value?.body || "", { status: value?.status || 200, headers: value?.headers || {} });
}

function contextFor(app) {
  return {
    app: { id: app.id, name: app.name, version: app.version },
    env: { runtimeVersion: appInfo.runtimeVersion, platform: "web", mode: "dev", theme: "system" },
    ui: createUiApi(),
    storage: {
      get: (key) => rpc("storage", "get", [key]),
      set: (key, value) => rpc("storage", "set", [key, value]),
      delete: (key) => rpc("storage", "delete", [key]),
      keys: () => rpc("storage", "keys"),
      clear: () => rpc("storage", "clear")
    },
    commands: {
      register(command) {
        rpc("log", "info", ["command registration requested", command?.id]).catch(() => {});
        const dispose = () => {};
        disposables.add(dispose);
        return dispose;
      }
    },
    events: {
      on(event, handler) {
        const set = eventHandlers.get(event) || new Set();
        set.add(handler);
        eventHandlers.set(event, set);
        const dispose = () => set.delete(handler);
        disposables.add(dispose);
        return dispose;
      },
      async emit(event, data) {
        const set = eventHandlers.get(event);
        if (!set) return;
        for (const handler of set) await handler(data);
      }
    },
    clipboard: {
      readText: () => rpc("clipboard", "readText"),
      writeText: (text) => rpc("clipboard", "writeText", [text])
    },
    notifications: {
      send: (notification) => rpc("notifications", "send", [notification])
    },
    network: {
      fetch: async (url, options) => responseFromValue(await rpc("network", "fetch", [url, options]))
    },
    fullscreen: {
      enter: () => rpc("fullscreen", "enter"),
      exit: () => rpc("fullscreen", "exit"),
      toggle: () => rpc("fullscreen", "toggle"),
      isActive: () => false
    },
    log: {
      debug: (...args) => rpc("log", "debug", args).catch(() => {}),
      info: (...args) => rpc("log", "info", args).catch(() => {}),
      warn: (...args) => rpc("log", "warn", args).catch(() => {}),
      error: (...args) => rpc("log", "error", args).catch(() => {})
    },
    state,
    toast: (message, options) => rpc("toast", "show", [message, options]),
    open: (target, options) => rpc("open", "open", [target, options]),
    dispose() {
      for (const dispose of disposables) dispose();
      disposables.clear();
      eventHandlers.clear();
    }
  };
}

async function mount(source) {
  try {
    const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
    const mod = await import(url);
    URL.revokeObjectURL(url);
    app = mod.default;
    const mounted = await app.mount(contextFor(app));
    render = typeof mounted === "function" ? mounted : undefined;
    await app.activate?.(contextFor(app));
    if (render) scheduleRender();
    else post({ type: "render", node: undefined });
  } catch (error) {
    post({ type: "error", error: String(error?.message || error) });
  }
}

window.addEventListener("message", async (event) => {
  const message = event.data;
  if (!message || message.yuiAppHost !== true) return;
  if (message.type === "mount") {
    await mount(message.source);
  } else if (message.type === "event") {
    try {
      await handlers.get(message.handlerId)?.(...(message.args || []));
    } catch (error) {
      post({ type: "error", error: String(error?.message || error) });
    }
  } else if (message.type === "dispose") {
    try {
      const ctx = contextFor(app || {});
      await app?.suspend?.(ctx);
      await app?.unmount?.(ctx);
      ctx.dispose();
    } catch {}
  }
});

post({ type: "ready" });
<\/script>
</body>
</html>`;

  function clearLoadTimers() {
    window.clearTimeout(readyTimer);
    window.clearTimeout(mountTimer);
    readyTimer = undefined;
    mountTimer = undefined;
  }

  function armLoadTimers() {
    clearLoadTimers();
    readyTimer = window.setTimeout(() => {
      if (!loading || sandboxReady) return;
      retryMount();
    }, 350);
    mountTimer = window.setTimeout(() => {
      if (!loading) return;
      retryMount();
    }, 2000);
  }

  function retryMount() {
    if (!loading) return;
    loadAttempt += 1;
    if (loadAttempt > 8) {
      error = sandboxReady
        ? "app did not finish mounting"
        : "app sandbox did not start";
      loading = false;
      clearLoadTimers();
      return;
    }
    sandboxReady = false;
    mountSent = false;
    recreateSandbox();
    armLoadTimers();
  }

  async function mountApp() {
    loading = true;
    error = "";
    node = undefined;
    sandboxReady = false;
    mountSent = false;
    loadAttempt = 0;
    ctx?.dispose();
    ctx = createYuiContext(devApp.app, () => {});
    iframe?.contentWindow?.postMessage(
      { yuiAppHost: true, type: "dispose" },
      "*",
    );
    recreateSandbox();
    armLoadTimers();
  }

  function sendToSandbox(message: Record<string, unknown>) {
    iframe?.contentWindow?.postMessage({ yuiAppHost: true, ...message }, "*");
  }

  function sendMountToSandbox() {
    if (!loading || !sandboxReady || mountSent) return;
    mountSent = true;
    sendToSandbox({ type: "mount", source: devApp.source });
  }

  function recreateSandbox() {
    if (!sandboxElement) return;
    iframe?.remove();
    const frame = document.createElement("iframe");
    frame.className = "simple-app-sandbox";
    frame.setAttribute("sandbox", "allow-scripts");
    frame.srcdoc = sandboxHtml;
    iframe = frame;
    sandboxElement.append(frame);
  }

  function createSandbox(element: HTMLDivElement) {
    sandboxElement = element;
    if (loading && !iframe) {
      recreateSandbox();
    }
    return {
      destroy() {
        iframe?.remove();
        if (sandboxElement === element) sandboxElement = undefined;
      },
    };
  }

  async function serializeResponse(response: Response) {
    const headers: Record<string, string> = {};
    response.headers.forEach((value, key) => {
      headers[key] = value;
    });
    return {
      status: response.status,
      headers,
      body: await response.text(),
    };
  }

  async function handleCall(
    message: Extract<SandboxMessage, { type: "call" }>,
  ) {
    if (!ctx) throw new Error("app context is not ready");
    const [first, second] = message.args;

    switch (`${message.api}.${message.method}`) {
      case "storage.get":
        return ctx.storage.get(first as string);
      case "storage.set":
        return ctx.storage.set(first as string, second);
      case "storage.delete":
        return ctx.storage.delete(first as string);
      case "storage.keys":
        return ctx.storage.keys();
      case "storage.clear":
        return ctx.storage.clear();
      case "network.fetch":
        return serializeResponse(
          await ctx.network.fetch(
            first as string,
            second as RequestInit | undefined,
          ),
        );
      case "clipboard.readText":
        return ctx.clipboard.readText();
      case "clipboard.writeText":
        return ctx.clipboard.writeText(first as string);
      case "notifications.send":
        return ctx.notifications.send(
          first as { title: string; body?: string; icon?: string },
        );
      case "fullscreen.enter":
        return ctx.fullscreen.enter();
      case "fullscreen.exit":
        return ctx.fullscreen.exit();
      case "fullscreen.toggle":
        return ctx.fullscreen.toggle();
      case "open.open":
        return ctx.open(
          first as string,
          second as { where?: "shell" | "external" | "new-view" } | undefined,
        );
      case "toast.show":
        return ctx.toast(
          first as string,
          second as { kind?: string; durationMs?: number } | undefined,
        );
      default:
        if (message.api === "log") {
          console[message.method as "debug" | "info" | "warn" | "error"]?.(
            `[${devApp.id}]`,
            ...message.args,
          );
          return undefined;
        }
        throw new Error(
          `unsupported app api call: ${message.api}.${message.method}`,
        );
    }
  }

  async function handleSandboxMessage(
    event: MessageEvent<SandboxMessage & { yuiSimpleAppSandbox?: boolean }>,
  ) {
    if (
      event.source !== iframe?.contentWindow ||
      event.data?.yuiSimpleAppSandbox !== true
    )
      return;
    const message = event.data;
    if (message.type === "ready") {
      sandboxReady = true;
      sendMountToSandbox();
      return;
    }
    if (message.type === "render") {
      node = message.node;
      loading = false;
      clearLoadTimers();
      return;
    }
    if (message.type === "error") {
      error = message.error;
      loading = false;
      clearLoadTimers();
      return;
    }
    if (message.type === "call") {
      try {
        const value = await handleCall(message);
        sendToSandbox({ type: "result", id: message.id, ok: true, value });
      } catch (err) {
        sendToSandbox({
          type: "result",
          id: message.id,
          ok: false,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }
  }

  function handleAppEvent(handlerId: string, args: unknown[]) {
    sendToSandbox({ type: "event", handlerId, args });
  }

  $: if (devApp) {
    const nextMountedId = `${devApp.id}:${instanceKey}`;
    if (nextMountedId !== mountedId) {
      mountedId = nextMountedId;
      void mountApp();
    }
  }

  onDestroy(() => {
    clearLoadTimers();
    sendToSandbox({ type: "dispose" });
    ctx?.dispose();
    window.removeEventListener(
      "message",
      handleSandboxMessage as unknown as EventListener,
    );
  });
</script>

<svelte:window on:message={handleSandboxMessage} />

<section class="simple-app-host" aria-label={devApp.name}>
  <div use:createSandbox></div>
  {#if loading}
    <div class="yui-app-loading">loading {devApp.name}</div>
  {:else if error}
    <div class="yui-app-crash">
      <strong>this app crashed</strong>
      <span>app: {devApp.id}</span>
      <code>{error}</code>
      <button class="yui-app-button" on:click={mountApp}>reload app</button>
    </div>
  {:else if node !== undefined}
    <YuiNodeView {node} app={devApp.app} onAppEvent={handleAppEvent} />
  {:else}
    <div class="yui-app-empty">app mounted without a view</div>
  {/if}
</section>
