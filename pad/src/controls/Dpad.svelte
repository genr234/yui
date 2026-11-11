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
    height: min(100%, 210px);
    min-height: 120px;
    width: auto;
    max-width: 100%;
    aspect-ratio: 1;
    align-self: center;
    margin: 0 auto;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    flex: 0 1 auto;
  }
  /* One white cross on a lip; arms light up in the player's colour. */
  .arm,
  .hub {
    position: absolute;
    background: linear-gradient(180deg, #fff, var(--kit-key));
    border: 2px solid var(--kit-key-line);
    box-shadow: 0 var(--yui-lip) 0 var(--kit-key-lip);
    pointer-events: none;
    transition: background 50ms, transform 50ms, box-shadow 50ms;
  }
  .hub {
    left: 33.3%;
    top: 33.3%;
    width: 33.4%;
    height: 33.4%;
    border: 0;
    background: var(--kit-key);
    box-shadow: 0 var(--yui-lip) 0 var(--kit-key-lip);
    z-index: 1;
  }
  .hub::after {
    content: '';
    position: absolute;
    inset: 26%;
    border-radius: 50%;
    background: var(--kit-well);
    box-shadow: inset 0 2px 0 rgba(0, 0, 0, 0.1);
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
    border-bottom: 0;
    border-radius: var(--yui-radius-hero) var(--yui-radius-hero) 0 0;
    box-shadow: none;
  }
  .arm.down {
    bottom: 0;
    border-top: 0;
    border-radius: 0 0 var(--yui-radius-hero) var(--yui-radius-hero);
  }
  .arm.left {
    left: 0;
    border-right: 0;
    border-radius: var(--yui-radius-hero) 0 0 var(--yui-radius-hero);
  }
  .arm.right {
    right: 0;
    border-left: 0;
    border-radius: 0 var(--yui-radius-hero) var(--yui-radius-hero) 0;
  }
  .arm::after {
    content: '';
    position: absolute;
    inset: 0;
    margin: auto;
    width: 0;
    height: 0;
    border: 11px solid transparent;
  }
  .arm.up::after {
    border-bottom-color: var(--kit-blue);
    border-top-width: 0;
  }
  .arm.down::after {
    border-top-color: var(--kit-blue);
    border-bottom-width: 0;
  }
  .arm.left::after {
    border-right-color: var(--kit-blue);
    border-left-width: 0;
  }
  .arm.right::after {
    border-left-color: var(--kit-blue);
    border-right-width: 0;
  }
  .arm.lit {
    background: var(--player);
    border-color: color-mix(in srgb, var(--player), #000 26%);
  }
  .arm.lit::after {
    filter: brightness(0) invert(1);
  }
  .arm.off {
    opacity: 0.35;
  }
</style>
