<script lang="ts">
  /** Top bar, left: who set the game up. The overlay is not told who launched a title, so this is the host phone's name. */
  import type { Rect } from "../../sdk/game-rect";
  import type { PadPlayer } from "../../sdk/pad";
  import { pickedBy } from "./chips";

  // svelte-ignore unused-export-let
  export let rect: Rect | undefined = undefined;
  // svelte-ignore unused-export-let
  export let gameRect: Rect | undefined = undefined;
  export let players: PadPlayer[] = [];
  // svelte-ignore unused-export-let
  export let bar: "top" | "bottom" = "top";

  $: name = pickedBy(players);
</script>

{#if name}
  <div class="picked-by">picked by <strong>{name}</strong></div>
{/if}
