<script lang="ts">
  /**
   * `ctx.ui.embed({ web: "index.html", channel?, input?, fill? })`: the app's
   * own page from its web bundle (contracts §6). The page talks to the app
   * over the embed channel; no website permission is involved because the
   * page is the app's own.
   */
  import { onDestroy, onMount } from "svelte";
  import type { YuiSimpleApp } from "../../sdk/apps/types";
  import { webHostFor } from "../../sdk/apps/web";

  export let app: YuiSimpleApp | undefined;
  export let path = "index.html";
  export let channel = "";
  export let input = false;
  export let fill = false;
  export let title = "";
  export let style = "";

  let frame: HTMLIFrameElement | undefined;
  let src = "";
  let error = "";
  let detach: (() => void) | undefined;

  function cleanPath(value: string) {
    const trimmed = value.replace(/^\.?\/+/, "");
    if (/^[a-z][a-z0-9+.-]*:/i.test(trimmed) || trimmed.startsWith("//")) throw new Error("web must be a path inside the app's bundle");
    return trimmed;
  }

  onMount(async () => {
    const host = app ? webHostFor(app.id) : undefined;
    if (!host) {
      error = "This app has no web page host.";
      return;
    }
    try {
      const origin = await host.origin();
      src = `${origin}/${cleanPath(path)}`;
      // The iframe exists once src is set; its WindowProxy stays the same
      // across navigations, so attach it right away to catch "ready".
      await Promise.resolve();
      if (frame?.contentWindow && (channel || input)) {
        detach = host.attach(channel || "__input", frame.contentWindow, new URL(src).origin, { input });
      }
    } catch (err) {
      error = err instanceof Error ? err.message.replace(/^YUI_[A-Z_]+:\s*/, "") : String(err);
    }
  });

  onDestroy(() => detach?.());

  function focusFrame() {
    // Keys from the page and the phone keyboard go to the game.
    frame?.focus();
  }
</script>

{#if error}
  <div class="yui-app-embed-blocked">
    <strong>Couldn't open this app's page</strong>
    <span>{error}</span>
  </div>
{:else}
  <iframe
    bind:this={frame}
    class="yui-app-embed yui-app-web-embed"
    class:fill
    src={src || "about:blank"}
    title={title || app?.name || "App page"}
    allow="autoplay; fullscreen; gamepad"
    allowfullscreen
    {style}
    on:load={focusFrame}
  ></iframe>
{/if}
