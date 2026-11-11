<script lang="ts">
  /**
   * Fills the space a game leaves above and below it (on the portrait kiosk,
   * a 16:9 or 4:3 game leaves a lot) with a top and a bottom bar. The bars
   * only host items; features register them in ./items.ts (see registry.ts).
   */
  import { onDestroy, onMount } from "svelte";
  import { barRects, barsApp, gameRect, watchNativeWindow } from "../../sdk/game-rect";
  import { pad, type PadPlayer } from "../../sdk/pad";
  import "./items";
  import { barItems, onBarItemsChanged, type BarItem, type BarZone } from "./registry";

  /** True while the PIN screen or Settings is open: no bars. */
  export let sensitive = false;

  let viewport = { w: window.innerWidth, h: window.innerHeight };
  let players: PadPlayer[] = [];
  let version = 0;
  const offs: Array<() => void> = [];

  const zones: BarZone[] = ["fill", "left", "center", "right"];

  function resize() {
    viewport = { w: window.innerWidth, h: window.innerHeight };
  }

  onMount(() => {
    offs.push(watchNativeWindow());
    offs.push(onBarItemsChanged(() => (version += 1)));
    offs.push(pad.onState((state) => (players = state.players)));
    void pad.state().then((state) => (players = state.players));
  });
  onDestroy(() => offs.forEach((off) => off()));

  $: hidden = sensitive || $barsApp?.bars === false;
  $: layout = hidden ? null : barRects($gameRect, viewport);
  $: bars = layout
    ? ([
        ["top", layout.top],
        ["bottom", layout.bottom],
      ] as const).filter((entry) => entry[1] !== null)
    : [];

  function itemsFor(bar: "top" | "bottom", zone: BarZone, _version: number): BarItem[] {
    return barItems(bar, zone);
  }
</script>

<svelte:window on:resize={resize} />

{#each bars as [bar, rect] (bar)}
  {#if rect && $gameRect}
    <div
      class="letterbox-bar letterbox-{bar}"
      data-bar={bar}
      style="left:{rect.x}px;top:{rect.y}px;width:{rect.w}px;height:{rect.h}px"
    >
      {#each zones as zone (zone)}
        {@const list = itemsFor(bar, zone, version)}
        {#if list.length > 0}
          <div class="bar-zone bar-zone-{zone}">
            {#each list as item (item.id)}
              <svelte:component this={item.component} {rect} gameRect={$gameRect} {players} {bar} />
            {/each}
          </div>
        {/if}
      {/each}
    </div>
  {/if}
{/each}
