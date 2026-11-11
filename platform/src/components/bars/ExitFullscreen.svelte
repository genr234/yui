<script lang="ts">
  import { shellFullscreenState } from "../../sdk/game-rect";

  // While bars are up they cover the app's own back/exit buttons, so the bar
  // carries one way out: leaving fullscreen removes the bars again.
  // svelte-ignore unused-export-let
  export let rect: unknown = undefined;
  // svelte-ignore unused-export-let
  export let gameRect: unknown = undefined;
  // svelte-ignore unused-export-let
  export let players: unknown = undefined;
  // svelte-ignore unused-export-let
  export let bar: unknown = undefined;

  function exit() {
    window.dispatchEvent(new CustomEvent("yui:shell-fullscreen", { detail: { active: false } }));
  }
</script>

{#if $shellFullscreenState}
  <button class="bar-exit" type="button" on:click={exit}>Exit fullscreen</button>
{/if}
