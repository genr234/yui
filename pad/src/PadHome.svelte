<script lang="ts">
  /**
   * /play with no code: where the installed app opens. Rejoin the last game,
   * scan the kiosk's QR code, or type the four-letter code. A code is checked
   * before the app goes there, so a typo or a finished game shows a message
   * here instead of a dead end.
   */
  import { onMount } from 'svelte'
  import { ApiError, getInfo } from './api'
  import Icon from './Icon.svelte'
  import InstallHint from './InstallHint.svelte'
  import QrScan from './QrScan.svelte'
  import { canScan, cleanCode, forgetCode, joinTarget, lastCode } from './platform/pwa'

  /** Navigates the app to a /play/<CODE> path (location.assign in the app). */
  export let go: (path: string) => void
  /** Why the app is back here, e.g. the code it was opened with has no game. */
  export let notice = ''

  const scanner = typeof window !== 'undefined' && canScan()
  // Offered only once the server says that game is still running.
  let last: string | null = null
  let typed = ''
  let scanning = false
  let checking = false
  let error = notice

  $: code = cleanCode(typed)

  onMount(() => {
    const remembered = lastCode()
    if (!remembered) return
    getInfo(remembered)
      .then(() => (last = remembered))
      .catch((e) => {
        if (e instanceof ApiError && e.status === 404) forgetCode(remembered)
        else last = remembered // offline or a hiccup: let them try
      })
  })

  async function open(target: string) {
    if (checking) return
    checking = true
    error = ''
    try {
      await getInfo(target)
      go(`/play/${target}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        forgetCode(target)
        if (last === target) last = null
        error = `There's no game with code ${target} right now. Check the four letters on the kiosk.`
      } else {
        error = "Can't reach Yui. Check your connection and try again."
      }
    } finally {
      checking = false
    }
  }

  function submit() {
    if (code) void open(code)
  }

  function scanned(text: string) {
    const target = joinTarget(text)
    scanning = false
    if (target) void open(target)
    else error = "That QR code isn't a Yui game. Scan the \"Scan to play\" code on the kiosk, or type its code."
  }
</script>

<section class="home">
  <p class="eyebrow">Yui Pad</p>
  <h1>Join a game</h1>

  {#if last}
    <button class="primary" type="button" disabled={checking} on:click={() => last && open(last)}>
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
        on:input={() => (error = '')}
        maxlength="4"
        autocapitalize="characters"
        autocomplete="off"
        spellcheck="false"
        placeholder="ABCD"
        aria-label="Four-letter game code"
      />
    </label>
    <p class="muted hint">The four letters on the kiosk, under its QR code.</p>
    {#if error}<p class="err" role="alert">{error}</p>{/if}
    <button class={last || scanner ? 'secondary' : 'primary'} type="submit" disabled={!code || checking}>
      {checking ? 'Checking…' : 'Join with code'}
    </button>
  </form>

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
    padding: 10px 12px;
    border: 2px solid #b8263a;
    border-radius: var(--yui-radius-hero);
    background: var(--kit-red);
    color: #fff;
    font-weight: 800;
    box-shadow: 0 3px 0 #b8263a;
  }
</style>
