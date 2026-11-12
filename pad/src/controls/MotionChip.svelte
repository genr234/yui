<script lang="ts">
  import { onDestroy } from 'svelte'
  import { capture, usePad } from './context'

  export let props: Record<string, unknown>
  export let style = ''

  const pad = usePad()
  const motion = pad.motion

  $: pointer = props.pointer === true
  let pressing = new Set<number>()
  let note = ''
  let noteTimer: ReturnType<typeof setTimeout> | undefined
  let cancelled = false

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

  $: calib = $motion.calib
  $: hint = calib ? (calib.phase === 'flat' ? 'Lay the phone flat, screen up' : calib.phase === 'still' ? 'Keep it still…' : 'Hold still…') : ''

  // Calibrating once motion is on, and announcing the unprompted calibration.
  let wasCalibrated = $motion.calibrated
  $: if ($motion.calibrated !== wasCalibrated) {
    if ($motion.calibrated && !calib) flash('Calibrated')
    wasCalibrated = $motion.calibrated
  }

  function flash(text: string) {
    note = text
    clearTimeout(noteTimer)
    noteTimer = setTimeout(() => (note = ''), 2500)
  }

  async function calibrate() {
    if (calib) {
      cancelled = true
      return pad.cancelCalibration()
    }
    pad.sink.feedback()
    note = ''
    cancelled = false
    const r = await pad.calibrate()
    if (cancelled) return
    if (r.ok) {
      pad.sink.feedback()
      flash('Calibrated')
    } else flash(r.reason === 'flat' ? "Couldn't see it lying flat" : r.reason === 'still' ? 'It kept moving' : 'No motion data')
  }

  onDestroy(() => {
    clearTimeout(noteTimer)
    cancelled = true
    pad.cancelCalibration()
  })
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
  {#if hint}<span class="note live">{hint}</span>{:else if note}<span class="note">{note}</span>{/if}
  {#if $motion.status === 'on'}
    <button
      type="button"
      class="mini cal"
      class:quiet={$motion.calibrated && !calib}
      class:running={!!calib}
      style="--p: {calib ? calib.progress : 0}"
      aria-label={calib ? 'Cancel calibration' : $motion.calibrated ? 'Recalibrate' : 'Calibrate'}
      on:click={calibrate}
      ><span>{calib ? 'Cancel' : $motion.calibrated ? 'Recalibrate' : 'Calibrate'}</span></button
    >
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
  .note.live {
    color: var(--ink);
    font-weight: 800;
  }
  .cal {
    position: relative;
    overflow: hidden;
  }
  .cal span {
    position: relative;
  }
  /* Fills left to right while the phone holds still. */
  .cal.running::before {
    content: '';
    position: absolute;
    inset: 0;
    width: calc(var(--p) * 100%);
    background: color-mix(in srgb, var(--kit-green), transparent 45%);
    transition: width 80ms linear;
  }
  .cal.quiet {
    color: var(--ink-muted);
    font-weight: 700;
  }
  .mini.pressed {
    transform: translateY(3px);
    border-color: color-mix(in srgb, var(--player), #000 26%);
    background: var(--player);
    color: #fff;
    box-shadow: none;
  }
</style>
