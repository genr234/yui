<script lang="ts">
  import { onDestroy } from 'svelte'
  import { ButtonDriver } from './drivers'
  import { capture, usePad } from './context'
  import { validBit } from './sink'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  $: id = typeof props.id === 'string' ? props.id : ''
  $: bit = validBit(props.bit)
  $: label = String(props.label ?? props.text ?? '')
  $: variant = typeof props.variant === 'string' ? props.variant : bit !== null ? 'secondary' : 'primary'
  $: shape = props.shape === 'round' ? 'round' : props.shape === 'pill' ? 'pill' : 'block'
  $: size = props.size === 'small' || props.size === 'large' ? props.size : 'normal'

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
  <span>{label}</span>
</button>

<style>
  .pb {
    flex: 1;
    min-width: 0;
    min-height: 64px;
    padding: 10px;
    border: 0;
    border-radius: var(--yui-radius-hero);
    background: var(--accent);
    color: #fff;
    font: inherit;
    font-size: 1.15rem;
    font-weight: 800;
    letter-spacing: 0.01em;
    box-shadow: 0 var(--yui-lip) 0 var(--accent-lip), 0 8px 18px rgba(0, 0, 0, 0.25);
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
    transition: transform 60ms var(--yui-ease-out), box-shadow 60ms var(--yui-ease-out), filter 60ms;
  }
  .pb span {
    pointer-events: none;
    display: block;
    overflow: hidden;
    line-height: 1.1;
    overflow-wrap: anywhere;
  }
  /* In a column a button keeps its height; the trackpad or stick takes the room (a `flex` prop overrides). */
  :global(.column) > .pb {
    flex: 0 0 auto;
  }
  .pb.pressed {
    transform: translateY(var(--yui-lip)) scale(0.97);
    box-shadow: 0 0 0 var(--accent-lip), 0 0 0 4px var(--player) inset;
    filter: brightness(1.15);
  }
  .pb[data-variant='primary'] {
    background: var(--player);
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.35), 0 8px 18px rgba(0, 0, 0, 0.25);
  }
  .pb[data-variant='primary'].pressed {
    box-shadow: 0 0 0 transparent, 0 0 0 4px rgba(255, 255, 255, 0.55) inset;
  }
  .pb[data-variant='secondary'] {
    background: var(--surface-2);
    color: var(--fg);
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.4), 0 0 0 1px var(--line) inset;
  }
  .pb[data-variant='danger'] {
    background: var(--yui-red);
    box-shadow: 0 var(--yui-lip) 0 #a83434;
  }
  .pb[data-variant='ghost'] {
    background: transparent;
    color: var(--fg);
    box-shadow: 0 0 0 2px var(--line) inset;
  }
  .pb.round {
    flex: 0 0 auto;
    width: var(--round, 76px);
    height: var(--round, 76px);
    min-height: 0;
    border-radius: 50%;
    align-self: center;
    justify-self: center;
  }
  .pb.round.large {
    --round: 104px;
    font-size: 1.8rem;
  }
  .pb.round.small {
    --round: 56px;
  }
  .pb.pill {
    border-radius: 999px;
  }
  :global(.column) > .pb.small {
    flex: 0 0 auto;
  }
  .pb.small {
    min-height: 44px;
    font-size: 0.95rem;
  }
  .pb.large:not(.round) {
    min-height: 96px;
    font-size: 1.6rem;
  }
</style>
