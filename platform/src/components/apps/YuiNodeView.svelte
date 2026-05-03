<script lang="ts">
  import {
    hasPermissionDecision,
    isPermissionDeclared,
    isPermissionGranted,
    requestAppPermission,
    setAppPermission,
  } from "../../sdk/apps/permissions";
  import {
    rememberEmbedStorage,
  } from "../../sdk/apps/embed-storage";
  import type {
    YuiChildren,
    YuiNode,
    YuiSimpleApp,
  } from "../../sdk/apps/types";

  export let node: YuiChildren;
  export let app: YuiSimpleApp | undefined = undefined;
  export let onAppEvent: (handlerId: string, args: unknown[]) => void = () => {};
  let embedUrl = "";
  let embedPermission = "";
  let embedGranted = false;
  let embedError = "";
  let embedLoadedSource = "";
  let embedFrameKey = 0;

  function children(value: YuiNode) {
    return normalize(value.children ?? []);
  }

  function normalize(value: YuiChildren): YuiChildren[] {
    if (value === null || value === undefined || value === false) return [];
    if (Array.isArray(value)) return value.flatMap((item) => normalize(item));
    return [value];
  }

  function prop<T>(name: string, fallback?: T): T | undefined {
    if (!node || typeof node !== "object" || Array.isArray(node))
      return fallback;
    return ((node as YuiNode).props?.[name] as T | undefined) ?? fallback;
  }

  function layoutStyle(value: YuiNode) {
    const props = value.props ?? {};
    const style: string[] = [];
    for (const key of ["gap", "padding", "margin"] as const) {
      if (props[key] !== undefined)
        style.push(
          `${key}: ${typeof props[key] === "number" ? `${props[key]}px` : props[key]}`,
        );
    }
    if (props.width !== undefined)
      style.push(
        `width: ${typeof props.width === "number" ? `${props.width}px` : props.width}`,
      );
    if (props.height !== undefined)
      style.push(
        `height: ${typeof props.height === "number" ? `${props.height}px` : props.height}`,
      );
    if (props.align !== undefined)
      style.push(
        `align-items: ${props.align === "start" ? "flex-start" : props.align === "end" ? "flex-end" : props.align}`,
      );
    if (props.justify !== undefined) {
      const value =
        props.justify === "between"
          ? "space-between"
          : props.justify === "around"
            ? "space-around"
            : props.justify === "start"
              ? "flex-start"
              : props.justify === "end"
                ? "flex-end"
                : props.justify;
      style.push(`justify-content: ${value}`);
    }
    if (props.wrap) style.push("flex-wrap: wrap");
    if (props.grow) style.push("flex: 1 1 auto");
    return style.join("; ");
  }

  function textContent(value: YuiNode) {
    return normalize(value.children ?? [])
      .map((item) => String(item ?? ""))
      .join("");
  }

  function callProp(name: string, ...args: unknown[]) {
    const handler = prop<unknown>(name);
    if (typeof handler === "function") {
      handler(...args);
      return;
    }
    if (
      handler &&
      typeof handler === "object" &&
      typeof (handler as { __yuiHandler?: unknown }).__yuiHandler === "string"
    ) {
      onAppEvent((handler as { __yuiHandler: string }).__yuiHandler, args);
    }
  }

  function handleInput(event: Event) {
    callProp(
      "onInput",
      (event.currentTarget as HTMLInputElement | HTMLTextAreaElement).value,
    );
  }

  function handleChange(event: Event) {
    callProp(
      "onChange",
      (event.currentTarget as HTMLInputElement | HTMLSelectElement).value,
    );
  }

  function handleCheckbox(event: Event) {
    callProp(
      "onChange",
      (event.currentTarget as HTMLInputElement).checked,
    );
  }

  function handleNumberInput(event: Event) {
    callProp(
      "onInput",
      Number((event.currentTarget as HTMLInputElement).value),
    );
  }

  function handleKeyDown(event: KeyboardEvent) {
    callProp("onKeyDown", {
      key: event.key,
      code: event.code,
      altKey: event.altKey,
      ctrlKey: event.ctrlKey,
      shiftKey: event.shiftKey,
      metaKey: event.metaKey,
    });
  }

  function selectOptions() {
    return prop<Array<{ label: string; value: string }>>("options", []) ?? [];
  }

  function autofocusAction(element: HTMLInputElement, enabled: boolean) {
    if (enabled) queueMicrotask(() => element.focus());
    return {};
  }

  function embedPermissionFor(value: string) {
    try {
      const url = new URL(value, window.location.href);
      if (url.protocol !== "https:" && url.protocol !== "http:") {
        return {
          url: "",
          permission: "",
          error: "embeds only support http and https urls",
        };
      }
      if (url.origin === window.location.origin) {
        return {
          url: "",
          permission: "",
          error: "apps cannot embed the yui shell origin",
        };
      }
      return { url: url.href, permission: `embed:${url.origin}`, error: "" };
    } catch {
      return { url: "", permission: "", error: "invalid embed url" };
    }
  }

  function embedReferrerPolicy() {
    return stringProp("referrerPolicy", "strict-origin-when-cross-origin");
  }

  function hasGrantedAppPermission(permission: string) {
    return Boolean(
      app &&
        isPermissionDeclared(app, permission) &&
        isPermissionGranted(app.id, permission),
    );
  }

  function iframeSandboxPolicy() {
    const requested = new Set(
      stringProp("sandbox", "allow-scripts allow-same-origin")
        .split(/\s+/)
        .map((token) => token.trim())
        .filter(Boolean),
    );
    const allowed = new Set(["allow-scripts", "allow-same-origin", "allow-forms"]);
    return [...requested].filter((token) => allowed.has(token)).join(" ");
  }

  function embedAllowPolicy() {
    const requested = stringProp(
      "allow",
      "autoplay; encrypted-media; picture-in-picture",
    );
    const allowed = new Set([
      "autoplay",
      "encrypted-media",
      "picture-in-picture",
      "accelerometer",
      "gyroscope",
    ]);

    if (hasGrantedAppPermission("clipboard.write")) {
      allowed.add("clipboard-write");
    }
    if (hasGrantedAppPermission("fullscreen")) {
      allowed.add("fullscreen");
    }
    return requested
      .split(";")
      .map((token) => token.trim())
      .filter(Boolean)
      .filter((token) => allowed.has(token.split(/\s+/)[0]))
      .join("; ");
  }

  function embedAllowsFullscreen() {
    return hasGrantedAppPermission("fullscreen");
  }

  function embedOriginLabel() {
    try {
      return new URL(embedUrl).origin;
    } catch {
      return embedPermission || "embedded website";
    }
  }

  function embedOrigin() {
    try {
      return new URL(embedUrl).origin;
    } catch {
      return "";
    }
  }

  async function loadEmbed(value: string) {
    embedLoadedSource = value;
    const next = embedPermissionFor(value);
    embedUrl = next.url;
    embedPermission = next.permission;
    embedError = next.error;
    embedGranted = false;
    embedFrameKey += 1;

    if (embedError || !app) return;
    if (!isPermissionDeclared(app, embedPermission)) {
      embedError = `missing permission: ${embedPermission}`;
      return;
    }
    if (isPermissionGranted(app.id, embedPermission)) {
      embedGranted = true;
      void rememberEmbedStorage(app.id, embedOrigin());
      return;
    }
    if (hasPermissionDecision(app.id, embedPermission)) {
      embedError = "website permission denied";
      return;
    }

    embedGranted = await requestAppPermission(app, embedPermission);
    if (embedGranted) void rememberEmbedStorage(app.id, embedOrigin());
    if (!embedGranted) embedError = "website permission denied";
  }

  function allowEmbed() {
    if (!app || !embedPermission || !isPermissionDeclared(app, embedPermission))
      return;
    setAppPermission(app.id, embedPermission, true);
    embedError = "";
    embedGranted = true;
    void rememberEmbedStorage(app.id, embedOrigin());
  }

  function resetEmbed() {
    embedFrameKey += 1;
  }

  function handleEmbedStorageCleared(event: CustomEvent<{ appId: string; origin?: string }>) {
    if (!app || event.detail.appId !== app.id) return;
    if (event.detail.origin && event.detail.origin !== embedOrigin()) return;
    resetEmbed();
  }

  const stringProp = (name: string, fallback = "") =>
    prop<string>(name, fallback) ?? fallback;
  const numberProp = (name: string, fallback = 0) =>
    prop<number>(name, fallback) ?? fallback;
  const booleanProp = (name: string, fallback = false) =>
    prop<boolean>(name, fallback) ?? fallback;
  const cssSizeProp = (name: string, fallback = "auto") => {
    const value = prop<string | number>(name);
    return typeof value === "number" ? `${value}px` : value || fallback;
  };

  $: if (
    node &&
    typeof node === "object" &&
    !Array.isArray(node) &&
    node.type === "embed"
  ) {
    const source = stringProp("url") || stringProp("src");
    if (source !== embedLoadedSource) {
      void loadEmbed(source);
    }
  }
</script>

<svelte:window on:yui:embed-storage-cleared={handleEmbedStorageCleared} />

{#if node === null || node === undefined || node === false}
  <span hidden></span>
{:else if typeof node === "string" || typeof node === "number" || typeof node === "boolean"}
  {node}
{:else if Array.isArray(node)}
  {#each normalize(node) as child}
    <svelte:self node={child} {app} {onAppEvent} />
  {/each}
{:else if node.type === "h1"}
  <h1 class="yui-app-h1">{textContent(node)}</h1>
{:else if node.type === "h2"}
  <h2 class="yui-app-h2">{textContent(node)}</h2>
{:else if node.type === "h3"}
  <h3 class="yui-app-h3">{textContent(node)}</h3>
{:else if node.type === "p"}
  <p>{textContent(node)}</p>
{:else if node.type === "small"}
  <small>{textContent(node)}</small>
{:else if node.type === "code"}
  <code>{textContent(node)}</code>
{:else if node.type === "pre"}
  <pre>{textContent(node)}</pre>
{:else if node.type === "text"}
  <span>{textContent(node)}</span>
{:else if node.type === "row"}
  <div class="yui-app-row {stringProp('class')}" style={layoutStyle(node)}>
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </div>
{:else if node.type === "column"}
  <div class="yui-app-column {stringProp('class')}" style={layoutStyle(node)}>
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </div>
{:else if node.type === "grid"}
  <div class="yui-app-grid {stringProp('class')}" style={layoutStyle(node)}>
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </div>
{:else if node.type === "card" || node.type === "panel"}
  <section class="yui-app-card {stringProp('class')}" style={layoutStyle(node)}>
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </section>
{:else if node.type === "div"}
  <div class={stringProp("class")} style={layoutStyle(node)}>
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </div>
{:else if node.type === "span"}
  <span class={stringProp("class")}>
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </span>
{:else if node.type === "button"}
  <button
    class="yui-app-button {stringProp('variant', 'default')}"
    disabled={booleanProp("disabled")}
    on:click={() => callProp("onClick")}
  >
    {stringProp("label")}
  </button>
{:else if node.type === "input"}
  <input
    class="yui-app-input"
    value={stringProp("value")}
    placeholder={stringProp("placeholder")}
    disabled={booleanProp("disabled")}
    use:autofocusAction={booleanProp("autofocus")}
    on:input={handleInput}
    on:change={handleChange}
    on:keydown={handleKeyDown}
  />
{:else if node.type === "textarea"}
  <textarea
    class="yui-app-input"
    value={stringProp("value")}
    placeholder={stringProp("placeholder")}
    rows={numberProp("rows", 4)}
    disabled={booleanProp("disabled")}
    style={`resize: ${booleanProp("resize", true) ? "vertical" : "none"}`}
    on:input={handleInput}
  />
{:else if node.type === "checkbox"}
  <label class="yui-app-check">
    <input
      type="checkbox"
      checked={booleanProp("checked")}
      disabled={booleanProp("disabled")}
      on:change={handleCheckbox}
    />
    {#if stringProp("label")}
      <span>{stringProp("label")}</span>
    {/if}
  </label>
{:else if node.type === "select"}
  <select
    class="yui-app-input"
    value={stringProp("value")}
    on:change={handleChange}
  >
    {#each selectOptions() as option}
      <option value={option.value}>{option.label}</option>
    {/each}
  </select>
{:else if node.type === "slider"}
  <input
    class="yui-app-input"
    type="range"
    value={numberProp("value")}
    min={numberProp("min")}
    max={numberProp("max", 100)}
    on:input={handleNumberInput}
  />
{:else if node.type === "list"}
  <ul class="yui-app-list">
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </ul>
{:else if node.type === "item"}
  <li>
    {#each children(node) as child}
      <svelte:self node={child} {app} {onAppEvent} />
    {/each}
  </li>
{:else if node.type === "empty"}
  <p class="yui-app-empty">{stringProp("message", "nothing here yet")}</p>
{:else if node.type === "spacer"}
  <div style={`height: ${numberProp("size", 12)}px`}></div>
{:else if node.type === "divider"}
  <hr class="yui-app-divider" />
{:else if node.type === "image"}
  <img
    class="yui-app-image"
    src={stringProp("src")}
    alt={stringProp("alt")}
    style={`width: ${cssSizeProp("width", "auto")}; height: ${cssSizeProp("height", "auto")};`}
  />
{:else if node.type === "embed"}
  {#if embedGranted}
    <div class="yui-app-embed-shell">
      <div class="yui-app-embed-toolbar">
        <span>{embedOriginLabel()}</span>
        <button class="yui-app-embed-reset" type="button" on:click={resetEmbed}
          >Reset</button
        >
      </div>
      {#key embedFrameKey}
        <iframe
          class="yui-app-embed"
          src={embedUrl}
          title={stringProp("title", embedUrl)}
          sandbox={iframeSandboxPolicy()}
          referrerpolicy={embedReferrerPolicy()}
          allow={embedAllowPolicy()}
          allowfullscreen={embedAllowsFullscreen()}
          credentialless
          style={`height: ${numberProp("height", 420)}px`}
        ></iframe>
      {/key}
    </div>
  {:else}
    <div class="yui-app-embed-blocked">
      <strong>Website blocked</strong>
      <span>{embedError || `waiting for permission: ${embedPermission}`}</span>
      {#if app && embedPermission && isPermissionDeclared(app, embedPermission)}
        <button class="yui-app-button primary" on:click={allowEmbed}
          >Allow website</button
        >
      {/if}
    </div>
  {/if}
{:else if node.type !== "css"}
  <div class="yui-app-unknown">unsupported node: {node.type}</div>
{/if}
