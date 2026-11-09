<script lang="ts">
  import { onMount } from 'svelte'
  import type { Node, Player } from '@yui/protocol'
  import { ApiError, codeFromPath, friendlyError, getInfo, join, linkAccount, qrTokenFromQuery, simSlotFromQuery, type SessionInfo } from './api'
  import { COLOR_NAMES, colorHex } from './colors'
  import Pad from './Pad.svelte'
  import AvatarPicker from './AvatarPicker.svelte'
  import CodeEntry from './CodeEntry.svelte'
  import { AVATAR_EMOJIS } from './avatar'
  import { demoGamepad, demoRemote, demoShell, demoTree } from './layout/demo'
  import { unlockAudio } from './platform/sound'

  type Screen = 'loading' | 'not_found' | 'join' | 'joined' | 'error' | 'kicked' | 'ended' | 'closed'

  const demoName = import.meta.env.DEV ? new URLSearchParams(location.search).get('demo') : null
  const demos: Record<string, Node> = { layout: demoTree, gamepad: demoGamepad, remote: demoRemote, shell: demoShell }
  const demo: Node | null = (demoName && demos[demoName]) || null
  const code = codeFromPath()
  const qrToken = qrTokenFromQuery()
  const simSlot = simSlotFromQuery()

  let screen: Screen = 'loading'
  let info: SessionInfo | null = null
  let player: Player | null = null
  let name = (() => {
    if (simSlot) return `Sim ${simSlot}`
    try {
      return localStorage.getItem('yui_pad_name') ?? ''
    } catch {
      return ''
    }
  })()
  let color = ''
  // A remembered emoji (selfies are never stored on the phone).
  let avatar: string | null = (() => {
    if (simSlot) return null
    try {
      const saved = localStorage.getItem('yui_pad_avatar')
      return saved && (AVATAR_EMOJIS as readonly string[]).includes(saved) ? saved : null
    } catch {
      return null
    }
  })()
  let busy = false
  // Plan 10: play as the phone's linked account (on by default), or link now with a phone code.
  let useAccountOn = true
  let enteringCode = false
  let pendingCode = ''
  let accountName: string | null = null
  let error = ''
  let log: string[] = []

  $: taken = new Set(info?.colors_taken ?? [])
  $: if (!color || taken.has(color)) color = COLOR_NAMES.find((c) => !taken.has(c)) ?? ''
  $: canJoin = name.trim().length > 0 && !!color && !busy

  async function load() {
    if (!code) {
      screen = 'not_found'
      return
    }
    screen = 'loading'
    try {
      info = await getInfo(code)
      player = info.player
      accountName = info.account_name ?? null
      screen = player ? 'joined' : 'join'
      if (!player && simSlot) await submit()
    } catch (e) {
      screen = e instanceof ApiError && e.status === 404 ? 'not_found' : 'error'
    }
  }

  async function submit() {
    if (!code || !canJoin) return
    busy = true
    error = ''
    unlockAudio()
    try {
      const res = await join(code, name.trim(), color, qrToken, avatar, accountName ? useAccountOn : undefined)
      player = res.player
      if (pendingCode) {
        // Linking needs a player, so it happens right after joining.
        const link = pendingCode
        pendingCode = ''
        try {
          const acc = await linkAccount(code, link)
          accountName = acc.account_name
          player = { ...player, account_linked: acc.account_linked }
        } catch (e) {
          error = friendlyError(e)
          return
        }
      }
      if (!simSlot) {
        try {
          localStorage.setItem('yui_pad_name', name.trim())
          if (avatar && (AVATAR_EMOJIS as readonly string[]).includes(avatar)) localStorage.setItem('yui_pad_avatar', avatar)
          else localStorage.removeItem('yui_pad_avatar')
        } catch {
          /* storage unavailable */
        }
      }
      screen = 'joined'
    } catch (e) {
      error = friendlyError(e)
      if (e instanceof ApiError && e.code === 'kicked') screen = 'kicked'
      else if (e instanceof ApiError && e.status === 404) screen = 'not_found'
      else {
        if (e instanceof ApiError && (e.code === 'validation' || e.code === 'validation_failed')) await load().catch(() => {})
        if (!player) screen = 'join'
      }
    } finally {
      busy = false
    }
  }

  onMount(() => {
    if (!demo) void load()
  })
</script>

{#if demo}
  <div class="demo">
    <Pad
      code="DEMO"
      player={{ id: '0', slot: 1, name: 'Demo', color: 'blue', host: true, connected: true }}
      demoTree={demo}
      onDemoEvent={(line) => (log = [line, ...log].slice(0, 4))}
    />
    <pre class="log">{log.join('\n') || 'no events yet'}</pre>
  </div>
{:else if screen === 'joined' && player && code}
  <Pad {code} {player} {accountName} kioskName={info?.kiosk_name ?? ''} onEnd={(s) => (screen = s)} />
{:else}
  <main class="shell">
    {#if screen === 'loading'}
      <section class="center">
        <div class="spinner" aria-label="Loading"></div>
      </section>
    {:else if screen === 'not_found' || screen === 'ended'}
      <section class="center">
        <div class="glyph" aria-hidden="true">{screen === 'ended' ? '✓' : '?'}</div>
        <h1>{screen === 'ended' ? 'Thanks for playing' : 'Session not found'}</h1>
        <p class="muted">
          {screen === 'ended'
            ? 'The kiosk ended this session.'
            : 'This session has ended or the link is wrong. Scan the QR code on the kiosk again.'}
        </p>
      </section>
    {:else if screen === 'kicked'}
      <section class="center">
        <div class="glyph" aria-hidden="true">✕</div>
        <h1>You were removed</h1>
        <p class="muted">The host removed you from this session. Ask them to let you back in, then scan the QR code again.</p>
      </section>
    {:else if screen === 'closed'}
      <section class="center">
        <h1>Disconnected</h1>
        <p class="muted">The kiosk closed the connection.</p>
        <button class="primary" on:click={load}>Try again</button>
      </section>
    {:else if screen === 'error'}
      <section class="center">
        <h1>Can't reach the kiosk</h1>
        <p class="muted">Check your connection and try again.</p>
        <button class="primary" on:click={load}>Retry</button>
      </section>
    {:else if screen === 'join'}
      <form class="join" on:submit|preventDefault={submit}>
        <p class="eyebrow">Yui Pad</p>
        <h1>Join {info?.kiosk_name ?? 'the game'}</h1>
        <p class="muted">Pick a name, a colour and an avatar.</p>

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

        <div class="field" style="--player:{colorHex(color)}">
          <span>Avatar <small class="muted">optional</small></span>
          <AvatarPicker bind:value={avatar} disabled={busy} />
        </div>

        {#if accountName}
          <label class="account-switch">
            <span><strong>Play as {accountName}</strong><small class="muted">Your progress can follow you to any kiosk.</small></span>
            <input type="checkbox" role="switch" bind:checked={useAccountOn} disabled={busy} />
          </label>
        {:else if pendingCode}
          <p class="account-note">
            <span>Phone code ready. You'll be linked when you join.</span>
            <button class="link" type="button" on:click={() => (pendingCode = '')}>Remove</button>
          </p>
        {:else if !enteringCode}
          <button class="link account-link" type="button" on:click={() => (enteringCode = true)}>Use my Yui account</button>
        {/if}

        {#if error}<p class="err" role="alert">{error}</p>{/if}

        <button class="primary" type="submit" disabled={!canJoin}>{busy ? 'Joining…' : 'Join'}</button>
      </form>
    {/if}
    {#if screen === 'join' && enteringCode}
      <div class="scrim" role="presentation" on:click|self={() => (enteringCode = false)}>
        <div class="sheet" role="dialog" aria-modal="true" aria-label="Use my Yui account">
          <h2>Use my Yui account</h2>
          <CodeEntry
            submitLabel="Use this code"
            onCancel={() => (enteringCode = false)}
            onSubmit={(c) => {
              pendingCode = c
              enteringCode = false
              error = ''
            }}
          />
        </div>
      </div>
    {/if}
  </main>
{/if}

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
  .eyebrow {
    margin: 0;
    color: var(--yui-blue-soft);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    font-size: 0.8rem;
  }
  .glyph {
    font-size: 3rem;
    font-weight: 800;
    width: 96px;
    height: 96px;
    display: grid;
    place-items: center;
    border-radius: var(--yui-radius-shell);
    background: var(--yui-chrome-fill);
    box-shadow: 0 0 0 1px var(--line) inset;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-weight: 700;
  }
  .field small {
    font-weight: 600;
  }
  .chips {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
  }
  .chip {
    aspect-ratio: 1;
    border: 3px solid transparent;
    border-radius: var(--yui-radius-hero);
    background: var(--chip);
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.35);
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
    border-radius: var(--yui-radius-card);
    background: rgba(216, 74, 74, 0.16);
    color: #ff9a9a;
    font-weight: 600;
  }
  .account-switch {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
    min-height: 56px;
    padding: 10px 14px;
    border-radius: var(--yui-radius-hero);
    background: var(--yui-chrome-fill);
  }
  .account-switch span {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .account-switch small {
    font-weight: 600;
  }
  .account-switch input {
    flex: 0 0 auto;
    width: 52px;
    height: 32px;
    min-height: 0;
    padding: 0;
    margin: 0;
    accent-color: var(--accent);
  }
  .account-note {
    margin: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    color: var(--muted);
    font-weight: 600;
  }
  .link {
    flex: 0 0 auto;
    min-height: 48px;
    padding: 0 12px;
    border: 0;
    background: none;
    color: var(--yui-blue-soft);
    font: inherit;
    font-weight: 700;
  }
  .account-link {
    align-self: center;
    font-size: 0.95rem;
  }
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 20;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    background: var(--yui-scrim);
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
  }
  .sheet {
    width: 100%;
    max-width: 480px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 20px max(20px, env(safe-area-inset-right)) max(24px, env(safe-area-inset-bottom))
      max(20px, env(safe-area-inset-left));
    border-radius: var(--yui-radius-shell) var(--yui-radius-shell) 0 0;
    background-color: #142233;
    box-shadow: 0 -20px 60px rgba(0, 0, 0, 0.45), 0 0 0 1px var(--line) inset;
  }
  .sheet h2 {
    margin: 0;
  }
  .demo {
    position: relative;
  }
  .log {
    position: fixed;
    left: 8px;
    right: 8px;
    bottom: 8px;
    z-index: 30;
    margin: 0;
    padding: 8px 12px;
    border-radius: var(--yui-radius-control);
    background: rgba(0, 0, 0, 0.7);
    color: var(--muted);
    font-size: 0.7rem;
    pointer-events: none;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .spinner {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    border: 6px solid var(--yui-chrome-fill);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
