<script lang="ts">
  import { onDestroy } from 'svelte'
  import { TrackpadDriver } from './drivers'
  import { capture, localPoint, usePad } from './context'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  $: id = typeof props.id === 'string' ? props.id : ''
  $: mode = props.mode === 'absolute' ? ('absolute' as const) : ('relative' as const)
  $: label = typeof props.label === 'string' ? props.label : mode === 'absolute' ? 'Touch to aim' : 'Trackpad'

  let driver: TrackpadDriver
  let fingers = 0
  let dot: [number, number] | null = null
  $: rebuild(id, mode)

  function rebuild(id: string, mode: 'relative' | 'absolute') {
    driver?.reset()
    driver = new TrackpadDriver(sink, id, mode)
    fingers = 0
    dot = null
  }

  function down(e: PointerEvent) {
    capture(e)
    const p = localPoint(e.currentTarget as Element, e)
    driver.down(e.pointerId, p.x, p.y, p.nx, p.ny)
    fingers = driver.fingers
    if (fingers === 1) sink.feedback()
    dot = [p.x, p.y]
  }
  function move(e: PointerEvent) {
    const p = localPoint(e.currentTarget as Element, e)
    driver.move(e.pointerId, p.x, p.y, p.nx, p.ny)
    if (driver.fingers) dot = [p.x, p.y]
  }
  function up(e: PointerEvent, cancelled = false) {
    driver.up(e.pointerId, cancelled)
    fingers = driver.fingers
    if (!fingers) dot = null
  }

  onDestroy(() => driver?.reset())
</script>

<div
  class="pad"
  class:touching={fingers > 0}
  {style}
  role="group"
  aria-label={label}
  on:pointerdown|preventDefault={down}
  on:pointermove={move}
  on:pointerup={(e) => up(e)}
  on:pointercancel={(e) => up(e, true)}
  on:lostpointercapture={(e) => up(e, true)}
>
  <span class="label">{fingers === 2 ? 'Scrolling' : label}</span>
  {#if dot}<span class="dot" style="left:{dot[0]}px;top:{dot[1]}px"></span>{/if}
  {#if mode === 'absolute'}<span class="cross"></span>{/if}
</div>

<style>
  .pad {
    position: relative;
    flex: 1;
    min-height: 96px;
    border: 2px solid var(--kit-panel-line);
    border-radius: var(--yui-radius-shell);
    background: var(--kit-panel);
    box-shadow: 0 var(--yui-lip) 0 var(--kit-panel-lip), inset 0 6px 0 rgba(58, 65, 81, 0.07);
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    overflow: hidden;
    display: grid;
    place-items: center;
  }
  .pad.touching {
    border-color: var(--player);
  }
  .label {
    padding: 0 20px;
    text-align: center;
    color: var(--ink-muted);
    font-weight: 800;
    pointer-events: none;
  }
  .dot {
    position: absolute;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    transform: translate(-50%, -50%);
    background: var(--player);
    opacity: 0.45;
    pointer-events: none;
  }
  .cross {
    position: absolute;
    left: 50%;
    top: 50%;
    width: 16px;
    height: 16px;
    transform: translate(-50%, -50%);
    border-radius: 50%;
    border: 2px solid var(--kit-panel-line);
    pointer-events: none;
  }
</style>
