<script lang="ts">
  import { onDestroy } from 'svelte'
  import { StickDriver } from './drivers'
  import { capture, localPoint, usePad } from './context'
  import { validBit } from './sink'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  $: id = typeof props.id === 'string' ? props.id : ''
  $: side = props.side === 'right' ? ('right' as const) : ('left' as const)
  $: clickBit = validBit(props.click_bit)

  let driver: StickDriver
  let active = false
  let base: [number, number] = [0, 0]
  let knob: [number, number] = [0, 0]
  let radius = 60
  $: rebuild(id, side, clickBit)

  function rebuild(id: string, side: 'left' | 'right', clickBit: number | null) {
    driver?.reset()
    driver = new StickDriver(sink, id, side, clickBit, radius)
    active = false
    knob = [0, 0]
  }

  function down(e: PointerEvent) {
    if (driver.touching) return
    capture(e)
    const p = localPoint(e.currentTarget as Element, e)
    radius = Math.max(36, Math.min(p.w, p.h) * 0.32)
    driver.radius = radius
    driver.down(e.pointerId, p.x, p.y)
    base = [p.x, p.y]
    active = true
    knob = [0, 0]
  }
  function move(e: PointerEvent) {
    const p = localPoint(e.currentTarget as Element, e)
    driver.move(e.pointerId, p.x, p.y)
    knob = driver.knob
  }
  function up(e: PointerEvent, cancelled = false) {
    driver.up(e.pointerId, cancelled)
    active = driver.touching
    knob = driver.knob
  }

  onDestroy(() => driver?.reset())
</script>

<div
  class="stick"
  class:active
  {style}
  role="group"
  aria-label="{side} stick"
  on:pointerdown|preventDefault={down}
  on:pointermove={move}
  on:pointerup={(e) => up(e)}
  on:pointercancel={(e) => up(e, true)}
  on:lostpointercapture={(e) => up(e, true)}
>
  {#if active}
    <span class="ring" style="left:{base[0]}px;top:{base[1]}px;width:{radius * 2}px;height:{radius * 2}px"></span>
    <span
      class="knob"
      style="left:{base[0] + knob[0] * radius}px;top:{base[1] - knob[1] * radius}px"
    ></span>
  {:else}
    <span class="ring idle"></span>
    <span class="knob idle"></span>
  {/if}
</div>

<style>
  .stick {
    position: relative;
    flex: 1;
    min-width: 120px;
    min-height: 140px;
    border-radius: var(--yui-radius-shell);
    background: rgba(255, 255, 255, 0.1);
    box-shadow: inset 0 0 0 2px rgba(255, 255, 255, 0.25);
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    overflow: hidden;
  }
  .ring,
  .knob {
    position: absolute;
    border-radius: 50%;
    transform: translate(-50%, -50%);
    pointer-events: none;
  }
  /* A sunken well the knob rides in. */
  .ring {
    border: 2px solid var(--kit-panel-line);
    background: var(--kit-well);
    box-shadow: inset 0 6px 0 rgba(58, 65, 81, 0.14), 0 3px 0 rgba(255, 255, 255, 0.35);
  }
  .ring.idle {
    left: 50%;
    top: 50%;
    width: 64%;
    aspect-ratio: 1;
    max-height: 90%;
    max-width: 90%;
  }
  .knob {
    width: 64px;
    height: 64px;
    border: 2px solid var(--kit-key-line);
    background: linear-gradient(180deg, #fff 0 50%, var(--kit-key) 50%);
    box-shadow: 0 var(--yui-lip) 0 var(--kit-key-lip), inset 0 0 0 6px rgba(58, 65, 81, 0.05);
  }
  .knob.idle {
    left: 50%;
    top: 50%;
  }
  .active .knob {
    border-color: color-mix(in srgb, var(--player), #000 26%);
    background: linear-gradient(180deg, color-mix(in srgb, var(--player), #fff 16%) 0 50%, var(--player) 50%);
    box-shadow: 0 var(--yui-lip) 0 color-mix(in srgb, var(--player), #000 26%), inset 0 0 0 3px rgba(255, 255, 255, 0.3);
  }
</style>
