<script context="module" lang="ts">
  // Kenney Mobile Controls glyphs (CC0, see assets/kenney/LICENSE.md). Vite inlines
  // these small files as data: URIs, which /play's img-src allows; any that grow
  // past the inline limit load from the same origin (also allowed).
  const files = import.meta.glob('./assets/kenney/icon_*.svg', { eager: true, query: '?url', import: 'default' }) as Record<
    string,
    string
  >
  const urls: Record<string, string> = {}
  for (const [path, url] of Object.entries(files)) urls[path.replace(/^.*icon_|\.svg$/g, '')] = url

  export type IconName = 'menu' | 'sound' | 'checkmark' | 'cross' | 'search' | 'arrow_rotate' | 'play' | 'star' | 'crosshair'
</script>

<script lang="ts">
  export let name: IconName
  /** Pixel size (square). */
  export let size = 22

  $: url = urls[name] ?? ''
</script>

<!-- A white glyph used as a mask, so it takes the surrounding text colour. -->
<span class="icon" aria-hidden="true" style:--icon={`url("${url}")`} style:width="{size}px" style:height="{size}px"></span>

<style>
  .icon {
    display: inline-block;
    flex: 0 0 auto;
    background-color: currentColor;
    -webkit-mask: var(--icon) center / contain no-repeat;
    mask: var(--icon) center / contain no-repeat;
  }
</style>
