<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { cursorColor, cursorIdleMs, pad, type PadCursor } from "../sdk/pad";

  // The phone trackpad's cursor. The controller moves the real mouse over
  // CDP; this only draws where it is, so it never takes pointer events.
  let cursor: PadCursor | null = null;
  let shown = false;
  let clicks = 0;
  let idleTimer: number | undefined;

  function update(next: PadCursor) {
    cursor = next;
    shown = next.visible;
    if (next.click) clicks += 1;
    window.clearTimeout(idleTimer);
    if (next.visible) {
      idleTimer = window.setTimeout(() => (shown = false), cursorIdleMs);
    }
  }

  onMount(() => pad.onCursor(update));
  onDestroy(() => window.clearTimeout(idleTimer));
</script>

{#if cursor}
  <div class="pad-cursor-layer" aria-hidden="true">
    <div
      class="pad-cursor"
      class:shown
      style={`transform: translate(${cursor.x}px, ${cursor.y}px); --pad-cursor-color: ${cursorColor(cursor.color)};`}
    >
      {#key clicks}
        {#if clicks > 0}
          <span class="pad-cursor-ripple"></span>
        {/if}
      {/key}
      <svg class="pad-cursor-arrow" viewBox="0 0 24 24" width="28" height="28">
        <path d="M3 2.5 L3 19.5 L8 15 L11.5 22 L14.8 20.5 L11.4 13.6 L18 13.2 Z" />
      </svg>
    </div>
  </div>
{/if}
