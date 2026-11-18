<script lang="ts">
  import { onDestroy } from 'svelte'
  import { capture, usePad } from './context'
  import Art from './Art.svelte'

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
    <span class="plate"><Art name="button_circle_wide" mode="cap" cx={32} glow={false} /></span>
    <span class="dot"></span>
    <span class="txt">
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
    >
      <span class="plate"><Art name="button_circle_wide" mode="cap" cx={32} glow={false} /></span>
      <span class="txt">{calib ? 'Cancel' : $motion.calibrated ? 'Recalibrate' : 'Calibrate'}</span>
    </button>
    <button
      type="button"
      class="mini"
      class:pressed={pressing.size > 0}
      on:pointerdown|preventDefault={down}
      on:pointerup={up}
      on:pointercancel={up}
      on:lostpointercapture={up}
      on:contextmenu|preventDefault
    >
      <span class="plate"><Art name="button_circle_wide" mode="cap" cx={32} lit={pressing.size > 0} /></span>
      <span class="txt">Recenter</span>
    </button>
  {/if}
</div>

<style>
  .chip {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    justify-content: center;
    min-height: 44px;
    padding-bottom: 4px;
  }
  .state,
  .mini {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 44px;
    padding: 6px 20px;
    border: 0;
    background: none;
    color: var(--ink);
    font: inherit;
    font-size: 0.9rem;
    font-weight: 800;
  }
  .plate {
    position: absolute;
    inset: 0;
    filter: drop-shadow(0 3px 0 var(--k-lip));
  }
  .state {
    color: var(--ink-muted);
  }
  .state.on {
    color: var(--ink);
  }
  .dot,
  .txt {
    position: relative;
  }
  .dot {
    width: 10px;
    height: 10px;
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
    touch-action: none;
    transition: transform 50ms var(--yui-ease-out);
  }
  .mini:disabled {
    opacity: 0.6;
  }
  .note.live {
    color: var(--ink);
    font-weight: 800;
  }
  .cal {
    position: relative;
  }
  .cal .txt {
    position: relative;
    z-index: 2;
  }
  /* Fills left to right while the phone holds still. */
  .cal.running::before {
    content: '';
    position: absolute;
    inset: 0;
    z-index: 1;
    width: calc(var(--p) * 100%);
    border-radius: 999px;
    background: color-mix(in srgb, var(--kit-green), transparent 45%);
    transition: width 80ms linear;
  }
  .cal.quiet {
    color: var(--ink-muted);
    font-weight: 700;
  }
  .mini.pressed {
    --k-lip: transparent;
    --k-face: var(--player);
    --k-rim: color-mix(in srgb, var(--player), #fff 34%);
    transform: translateY(3px);
    color: #fff;
  }
</style>
