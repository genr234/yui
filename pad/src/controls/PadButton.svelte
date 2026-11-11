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
  /*
   * Chunky game buttons: a lit top half, a darker rim and a lip the face
   * sinks onto when pressed. --b is the face colour.
   */
  .pb {
    --b: var(--accent);
    --b-hi: color-mix(in srgb, var(--b), #fff 16%);
    --b-edge: color-mix(in srgb, var(--b), #000 26%);
    --b-ink: #fff;
    flex: 1;
    min-width: 0;
    min-height: 64px;
    padding: 10px;
    border: 2px solid var(--b-edge);
    border-radius: var(--yui-radius-hero);
    background: linear-gradient(180deg, var(--b-hi) 0 48%, var(--b) 48% 100%);
    color: var(--b-ink);
    font: inherit;
    font-size: 1.15rem;
    font-weight: 900;
    letter-spacing: 0.01em;
    text-shadow: 0 2px 0 var(--b-edge);
    box-shadow: 0 var(--yui-lip) 0 var(--b-edge), inset 0 2px 0 rgba(255, 255, 255, 0.35);
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
    transition: transform 50ms var(--yui-ease-out), box-shadow 50ms var(--yui-ease-out), filter 50ms;
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
    transform: translateY(var(--yui-lip));
    box-shadow: 0 0 0 var(--b-edge), inset 0 2px 0 rgba(255, 255, 255, 0.25);
    filter: brightness(1.08);
  }
  .pb[data-variant='primary'] {
    --b: var(--player);
  }
  /* White keys, like a menu's plain buttons; they light in the player's colour. */
  .pb[data-variant='secondary'] {
    --b: var(--kit-key);
    --b-hi: #fff;
    --b-edge: var(--kit-key-lip);
    --b-ink: var(--ink);
    border-color: var(--kit-key-line);
    text-shadow: none;
    box-shadow: 0 var(--yui-lip) 0 var(--kit-key-lip), inset 0 -3px 0 rgba(58, 65, 81, 0.06);
  }
  .pb[data-variant='secondary'].pressed {
    --b: var(--player);
    --b-hi: color-mix(in srgb, var(--player), #fff 16%);
    --b-ink: #fff;
    border-color: color-mix(in srgb, var(--player), #000 26%);
    box-shadow: none;
    filter: none;
  }
  .pb[data-variant='danger'] {
    --b: var(--kit-red);
  }
  .pb[data-variant='ghost'] {
    background: rgba(255, 255, 255, 0.12);
    border: 2px dashed rgba(255, 255, 255, 0.7);
    color: #fff;
    text-shadow: none;
    box-shadow: none;
  }
  .pb[data-variant='ghost'].pressed {
    background: rgba(255, 255, 255, 0.3);
    transform: none;
  }
  .pb.round {
    flex: 0 0 auto;
    width: var(--round, 76px);
    height: var(--round, 76px);
    min-height: 0;
    padding: 0;
    border-radius: 50%;
    align-self: center;
    justify-self: center;
    /* A ring of shine, like a game's round icon buttons. */
    box-shadow: 0 var(--yui-lip) 0 var(--b-edge), inset 0 0 0 3px rgba(255, 255, 255, 0.3);
  }
  .pb.round.pressed {
    box-shadow: 0 0 0 var(--b-edge), inset 0 0 0 3px rgba(255, 255, 255, 0.3);
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
    --yui-lip: 3px;
  }
  .pb.large:not(.round) {
    min-height: 96px;
    font-size: 1.6rem;
  }
</style>
