<script lang="ts">
  import { onDestroy } from 'svelte'
  import { DpadDriver, type Dir } from './drivers'
  import { capture, localPoint, usePad } from './context'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()
  const ALL: Dir[] = ['up', 'down', 'left', 'right']

  $: id = typeof props.id === 'string' ? props.id : ''
  $: mode = props.mode === 'events' ? ('events' as const) : ('bits' as const)
  $: rotate = props.rotate === 90 ? (90 as const) : (0 as const)
  $: allowed = Array.isArray(props.dirs) ? ALL.filter((d) => (props.dirs as unknown[]).includes(d)) : ALL

  let driver: DpadDriver
  let lit: Dir[] = []
  $: rebuild(id, mode, allowed.join(), rotate)

  function rebuild(..._: unknown[]) {
    driver?.reset()
    driver = new DpadDriver(sink, id, mode, allowed, rotate)
    lit = []
  }

  // Which on-screen arm to light for a direction the driver reports (undo the rotation).
  const SCREEN: Record<Dir, Dir> = { right: 'up', up: 'left', left: 'down', down: 'right' }
  $: screenLit = rotate === 90 ? lit.map((d) => SCREEN[d]) : lit

  function down(e: PointerEvent) {
    capture(e)
    const p = localPoint(e.currentTarget as Element, e)
    driver.down(e.pointerId, p.nx, p.ny)
    lit = [...driver.current]
  }
  function move(e: PointerEvent) {
    const p = localPoint(e.currentTarget as Element, e)
    driver.move(e.pointerId, p.nx, p.ny)
    lit = [...driver.current]
  }
  function up(e: PointerEvent) {
    driver.up(e.pointerId)
    lit = [...driver.current]
  }

  onDestroy(() => driver?.reset())
</script>

<div
  class="dpad"
  {style}
  role="group"
  aria-label="Direction pad"
  on:pointerdown|preventDefault={down}
  on:pointermove={move}
  on:pointerup={up}
  on:pointercancel={up}
  on:lostpointercapture={up}
>
  {#each ALL as d}
    <span class="arm {d}" class:lit={screenLit.includes(d)} class:off={rotate === 0 && !allowed.includes(d)}></span>
  {/each}
  <span class="hub"></span>
</div>

<style>
  .dpad {
    position: relative;
    width: min(100%, 200px);
    max-height: 100%;
    aspect-ratio: 1;
    align-self: center;
    margin: 0 auto;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    flex: 0 1 auto;
  }
  .arm,
  .hub {
    position: absolute;
    background: var(--surface-2);
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.4), 0 0 0 1px var(--line) inset;
    pointer-events: none;
    transition: background 60ms, transform 60ms;
  }
  .hub {
    left: 33.3%;
    top: 33.3%;
    width: 33.4%;
    height: 33.4%;
    box-shadow: none;
  }
  .arm.up,
  .arm.down {
    left: 33.3%;
    width: 33.4%;
    height: 36%;
  }
  .arm.left,
  .arm.right {
    top: 33.3%;
    height: 33.4%;
    width: 36%;
  }
  .arm.up {
    top: 0;
    border-radius: var(--yui-radius-card) var(--yui-radius-card) 0 0;
  }
  .arm.down {
    bottom: 0;
    border-radius: 0 0 var(--yui-radius-card) var(--yui-radius-card);
  }
  .arm.left {
    left: 0;
    border-radius: var(--yui-radius-card) 0 0 var(--yui-radius-card);
  }
  .arm.right {
    right: 0;
    border-radius: 0 var(--yui-radius-card) var(--yui-radius-card) 0;
  }
  .arm::after {
    content: '';
    position: absolute;
    inset: 0;
    margin: auto;
    width: 0;
    height: 0;
    border: 9px solid transparent;
  }
  .arm.up::after {
    border-bottom-color: var(--muted);
    border-top-width: 0;
  }
  .arm.down::after {
    border-top-color: var(--muted);
    border-bottom-width: 0;
  }
  .arm.left::after {
    border-right-color: var(--muted);
    border-left-width: 0;
  }
  .arm.right::after {
    border-left-color: var(--muted);
    border-right-width: 0;
  }
  .arm.lit {
    background: var(--player);
    transform: scale(0.96);
  }
  .arm.off {
    opacity: 0.25;
  }
</style>
