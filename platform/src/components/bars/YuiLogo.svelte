<script lang="ts">
  /**
   * Top bar, top-right corner: the Yui logo. It sits beside the exit icon while
   * the shell is fullscreen, and takes the corner itself when there is no exit
   * icon (a native game's bars).
   */
  import logoUrl from "../../assets/images/logo.png";
  import { platformAssetUrl } from "../../assets";
  import { shellFullscreenState } from "../../sdk/game-rect";

  // svelte-ignore unused-export-let
  export let rect: unknown = undefined;
  // svelte-ignore unused-export-let
  export let gameRect: unknown = undefined;
  // svelte-ignore unused-export-let
  export let players: unknown = undefined;
  // svelte-ignore unused-export-let
  export let bar: unknown = undefined;

  const src = platformAssetUrl(logoUrl);
</script>

<span class="bar-logo" class:beside-exit={$shellFullscreenState}>
  <img {src} alt="Yui" draggable="false" />
</span>
