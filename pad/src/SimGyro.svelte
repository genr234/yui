<script lang="ts">
  import type { SimMotionSource } from './platform/motion'

  /** /sim only: drag with the mouse to turn the "phone" (fake gyro). */
  export let source: SimMotionSource

  let dragging = false
  let last: { x: number; y: number; t: number } | null = null

  function down(e: PointerEvent) {
    ;(e.currentTarget as Element).setPointerCapture(e.pointerId)
    dragging = true
    last = { x: e.clientX, y: e.clientY, t: performance.now() }
  }
  function move(e: PointerEvent) {
    if (!dragging || !last) return
    const t = performance.now()
    source.drag(e.clientX - last.x, e.clientY - last.y, t - last.t)
    last = { x: e.clientX, y: e.clientY, t }
  }
  function up() {
    dragging = false
    last = null
    source.release()
  }
</script>

<div class="sim">
  <div
    class="area"
    class:dragging
    role="presentation"
    on:pointerdown|preventDefault={down}
    on:pointermove={move}
    on:pointerup={up}
    on:pointercancel={up}
  >
    Drag to aim (fake gyro)
  </div>
  <button type="button" class="secondary" on:click={() => source.shake()}>Swing</button>
</div>

<style>
  .sim {
    display: flex;
    gap: 8px;
    padding: 0 14px 14px;
  }
  .area {
    flex: 1;
    display: grid;
    place-items: center;
    min-height: 56px;
    border-radius: var(--yui-radius-card);
    border: 2px dashed var(--line);
    color: var(--muted);
    font-size: 0.85rem;
    cursor: grab;
    touch-action: none;
  }
  .area.dragging {
    border-color: var(--player);
    cursor: grabbing;
  }
  .secondary {
    min-height: 56px;
    padding: 0 16px;
  }
</style>
