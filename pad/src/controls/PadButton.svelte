<script lang="ts">
  import { onDestroy } from 'svelte'
  import { ButtonDriver } from './drivers'
  import { capture, usePad } from './context'
  import { validBit } from './sink'
  import Art from './Art.svelte'
  import { glyphFor } from './art'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  $: id = typeof props.id === 'string' ? props.id : ''
  $: bit = validBit(props.bit)
  $: label = String(props.label ?? props.text ?? '')
  $: variant = typeof props.variant === 'string' ? props.variant : bit !== null ? 'secondary' : 'primary'
  $: shape = props.shape === 'round' ? 'round' : props.shape === 'pill' ? 'pill' : 'block'
  $: size = props.size === 'small' || props.size === 'large' ? props.size : 'normal'

  $: glyph = shape === 'round' || label.length === 1 ? glyphFor(label) : null
  $: tinted = variant === 'primary' || variant === 'danger' || (variant === 'secondary' && pressed)
  $: artName = shape === 'round' ? 'button_circle' : shape === 'pill' ? 'button_circle_wide' : 'button_square_wide'
  $: scale = size === 'small' ? 0.75 : size === 'large' ? 1.25 : 1

  let driver: ButtonDriver
  let pressed = false
  $: rebuild(id, bit)

  function rebuild(id: string, bit: number | null) {
    driver?.reset()
    driver = new ButtonDriver(sink, id, bit === null ? 0 : 1 << bit)
    pressed = false
  }

  function down(e: PointerEvent) {
    capture(e)
    driver.down(e.pointerId)
    pressed = driver.pressed
  }
  function up(e: PointerEvent, cancelled = false) {
    driver.up(e.pointerId, cancelled)
    pressed = driver.pressed
  }

  onDestroy(() => driver?.reset())
</script>

<button
  type="button"
  class="pb {shape} {size}"
  class:pressed
  data-variant={variant}
  {style}
  aria-label={label || id}
  on:pointerdown|preventDefault={down}
  on:pointerup={(e) => up(e)}
  on:pointercancel={(e) => up(e, true)}
  on:lostpointercapture={(e) => up(e, true)}
  on:contextmenu|preventDefault
>
  <span class="face" class:k-player={tinted}>
    {#if variant !== 'ghost'}
      <span class="plate">
        {#if shape === 'round'}
          <Art name={artName} lit={pressed} />
        {:else if shape === 'pill'}
          <Art name={artName} mode="cap" cx={32} lit={pressed} />
        {:else}
          <Art name={artName} mode="nine" cx={12} cy={12} s={scale} lit={pressed} />
        {/if}
      </span>
    {/if}
    {#if glyph}
      <span class="glyph" style:--glyph={`url("${glyph}")`}></span>
    {:else}
      <span class="lbl">{label}</span>
    {/if}
  </span>
</button>

<style>
  /*
   * Kenney Mobile Controls art (style F), recoloured by the --k-* palette:
   * neutral for secondary keys, the player's colour for primary ones (.k-player).
   * The face sinks onto its lip and the art's glow outline lights when pressed.
   */
  .pb {
    position: relative;
    flex: 1;
    min-width: 0;
    min-height: 64px;
    padding: 0;
    border: 0;
    background: none;
    color: var(--k-ink);
    font: inherit;
    font-size: 1.15rem;
    font-weight: 900;
    letter-spacing: 0.01em;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
  }
  .face {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    padding: 8px 14px;
    color: var(--k-ink);
    transition: transform 50ms var(--yui-ease-out);
  }
  .plate {
    position: absolute;
    inset: 0;
    filter: drop-shadow(0 var(--yui-lip) 0 var(--k-lip));
    transition: filter 50ms var(--yui-ease-out);
  }
  .lbl {
    position: relative;
    max-width: 100%;
    overflow: hidden;
    line-height: 1.1;
    overflow-wrap: anywhere;
    pointer-events: none;
  }
  .face.k-player .lbl {
    text-shadow: 0 2px 0 var(--k-edge);
  }
  /* A white A/B/X/Y/L/R glyph from the pack; it takes the label colour. */
  .glyph {
    position: relative;
    width: 46%;
    height: 46%;
    max-width: 44px;
    max-height: 44px;
    background-color: var(--k-ink);
    -webkit-mask: var(--glyph) center / contain no-repeat;
    mask: var(--glyph) center / contain no-repeat;
    pointer-events: none;
  }
  .face.k-player .glyph {
    filter: drop-shadow(0 2px 0 var(--k-edge));
  }
  .pill .glyph,
  .block .glyph {
    width: 28px;
    height: 28px;
  }
  /* In a column a button keeps its height; the trackpad or stick takes the room (a `flex` prop overrides). */
  :global(.column) > .pb {
    flex: 0 0 auto;
  }
  .pb.pressed .face {
    transform: translateY(var(--yui-lip));
  }
  .pb.pressed .plate {
    filter: drop-shadow(0 0 0 var(--k-lip));
  }
  .pb.pressed .face.k-player {
    --k-face: color-mix(in srgb, var(--player), #000 8%);
  }
  .pb[data-variant='danger'] {
    --player: var(--kit-red);
  }
  .pb[data-variant='ghost'] {
    --k-ink: #fff;
  }
  .pb[data-variant='ghost'] .face {
    margin: 2px;
    border: 2px dashed rgba(255, 255, 255, 0.7);
    border-radius: var(--yui-radius-hero);
    background: rgba(255, 255, 255, 0.12);
  }
  .pb[data-variant='ghost'].pressed .face {
    background: rgba(255, 255, 255, 0.3);
    transform: none;
  }
  .pb.round {
    flex: 0 0 auto;
    width: var(--round, 76px);
    height: var(--round, 76px);
    min-height: 0;
    padding: 0;
    align-self: center;
    justify-self: center;
  }
  .pb.round .face {
    padding: 0;
  }
  .pb.round.large {
    --round: 104px;
    font-size: 1.8rem;
  }
  .pb.round.small {
    --round: 56px;
  }
  .pb.pill .face {
    padding: 6px 18px;
  }
  :global(.column) > .pb.small {
    flex: 0 0 auto;
  }
  .pb.small {
    min-height: 44px;
    font-size: 0.95rem;
    --yui-lip: 3px;
  }
  .pb.large:not(.round) {
    min-height: 96px;
    font-size: 1.6rem;
  }
</style>
