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
    background: radial-gradient(circle at 50% 50%, var(--yui-chrome-fill), transparent 70%);
    box-shadow: 0 0 0 1px var(--line) inset;
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
  .ring {
    border: 2px solid var(--line);
    background: rgba(0, 0, 0, 0.18);
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
    background: var(--surface-2);
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.4), 0 0 0 1px var(--line) inset;
  }
  .knob.idle {
    left: 50%;
    top: 50%;
  }
  .active .knob {
    background: var(--player);
  }
</style>
