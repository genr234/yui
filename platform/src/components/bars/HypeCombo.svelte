<script lang="ts">
  /**
   * Top bar, behind the title: the combo banner (the same emote from two or
   * more players within two seconds becomes one big emote with "x3") and the
   * hype meter, which each reaction fills and time drains.
   */
  import { onDestroy, onMount } from "svelte";
  import type { Rect } from "../../sdk/game-rect";
  import type { PadPlayer } from "../../sdk/pad";
  import { emoteUrl } from "./emotes";
  import { social } from "./social";

  // svelte-ignore unused-export-let
  export let rect: Rect | undefined = undefined;
  // svelte-ignore unused-export-let
  export let gameRect: Rect | undefined = undefined;
  // svelte-ignore unused-export-let
  export let players: PadPlayer[] = [];
  // svelte-ignore unused-export-let
  export let bar: "top" | "bottom" = "top";

  let stop: (() => void) | undefined;
  onMount(() => (stop = social.start()));
  onDestroy(() => stop?.());

  $: combo = $social.combo;
  $: hype = Math.min(1, Math.max(0, $social.hype));
  $: level = hype >= 0.75 ? "hot" : hype >= 0.4 ? "warm" : "cool";
</script>

<div class="hype" aria-hidden="true">
  {#if combo}
    {#key combo.key}
      <div class="combo">
        <img class="combo-emote" src={emoteUrl(combo.emote)} alt="" draggable="false" />
        <span class="combo-count">x{combo.count}</span>
      </div>
    {/key}
  {/if}
  <div class="hype-meter" class:on={hype > 0.01} data-level={level}>
    <span class="hype-label">HYPE</span>
    <span class="hype-track"><span class="hype-fill" style="width:{(hype * 100).toFixed(1)}%"></span></span>
  </div>
</div>
