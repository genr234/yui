<script lang="ts">
  import { onMount } from 'svelte'
  import type { Player } from '@yui/protocol'
  import { ApiError, codeFromPath, friendlyError, getInfo, join, qrTokenFromQuery, type SessionInfo } from './api'
  import { COLOR_NAMES, colorHex } from './colors'
  import LayoutView from './layout/LayoutView.svelte'
  import { demoTree } from './layout/demo'
  import { keepAwake } from './platform/wakelock'
  import { requestMotionPermission, type MotionPermission } from './platform/motion'

  type Screen = 'loading' | 'not_found' | 'join' | 'joined' | 'error'

  const demo = import.meta.env.DEV && new URLSearchParams(location.search).get('demo') === 'layout'
  const code = codeFromPath()
  const qrToken = qrTokenFromQuery()

  let screen: Screen = 'loading'
  let info: SessionInfo | null = null
  let player: Player | null = null
  let name = (() => {
    try {
      return localStorage.getItem('yui_pad_name') ?? ''
    } catch {
      return ''
    }
  })()
  let color = ''
  let busy = false
  let error = ''
  let motion: MotionPermission | null = null
  let log: string[] = []

  $: taken = new Set(info?.colors_taken ?? [])
  $: if (!color || taken.has(color)) color = COLOR_NAMES.find((c) => !taken.has(c)) ?? ''
  $: canJoin = name.trim().length > 0 && !!color && !busy

  async function load() {
    if (!code) {
      screen = 'not_found'
      return
    }
    try {
      info = await getInfo(code)
      player = info.player
      screen = player ? 'joined' : 'join'
      if (player) void keepAwake()
    } catch (e) {
      screen = e instanceof ApiError && e.status === 404 ? 'not_found' : 'error'
    }
  }

  async function submit() {
    if (!code || !canJoin) return
    busy = true
    error = ''
    try {
      const res = await join(code, name.trim(), color, qrToken)
      player = res.player
      try {
        localStorage.setItem('yui_pad_name', name.trim())
      } catch {
        /* storage unavailable */
      }
      screen = 'joined'
      void keepAwake()
      // Join is a tap, so this is a valid moment for the iOS motion prompt.
      motion = await requestMotionPermission()
    } catch (e) {
      error = friendlyError(e)
      if (e instanceof ApiError && e.code === 'validation') await load().catch(() => {})
      if (screen !== 'join' && !player) screen = 'join'
    } finally {
      busy = false
    }
  }

  function onDemoEvent(e: { control: string; kind: string }) {
    log = [`${e.kind} ${e.control}`, ...log].slice(0, 5)
  }

  onMount(() => {
    if (!demo) void load()
  })
</script>

<main class="shell">
  {#if demo}
    <section class="demo">
      <LayoutView node={demoTree} onEvent={onDemoEvent} />
      <pre class="log">{log.join('\n') || 'no events yet'}</pre>
    </section>
  {:else if screen === 'loading'}
    <section class="center">
      <div class="spinner" aria-label="Loading"></div>
    </section>
  {:else if screen === 'not_found'}
    <section class="center">
      <div class="emoji" aria-hidden="true">?</div>
      <h1>Session not found</h1>
      <p class="muted">This session has ended or the link is wrong. Scan the QR code on the kiosk again.</p>
    </section>
  {:else if screen === 'error'}
    <section class="center">
      <h1>Can't reach the kiosk</h1>
      <p class="muted">Check your connection and try again.</p>
      <button class="primary" on:click={load}>Retry</button>
    </section>
  {:else if screen === 'join'}
    <form class="join" on:submit|preventDefault={submit}>
      <h1>Join {info?.kiosk_name ?? 'the game'}</h1>
      <p class="muted">Pick a name and a colour.</p>

      <label class="field">
        <span>Your name</span>
        <input bind:value={name} maxlength="20" autocomplete="nickname" autocapitalize="words" placeholder="Player" />
      </label>

      <div class="field">
        <span>Colour</span>
        <div class="chips" role="radiogroup" aria-label="Colour">
          {#each COLOR_NAMES as c}
            <button
              type="button"
              role="radio"
              aria-checked={color === c}
              aria-label={c}
              class="chip"
              class:selected={color === c}
              disabled={taken.has(c)}
              style="--chip:{colorHex(c)}"
              on:click={() => (color = c)}
            ></button>
          {/each}
        </div>
      </div>

      {#if error}<p class="err" role="alert">{error}</p>{/if}

      <button class="primary" type="submit" disabled={!canJoin}>{busy ? 'Joining…' : 'Join'}</button>
    </form>
  {:else if screen === 'joined' && player}
    <section class="center joined">
      {#if player.slot}
        <div class="badge" style="--chip:{colorHex(player.color)}">P{player.slot}</div>
      {:else}
        <div class="badge spectator" style="--chip:{colorHex(player.color)}">Spectator</div>
      {/if}
      <h1>{player.name}</h1>
      <p class="muted">{player.host ? "You're the host. " : ''}Waiting for the kiosk…</p>
      {#if motion === 'denied'}
        <p class="err">Motion access was denied. Motion games will not work.</p>
      {/if}
    </section>
  {/if}
</main>

<style>
  .shell {
    min-height: 100dvh;
    display: flex;
    flex-direction: column;
    padding: max(20px, env(safe-area-inset-top)) max(20px, env(safe-area-inset-right))
      max(20px, env(safe-area-inset-bottom)) max(20px, env(safe-area-inset-left));
  }
  section,
  form {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: 100%;
    max-width: 480px;
    margin: 0 auto;
  }
  .center {
    align-items: center;
    justify-content: center;
    text-align: center;
  }
  .join {
    justify-content: center;
  }
  .demo {
    justify-content: flex-start;
  }
  .emoji {
    font-size: 4rem;
    font-weight: 800;
    width: 96px;
    height: 96px;
    display: grid;
    place-items: center;
    border-radius: 32px;
    background: var(--surface-2);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-weight: 700;
  }
  .chips {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
  }
  .chip {
    aspect-ratio: 1;
    border: 4px solid transparent;
    border-radius: 22px;
    background: var(--chip);
    box-shadow: 0 5px 0 rgba(0, 0, 0, 0.35);
    padding: 0;
  }
  .chip.selected {
    border-color: #fff;
    transform: translateY(-2px);
  }
  .chip:disabled {
    opacity: 0.22;
    box-shadow: none;
  }
  .err {
    margin: 0;
    padding: 10px 14px;
    border-radius: 14px;
    background: rgba(255, 84, 112, 0.16);
    color: #ff9fb0;
    font-weight: 600;
  }
  .badge {
    min-width: 200px;
    padding: 28px 36px;
    border-radius: 48px;
    background: var(--chip);
    color: #0f1020;
    font-size: 5rem;
    font-weight: 900;
    line-height: 1;
    box-shadow: 0 10px 0 rgba(0, 0, 0, 0.35);
  }
  .badge.spectator {
    font-size: 2.2rem;
  }
  .joined h1 {
    margin: 8px 0 0;
  }
  .log {
    margin: 0;
    min-height: 5.5em;
    padding: 10px 14px;
    border-radius: 14px;
    background: var(--surface);
    color: var(--muted);
    font-size: 0.85rem;
  }
  .spinner {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    border: 6px solid var(--surface-2);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
