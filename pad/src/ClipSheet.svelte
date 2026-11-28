<script lang="ts">
  import { onDestroy } from 'svelte'
  import type { ClipLayout } from '@yui/protocol'
  import Icon from './Icon.svelte'
  import { canShareFile } from './platform/snap'
  import { CLIP_LENGTHS, clipFileName, defaultClipLength, type ClipReadyInfo, type ClipVideo } from './platform/clip'

  /**
   * What the sheet shows: the options for a pinned moment, the wait while the
   * kiosk makes the video, the transfer, or the finished clip. null hides it.
   */
  export let view: { stage: 'options'; info: ClipReadyInfo } | { stage: 'making' } | { stage: 'receiving'; progress: number } | { stage: 'done'; clip: ClipVideo } | null =
    null
  export let onMake: (opts: { secs: number; camera: boolean; layout: ClipLayout }) => void = () => {}
  export let onClose: () => void = () => {}

  // ------------------------------------------------------------ options
  let secs = 20
  let camera = true
  let layout: ClipLayout = 'wide'
  let optionsFor: ClipReadyInfo | null = null
  $: if (view?.stage === 'options' && view.info !== optionsFor) {
    optionsFor = view.info
    secs = defaultClipLength(view.info.secs)
    camera = view.info.camera
  }
  $: available = view?.stage === 'options' ? view.info.secs : 0
  // When less than the shortest choice is recorded, offer just what there is.
  $: lengths = available > 0 && available < CLIP_LENGTHS[0] ? [available] : [...CLIP_LENGTHS]

  function make() {
    if (view?.stage !== 'options') return
    onMake({ secs: Math.min(secs, view.info.secs), camera: camera && view.info.camera, layout })
  }

  // ---------------------------------------------------------- finished clip
  let name = clipFileName()
  let url: string | null = null
  let shownClip: ClipVideo | null = null
  let canShare = false
  let shareBusy = false
  $: if (view?.stage === 'done' && view.clip !== shownClip) showClip(view.clip)
  $: if (view?.stage !== 'done' && shownClip) showClip(null)

  function showClip(clip: ClipVideo | null) {
    if (url) URL.revokeObjectURL(url)
    url = null
    shownClip = clip
    if (!clip) return
    name = clipFileName()
    url = URL.createObjectURL(clip.blob)
    canShare = canShareFile(new File([clip.blob], name, { type: 'video/mp4' }))
  }
  onDestroy(() => showClip(null))

  function save() {
    if (!shownClip || !url) return
    const a = document.createElement('a')
    a.href = url
    a.download = name
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  async function share() {
    if (!shownClip || shareBusy) return
    shareBusy = true
    try {
      await navigator.share({ files: [new File([shownClip.blob], name, { type: 'video/mp4' })], title: 'Yui' })
    } catch {
      /* cancelled */
    }
    shareBusy = false
  }

  $: busy = view?.stage === 'making' || view?.stage === 'receiving'
  $: pct = view?.stage === 'receiving' ? Math.round(Math.max(0, Math.min(1, view.progress)) * 100) : 0
</script>

{#if view}
  <div class="scrim" role="presentation" on:click|self={() => !busy && onClose()}>
    <div class="card kit-panel" role="dialog" aria-modal="true" aria-label="Clip the last moments">
      {#if view.stage === 'options'}
        <h2>Clip that!</h2>
        <section>
          <h3>Length</h3>
          <div class="tabs" style:grid-template-columns={`repeat(${lengths.length}, 1fr)`}>
            {#each lengths as n}
              <button class="tab" class:on={secs === n} type="button" disabled={n > available} aria-pressed={secs === n} on:click={() => (secs = n)}
                >{n}s</button
              >
            {/each}
          </div>
        </section>
        <section>
          <h3>Shape</h3>
          <div class="tabs two">
            <button class="tab" class:on={layout === 'wide'} type="button" aria-pressed={layout === 'wide'} on:click={() => (layout = 'wide')}>
              <span class="shape wide" aria-hidden="true"></span> Wide
            </button>
            <button class="tab" class:on={layout === 'tall'} type="button" aria-pressed={layout === 'tall'} on:click={() => (layout = 'tall')}>
              <span class="shape tall" aria-hidden="true"></span> Tall
            </button>
          </div>
        </section>
        {#if view.info.camera}
          <label class="row-switch">
            <span>
              <b>Facecam</b>
              <small>Add the kiosk camera as a bubble</small>
            </span>
            <input class="set-switch" type="checkbox" role="switch" bind:checked={camera} />
          </label>
        {/if}
        <button class="primary big" type="button" on:click={make}>Make clip</button>
        <button class="close" type="button" on:click={onClose}><Icon name="cross" size={16} /> Cancel</button>
      {:else if view.stage === 'making' || view.stage === 'receiving'}
        <div class="wait" role="status">
          {#if view.stage === 'making'}
            <span class="spin" aria-hidden="true"></span>
            <p>Making your clip…</p>
          {:else}
            <p>Sending it to your phone… {pct}%</p>
            <div class="bar" aria-hidden="true"><span style:width={`${pct}%`}></span></div>
          {/if}
        </div>
      {:else if view.stage === 'done' && url}
        <!-- svelte-ignore a11y-media-has-caption -->
        <video src={url} controls autoplay muted loop playsinline width={view.clip.w} height={view.clip.h}></video>
        <div class="row">
          <button class="secondary" type="button" on:click={save}>Save</button>
          {#if canShare}<button class="primary" type="button" disabled={shareBusy} on:click={share}>Share</button>{/if}
        </div>
        <button class="close" type="button" on:click={onClose}><Icon name="cross" size={16} /> Close</button>
      {/if}
    </div>
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 30;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: max(16px, env(safe-area-inset-top)) max(16px, env(safe-area-inset-right)) max(16px, env(safe-area-inset-bottom)) max(16px, env(safe-area-inset-left));
    background: var(--yui-scrim);
  }
  .card {
    width: 100%;
    max-width: 380px;
    max-height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px;
    animation: slide 260ms var(--yui-ease-out);
  }
  @keyframes slide {
    from {
      transform: translateY(60px);
      opacity: 0;
    }
  }
  h2 {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 2px 2px 0;
    font-size: 1.35rem;
    font-weight: 900;
  }
  h3 {
    margin: 0 2px 6px;
    color: var(--ink-muted);
    font-size: 0.8rem;
    font-weight: 900;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  /* Tab chips, as in the Home sheet. */
  .tabs {
    display: grid;
    gap: 8px;
  }
  .tabs.two {
    grid-template-columns: 1fr 1fr;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    min-height: 46px;
    padding: 8px 6px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: var(--kit-well);
    color: var(--ink-muted);
    font-weight: 900;
    font-size: 1rem;
  }
  .tab:disabled {
    opacity: 0.45;
  }
  .tab.on {
    --edge: var(--yui-blue-lip);
    border-color: var(--edge);
    background: linear-gradient(180deg, #4cb4f2 0 50%, var(--kit-blue) 50%);
    color: #fff;
    text-shadow: 0 2px 0 var(--edge);
    box-shadow: 0 3px 0 var(--edge);
  }
  .shape {
    display: inline-block;
    border: 2.5px solid currentColor;
    border-radius: 4px;
  }
  .shape.wide {
    width: 22px;
    height: 13px;
  }
  .shape.tall {
    width: 13px;
    height: 22px;
  }
  .row-switch {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 14px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: var(--kit-key);
  }
  .row-switch span {
    display: flex;
    flex-direction: column;
  }
  .row-switch small {
    color: var(--ink-muted);
    font-weight: 600;
  }
  .big {
    min-height: 62px;
    font-size: 1.25rem;
  }
  .wait {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 14px;
    padding: 28px 8px;
    text-align: center;
  }
  .wait p {
    margin: 0;
    font-weight: 900;
    font-size: 1.1rem;
  }
  .spin {
    width: 42px;
    height: 42px;
    border: 5px solid var(--kit-key-line);
    border-top-color: var(--kit-red);
    border-radius: 50%;
    animation: spin 800ms linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .bar {
    width: 100%;
    height: 14px;
    overflow: hidden;
    border: 2px solid var(--kit-key-line);
    border-radius: 999px;
    background: var(--kit-well);
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--kit-blue);
    transition: width 160ms linear;
  }
  video {
    display: block;
    width: 100%;
    height: auto;
    min-height: 0;
    max-height: 62vh;
    object-fit: contain;
    border: 2px solid var(--kit-panel-line);
    border-radius: var(--yui-radius-control);
    background: #000;
  }
  .row {
    display: flex;
    gap: 10px;
  }
  .row button {
    flex: 1;
    min-height: 54px;
  }
  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    min-height: 40px;
    border: 0;
    background: transparent;
    color: var(--ink-muted);
    font-weight: 800;
  }
  @media (prefers-reduced-motion: reduce) {
    .card,
    .spin {
      animation: none;
    }
  }
</style>
