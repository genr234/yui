<script context="module" lang="ts">
  // Thin line glyphs for settings rows (drawn for Yui; 24×24, stroked with the text colour).
  export type SettingIconName =
    | 'target'
    | 'tilt'
    | 'refresh'
    | 'swap'
    | 'volume'
    | 'pulse'
    | 'speaker'
    | 'bell'
    | 'user'
    | 'link'
    | 'logout'
    | 'power'
    | 'chevron'
</script>

<script lang="ts">
  export let name: SettingIconName
  export let size = 20
</script>

<svg
  class="setting-icon"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {#if name === 'target'}
    <circle cx="12" cy="12" r="8" /><circle cx="12" cy="12" r="3" /><path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
  {:else if name === 'tilt'}
    <rect x="7" y="3" width="10" height="18" rx="2.5" transform="rotate(-18 12 12)" /><path d="M3 7a9 9 0 0 1 3-4M21 17a9 9 0 0 1-3 4" />
  {:else if name === 'refresh'}
    <path d="M20 12a8 8 0 1 1-2.34-5.66" /><path d="M20 4v5h-5" />
  {:else if name === 'swap'}
    <path d="M7 4 3 8l4 4" /><path d="M3 8h14" /><path d="m17 20 4-4-4-4" /><path d="M21 16H7" />
  {:else if name === 'volume'}
    <path d="M11 5 6 9H3v6h3l5 4z" /><path d="M15.5 8.5a5 5 0 0 1 0 7" /><path d="M18.5 5.5a9 9 0 0 1 0 13" />
  {:else if name === 'pulse'}
    <path d="M2 12h4l3-7 6 14 3-7h4" />
  {:else if name === 'speaker'}
    <rect x="5" y="2.5" width="14" height="19" rx="3" /><circle cx="12" cy="14" r="3.5" /><path d="M12 6.5h.01" />
  {:else if name === 'bell'}
    <path d="M6 16v-5a6 6 0 0 1 12 0v5l1.5 2h-15z" /><path d="M10 21h4" />
  {:else if name === 'user'}
    <circle cx="12" cy="8" r="4" /><path d="M4 21a8 8 0 0 1 16 0" />
  {:else if name === 'link'}
    <path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1" /><path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1" />
  {:else if name === 'logout'}
    <path d="M9 4H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h4" /><path d="m15 16 4-4-4-4" /><path d="M19 12H9" />
  {:else if name === 'power'}
    <path d="M12 3v9" /><path d="M6.34 6.34a8 8 0 1 0 11.32 0" />
  {:else if name === 'chevron'}
    <path d="m9 6 6 6-6 6" />
  {/if}
</svg>

<style>
  .setting-icon {
    display: block;
    flex: 0 0 auto;
  }
</style>
