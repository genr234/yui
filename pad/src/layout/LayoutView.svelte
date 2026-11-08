<script lang="ts">
  import type { Node } from '@yui/protocol'

  export let node: Node
  export let onEvent: (e: { control: string; kind: 'press' | 'release' }) => void = () => {}

  $: props = node.props ?? {}
  $: children = node.children ?? []
  $: id = typeof props.id === 'string' ? props.id : ''
  $: gap = typeof props.gap === 'number' ? props.gap : 12
  $: flex = typeof props.flex === 'number' ? props.flex : undefined
  $: columns = typeof props.columns === 'number' && props.columns > 0 ? Math.floor(props.columns) : 2
  $: label = String(props.label ?? props.text ?? '')
  $: style = [
    `--gap:${gap}px`,
    flex !== undefined ? `flex:${flex}` : '',
    node.type === 'grid' ? `--cols:${columns}` : '',
  ]
    .filter(Boolean)
    .join(';')

  let down = false

  function press(ev: PointerEvent) {
    if (!id || down) return
    down = true
    ;(ev.currentTarget as HTMLElement).setPointerCapture?.(ev.pointerId)
    onEvent({ control: id, kind: 'press' })
  }
  function release() {
    if (!down) return
    down = false
    onEvent({ control: id, kind: 'release' })
  }
</script>

{#if node.type === 'row' || node.type === 'column' || node.type === 'grid'}
  <div class="lv {node.type}" {style}>
    {#each children as child}
      <svelte:self node={child} {onEvent} />
    {/each}
  </div>
{:else if node.type === 'spacer'}
  <div class="lv-spacer" style={flex === undefined ? 'flex:1' : style}></div>
{:else if node.type === 'text'}
  <p class="lv-text" class:big={props.size === 'large'} style={style}>{label}</p>
{:else if node.type === 'button'}
  <button
    type="button"
    class="lv-button"
    data-variant={props.variant ?? 'primary'}
    style={style}
    on:pointerdown|preventDefault={press}
    on:pointerup={release}
    on:pointercancel={release}
    on:lostpointercapture={release}
    on:contextmenu|preventDefault
  >
    {label}
  </button>
{:else}
  <div class="lv-unknown" style={style}>unsupported: {node.type}</div>
{/if}

<style>
  .lv {
    display: flex;
    gap: var(--gap);
    min-width: 0;
  }
  .row {
    flex-direction: row;
    align-items: stretch;
  }
  .column {
    flex-direction: column;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
  }
  .lv-spacer {
    min-height: 0;
    min-width: 0;
  }
  .lv-text {
    margin: 0;
    color: var(--muted);
    text-align: center;
    font-size: 1rem;
    overflow-wrap: anywhere;
  }
  .lv-text.big {
    color: var(--fg);
    font-size: 1.5rem;
    font-weight: 700;
  }
  .lv-button {
    flex: 1;
    min-width: 0;
    min-height: 72px;
    padding: 12px;
    border: 0;
    border-radius: 24px;
    background: var(--accent);
    color: #0f1020;
    font: inherit;
    font-size: 1.25rem;
    font-weight: 800;
    box-shadow: 0 6px 0 rgba(0, 0, 0, 0.35);
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
    transition: transform 60ms, box-shadow 60ms;
  }
  .lv-button:active {
    transform: translateY(5px);
    box-shadow: 0 1px 0 rgba(0, 0, 0, 0.35);
  }
  .lv-button[data-variant='secondary'] {
    background: var(--surface-2);
    color: var(--fg);
  }
  .lv-button[data-variant='danger'] {
    background: #ff5470;
  }
  .lv-unknown {
    padding: 8px;
    border: 1px dashed var(--muted);
    border-radius: 12px;
    color: var(--muted);
    font-size: 0.8rem;
  }
</style>
