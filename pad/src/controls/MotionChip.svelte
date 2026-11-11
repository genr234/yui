<script lang="ts">
  import { capture, usePad } from './context'
  import Art from './Art.svelte'

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
  {#if note}<span class="note">{note}</span>{/if}
  {#if $motion.status === 'on'}
    {#if !$motion.calibrated}
      <button type="button" class="mini" disabled={calibrating} on:click={calibrate}>
        <span class="plate"><Art name="button_circle_wide" mode="cap" cx={32} glow={false} /></span>
        <span class="txt">Calibrate</span>
      </button>
    {/if}
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
  .mini.pressed {
    --k-lip: transparent;
    --k-face: var(--player);
    --k-rim: color-mix(in srgb, var(--player), #fff 34%);
    transform: translateY(3px);
    color: #fff;
  }
</style>
