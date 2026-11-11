<script lang="ts">
  import { onDestroy } from 'svelte'
  import Icon from './Icon.svelte'
  import { canShareFile, snapFileName } from './platform/snap'

  /** The picture to show (a data: URL for the <img>, the Blob for Save and Share), or null. */
  export let card: { url: string; blob: Blob; w?: number; h?: number } | null = null
  /** A short error to show for a moment; '' hides it. */
  export let toast = ''
  /** Changes every time a snap is taken: plays the white flash. */
  export let flash = 0
  export let onClose: () => void = () => {}

  let name = snapFileName()
  let canShare = false
  let shareBusy = false
  $: if (card) {
    name = snapFileName()
    canShare = canShareFile(new File([card.blob], name, { type: 'image/jpeg' }))
  }

  let objectUrl: string | null = null
  function revoke() {
    if (objectUrl) URL.revokeObjectURL(objectUrl)
    objectUrl = null
  }
  $: if (!card) revoke()
  onDestroy(revoke)

  function save() {
    if (!card) return
    revoke()
    objectUrl = URL.createObjectURL(card.blob)
    const a = document.createElement('a')
    a.href = objectUrl
    a.download = name
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  async function share() {
    if (!card || shareBusy) return
    shareBusy = true
    try {
      await navigator.share({ files: [new File([card.blob], name, { type: 'image/jpeg' })], title: 'Yui' })
    } catch {
      /* cancelled */
    }
    shareBusy = false
  }
</script>

{#key flash}
  {#if flash > 0}<div class="flash" aria-hidden="true"></div>{/if}
{/key}

{#if card}
  <div class="scrim" role="presentation" on:click|self={onClose}>
    <div class="card kit-panel" role="dialog" aria-modal="true" aria-label="Screen snapshot">
      <img src={card.url} alt="The screen" width={card.w} height={card.h} />
      <div class="row">
        <button class="secondary" type="button" on:click={save}>Save</button>
        {#if canShare}<button class="primary" type="button" disabled={shareBusy} on:click={share}>Share</button>{/if}
      </div>
      <button class="close" type="button" on:click={onClose}><Icon name="cross" size={16} /> Close</button>
    </div>
  </div>
{/if}

{#if toast}
  <div class="toast" role="status">{toast}</div>
{/if}

<style>
  .flash {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: #fff;
    pointer-events: none;
    animation: flash 360ms ease-out forwards;
  }
  @keyframes flash {
    from {
      opacity: 0.9;
    }
    to {
      opacity: 0;
    }
  }
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
  img {
    display: block;
    width: 100%;
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
  .toast {
    position: fixed;
    z-index: 50;
    left: 50%;
    bottom: max(24px, env(safe-area-inset-bottom));
    max-width: min(90vw, 360px);
    transform: translateX(-50%);
    padding: 10px 16px;
    border: 2px solid var(--kit-panel-line);
    border-radius: var(--yui-radius-hero);
    background: #fff;
    box-shadow: 0 4px 0 var(--kit-panel-lip);
    color: var(--ink);
    font-weight: 800;
    text-align: center;
    animation: pop 200ms var(--yui-ease-out);
  }
  @keyframes pop {
    from {
      transform: translateX(-50%) translateY(16px);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .card,
    .toast {
      animation: none;
    }
  }
</style>
