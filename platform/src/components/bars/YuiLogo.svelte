<script lang="ts">
  /**
   * Top bar, top-left corner: the Yui logo (the exit icon has the top-right one).
   */
  import logoUrl from "../../assets/images/logo.png";
  import { platformAssetUrl } from "../../assets";

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

<span class="bar-logo">
  <img {src} alt="Yui" draggable="false" />
</span>
