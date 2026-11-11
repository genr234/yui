<script lang="ts">
  import { onDestroy } from 'svelte'
  import { DpadDriver, type Dir } from './drivers'
  import { capture, localPoint, usePad } from './context'
  import Art from './Art.svelte'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()
  const ALL: Dir[] = ['up', 'down', 'left', 'right']
  // Where each arm sits in the cross art (fractions of the 128-unit file), so one
  // recoloured copy of the d-pad can be clipped to a single arm to light it.
  type Rect = [number, number, number, number]
  const ARM: Record<Dir, Rect> = {
    up: [0.305, 0, 0.695, 0.305],
    down: [0.305, 0.695, 0.695, 1],
    left: [0, 0.305, 0.305, 0.695],
    right: [0.695, 0.305, 1, 0.695],
  }
  const HUB: Rect = [0.305, 0.305, 0.695, 0.695]

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
  <span class="stack">
    {#if rotate === 90 || allowed.length === ALL.length}
      <Art name="dpad" glow={false} />
    {:else}
      <!-- Directions left out of the layout stay on the cross but dimmed. -->
      <span class="piece" class:off={false}><Art name="dpad" glow={false} part={HUB} /></span>
      {#each ALL as d}
        <span class="piece" class:off={!allowed.includes(d)}><Art name="dpad" glow={false} part={ARM[d]} /></span>
      {/each}
    {/if}
    {#each ALL as d}
      {#if screenLit.includes(d)}
        <span class="piece k-player"><Art name="dpad" part={ARM[d]} lit /></span>
      {/if}
    {/each}
  </span>
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
  /* The pack's cross on a lip; a pressed arm is a copy of the art in the player's colour with the glow outline. */
  .stack {
    position: absolute;
    inset: 0;
    filter: drop-shadow(0 var(--yui-lip) 0 var(--k-lip));
    pointer-events: none;
  }
  .piece {
    position: absolute;
    inset: 0;
  }
  .piece.off {
    opacity: 0.35;
  }
</style>
