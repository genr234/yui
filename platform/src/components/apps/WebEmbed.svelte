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
  import { fitAspect, parseAspect, reportWebRect } from "../../sdk/game-rect";

  export let app: YuiSimpleApp | undefined;
  export let path = "index.html";
  export let channel = "";
  export let input = false;
  export let fill = false;
  export let title = "";
  export let style = "";
  /** "16:9", "4:3" or width / height: size the frame to it, centred, and leave the rest for the letterbox bars. */
  export let aspect: string | number | undefined = undefined;

  $: ratio = parseAspect(aspect);
  let stage: HTMLDivElement | undefined;
  let frameBox = "";

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

  // With an aspect the iframe is fitted into the stage in JS, so the exact
  // rect is known and published for the bars (sdk/game-rect.ts).
  function measure() {
    if (!stage || !ratio) return;
    const box = stage.getBoundingClientRect();
    const fit = fitAspect({ x: box.left, y: box.top, w: box.width, h: box.height }, ratio);
    frameBox = `width:${fit.w}px;height:${fit.h}px;`;
    if (app) reportWebRect(app.id, fit);
  }

  // The shell animates in with a transform; measure again once it settles.
  let settle: number | undefined;
  function measureSoon() {
    measure();
    window.clearTimeout(settle);
    settle = window.setTimeout(measure, 450);
  }

  let observer: ResizeObserver | undefined;
  $: if (stage && ratio) {
    observer?.disconnect();
    observer = new ResizeObserver(measureSoon);
    observer.observe(stage);
    measure();
  }
  onDestroy(() => {
    observer?.disconnect();
    window.clearTimeout(settle);
    if (app && ratio) reportWebRect(app.id, null);
  });

  function focusFrame() {
    // Keys from the page and the phone keyboard go to the game.
    frame?.focus();
  }
</script>

<svelte:window on:resize={measureSoon} />

{#if error}
  <div class="yui-app-embed-blocked">
    <strong>Couldn't open this app's page</strong>
    <span>{error}</span>
  </div>
{:else if ratio}
  <div class="yui-app-web-stage" bind:this={stage}>
    <iframe
      bind:this={frame}
      class="yui-app-embed yui-app-web-embed aspect"
      src={src || "about:blank"}
      title={title || app?.name || "App page"}
      allow="autoplay; fullscreen; gamepad"
      allowfullscreen
      style={frameBox}
      on:load={focusFrame}
    ></iframe>
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
