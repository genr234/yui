<script lang="ts">
  import type { Node } from '@yui/protocol'
  import PadButton from '../controls/PadButton.svelte'
  import Dpad from '../controls/Dpad.svelte'
  import Stick from '../controls/Stick.svelte'
  import Trackpad from '../controls/Trackpad.svelte'
  import Keyboard from '../controls/Keyboard.svelte'
  import Choice from '../controls/Choice.svelte'
  import List from '../controls/List.svelte'
  import Sketch from '../controls/Sketch.svelte'
  import MotionChip from '../controls/MotionChip.svelte'

  /** An expanded tree (presets already replaced, see presets.ts expandTree). */
  export let node: Node

  $: props = (node.props ?? {}) as Record<string, unknown>
  $: children = node.children ?? []
  $: gap = typeof props.gap === 'number' && props.gap >= 0 ? Math.min(props.gap, 64) : 12
  $: flex = typeof props.flex === 'number' && props.flex >= 0 ? props.flex : undefined
  $: columns = typeof props.columns === 'number' && props.columns > 0 ? Math.min(6, Math.floor(props.columns)) : 2
  $: align = props.align === 'left' || props.align === 'right' ? props.align : 'center'
  $: flexStyle = flex !== undefined ? `flex:${flex} ${flex} 0` : ''
  $: boxStyle = [`--gap:${gap}px`, flexStyle, node.type === 'grid' ? `--cols:${columns}` : ''].filter(Boolean).join(';')
</script>

{#if node.type === 'row' || node.type === 'column' || node.type === 'grid'}
  <div class="lv {node.type}" class:square={props.square === true} class:rigid={flex === undefined} style={boxStyle}>
    {#each children as child}
      <svelte:self node={child} />
    {/each}
  </div>
{:else if node.type === 'spacer'}
  <div class="lv-spacer" style={flex === undefined ? 'flex:1 1 0' : flexStyle}></div>
{:else if node.type === 'text'}
  <p class="lv-text {props.size === 'small' || props.size === 'large' ? props.size : ''}" style="text-align:{align};{flexStyle}">
    {String(props.text ?? props.label ?? '')}
  </p>
{:else if node.type === 'button'}
  <PadButton {props} style={flexStyle} />
{:else if node.type === 'dpad'}
  <Dpad {props} style={flexStyle} />
{:else if node.type === 'stick'}
  <Stick {props} style={flexStyle} />
{:else if node.type === 'trackpad'}
  <Trackpad {props} style={flexStyle} />
{:else if node.type === 'keyboard'}
  <Keyboard {props} style={flexStyle} />
{:else if node.type === 'choice'}
  <Choice {props} style={flexStyle} />
{:else if node.type === 'list'}
  <List {props} style={flexStyle} />
{:else if node.type === 'sketch'}
  <Sketch {props} style={flexStyle} />
{:else if node.type === 'motion'}
  <MotionChip {props} style={flexStyle} />
{/if}

<style>
  .lv {
    display: flex;
    gap: var(--gap);
    min-width: 0;
    min-height: 0;
  }
  /* A box without `flex` never shrinks below its content (its controls would overlap). */
  .lv.rigid {
    flex-shrink: 0;
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
  .grid.square {
    height: min(100%, 260px);
    min-height: 150px;
    width: auto;
    max-width: 100%;
    aspect-ratio: 1;
    margin: auto;
    grid-template-rows: repeat(3, minmax(0, 1fr));
    place-items: center;
  }
  .grid.square > :global(.pb.round) {
    --round: min(76px, 100%);
    width: 100%;
    height: 100%;
    max-width: 84px;
    max-height: 84px;
  }
  .lv-spacer {
    min-height: 0;
    min-width: 0;
  }
  .lv-text {
    margin: 0;
    color: var(--muted);
    font-size: 1rem;
    overflow-wrap: anywhere;
  }
  .lv-text.small {
    font-size: 0.85rem;
  }
  .lv-text.large {
    color: var(--fg);
    font-size: 1.5rem;
    font-weight: 900;
    text-shadow: 0 2px 0 var(--kit-sky-line);
  }
</style>
