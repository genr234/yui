<script lang="ts">
  /**
   * /play with no code: where the installed app opens. Rejoin the last game,
   * scan the kiosk's QR code, or type the four-letter code.
   */
  import Icon from './Icon.svelte'
  import InstallHint from './InstallHint.svelte'
  import QrScan from './QrScan.svelte'
  import { canScan, cleanCode, joinTarget, lastCode } from './platform/pwa'

  /** Navigates the app to a /play/<CODE> path (location.assign in the app). */
  export let go: (path: string) => void

  const last = lastCode()
  const scanner = typeof window !== 'undefined' && canScan()
  let typed = ''
  let scanning = false
  let error = ''

  $: code = cleanCode(typed)

  function submit() {
    if (code) go(`/play/${code}`)
  }

  function scanned(text: string) {
    const target = joinTarget(text)
    scanning = false
    if (target) go(target)
    else error = "That QR code isn't a Yui game. Scan the \"Scan to play\" code on the kiosk."
  }
</script>

<section class="home">
  <p class="eyebrow">Yui Pad</p>
  <h1>Join a game</h1>

  {#if last}
    <button class="primary" type="button" on:click={() => go(`/play/${last}`)}>
      <Icon name="play" size={22} /> Rejoin {last}
    </button>
  {/if}

  {#if scanner}
    <button class={last ? 'secondary' : 'primary'} type="button" on:click={() => ((error = ''), (scanning = true))}>
      <Icon name="crosshair" size={22} /> Scan the kiosk's QR code
    </button>
  {/if}

  <form class="card kit-panel" on:submit|preventDefault={submit}>
    <label class="field">
      <span>Game code</span>
      <input
        class="code"
        bind:value={typed}
        maxlength="4"
        autocapitalize="characters"
        autocomplete="off"
        spellcheck="false"
        placeholder="ABCD"
        aria-label="Four-letter game code"
      />
    </label>
    <p class="muted hint">The four letters on the kiosk, under its QR code.</p>
    <button class="secondary" type="submit" disabled={!code}>Join with code</button>
  </form>
  {#if error}<p class="err" role="alert">{error}</p>{/if}

  <InstallHint />
</section>

{#if scanning}
  <QrScan onResult={scanned} onCancel={() => (scanning = false)} />
{/if}

<style>
  .home {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    justify-content: center;
    gap: 14px;
    width: 100%;
    max-width: 420px;
    margin: 0 auto;
    padding: 24px 20px;
    text-align: center;
  }
  h1 {
    margin: 0 0 6px;
  }
  .eyebrow {
    align-self: center;
    margin: 0;
    padding: 6px 14px;
    border: 2px solid var(--yui-blue-lip);
    border-radius: var(--yui-radius-control);
    background: linear-gradient(180deg, #4cb4f2 0 50%, var(--kit-blue) 50%);
    color: #fff;
    font-weight: 800;
    font-size: 0.85rem;
    letter-spacing: 0.04em;
    box-shadow: 0 3px 0 var(--yui-blue-lip);
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }
  .card {
    display: grid;
    gap: 10px;
    padding: 14px;
    color: var(--ink);
    text-align: left;
  }
  .field {
    display: grid;
    gap: 6px;
    font-weight: 800;
  }
  .code {
    font-size: 1.6rem;
    letter-spacing: 0.3em;
    text-transform: uppercase;
    text-align: center;
  }
  .hint {
    margin: 0;
    color: var(--ink-muted);
    font-size: 0.9rem;
  }
  .err {
    margin: 0;
    color: var(--kit-yellow);
    font-weight: 800;
  }
</style>
