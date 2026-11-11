<script lang="ts">
  import { capture, usePad } from './context'

  export let props: Record<string, unknown>
  export let style = ''

  const pad = usePad()
  const motion = pad.motion

  $: pointer = props.pointer === true
  let pressing = new Set<number>()
  let calibrating = false
  let note = ''

  function down(e: PointerEvent) {
    capture(e)
    if (pressing.size === 0) {
      pad.sink.feedback()
      pad.recenter(true)
    }
    pressing.add(e.pointerId)
    pressing = pressing
  }
  function up(e: PointerEvent) {
    if (!pressing.delete(e.pointerId)) return
    pressing = pressing
    if (pressing.size === 0) pad.recenter(false)
  }

  async function calibrate() {
    calibrating = true
    note = 'Lay the phone flat, screen up…'
    const r = await pad.calibrate()
    calibrating = false
    note = r.ok ? 'Calibrated' : r.reason === 'flat' ? 'Lay it flat and try again' : r.reason === 'still' ? 'Keep it still and try again' : 'No motion data'
    setTimeout(() => (note = ''), 2500)
  }
</script>

<div class="chip" {style}>
  <span class="state" class:on={$motion.status === 'on'}>
    <span class="dot"></span>
    {#if $motion.status === 'on'}
      Motion on{pointer ? ' · pointer' : ''}
    {:else if $motion.status === 'denied'}
      Motion blocked
    {:else if $motion.status === 'unsupported'}
      No motion sensor
    {:else}
      Motion off
    {/if}
  </span>
  {#if note}<span class="note">{note}</span>{/if}
  {#if $motion.status === 'on'}
    {#if !$motion.calibrated}
      <button type="button" class="mini" disabled={calibrating} on:click={calibrate}>Calibrate</button>
    {/if}
    <button
      type="button"
      class="mini"
      class:pressed={pressing.size > 0}
      on:pointerdown|preventDefault={down}
      on:pointerup={up}
      on:pointercancel={up}
      on:lostpointercapture={up}
      on:contextmenu|preventDefault>Recenter</button
    >
  {/if}
</div>

<style>
  .chip {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: center;
    min-height: 44px;
  }
  .state {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
    border: 2px solid var(--kit-key-line);
    border-radius: 999px;
    background: var(--kit-key);
    color: var(--ink-muted);
    font-size: 0.9rem;
    font-weight: 800;
    box-shadow: 0 3px 0 var(--kit-key-lip);
  }
  .state.on {
    color: var(--ink);
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--kit-panel-line);
  }
  .on .dot {
    background: var(--kit-green);
    box-shadow: inset 0 -2px 0 rgba(0, 0, 0, 0.2);
  }
  .note {
    color: var(--muted);
    font-size: 0.85rem;
  }
  .mini {
    min-height: 44px;
    padding: 8px 16px;
    border: 2px solid var(--kit-key-line);
    border-radius: 999px;
    background: linear-gradient(180deg, #fff 0 50%, var(--kit-key) 50%);
    color: var(--ink);
    font-weight: 800;
    box-shadow: 0 3px 0 var(--kit-key-lip);
    touch-action: none;
  }
  .mini.pressed {
    transform: translateY(3px);
    border-color: color-mix(in srgb, var(--player), #000 26%);
    background: var(--player);
    color: #fff;
    box-shadow: none;
  }
</style>
