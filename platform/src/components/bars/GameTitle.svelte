<script lang="ts">
  import { barsApp } from "../../sdk/game-rect";
  import type { Rect } from "../../sdk/game-rect";
  import type { PadPlayer } from "../../sdk/pad";

  // Props every bar item receives (see registry.ts); only some are used here.
  // svelte-ignore unused-export-let
  export let rect: Rect | undefined = undefined;
  // svelte-ignore unused-export-let
  export let gameRect: Rect | undefined = undefined;
  // svelte-ignore unused-export-let
  export let players: PadPlayer[] = [];
  // svelte-ignore unused-export-let
  export let bar: "top" | "bottom" = "top";
</script>

{#if $barsApp}
  <div class="bar-title">{$barsApp.name}</div>
{/if}
