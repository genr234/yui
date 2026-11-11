<script lang="ts">
  import type { Player } from '@yui/protocol'
  import { colorHex } from './colors'
  import Avatar from './Avatar.svelte'
  import AvatarPicker from './AvatarPicker.svelte'
  import CodeEntry from './CodeEntry.svelte'
  import { friendlyError } from './api'
  import Icon, { type IconName } from './Icon.svelte'

  export let player: Player
  export let kioskName = ''
  export let volume = 1
  export let onResume: () => void
  export let onQuit: () => void
  /** Something (an app or a game) is running on the kiosk, so Quit means something. */
  export let playing = false
  export let onVolume: (v: number) => void
  /** Take the sound here; with a channel, join the current speaker as that half of a stereo pair. */
  export let onSpeaker: (channel?: 'left' | 'right') => void
  /** Vibration on/off for this phone; null hides the switch (no haptics here). */
  export let vibration: boolean | null = null
  export let onVibration: (on: boolean) => void = () => {}
  /** Saves a new avatar (null removes it). Rejects when the kiosk refused it. */
  export let onAvatar: (avatar: string | null) => Promise<void>
  /** The Yui account linked to this phone, if any (plan 10). */
  export let accountName: string | null = null
  /** Play as the account (true) or as a guest (false) this session. Rejects on failure. */
  export let onAccountUse: (use: boolean) => Promise<void> = async () => {}
  /** Forget the link on this phone. */
  export let onAccountForget: () => Promise<void> = async () => {}
  /** Redeem a phone code. */
  export let onAccountLink: (code: string) => Promise<void> = async () => {}

  // Tabs. The last one stays selected for the rest of this browser session.
  type Tab = 'game' | 'sound' | 'you'
  const TABS: { id: Tab; label: string; icon: IconName }[] = [
    { id: 'game', label: 'Game', icon: 'play' },
    { id: 'sound', label: 'Sound', icon: 'sound' },
    { id: 'you', label: 'You', icon: 'star' },
  ]
  const TAB_KEY = 'yui_pad_home_tab'
  let lastTab: Tab = 'game'
  function loadTab(): Tab {
    try {
      const t = sessionStorage.getItem(TAB_KEY)
      if (t === 'game' || t === 'sound' || t === 'you') return t
    } catch {
      /* storage unavailable */
    }
    return lastTab
  }
  let tab: Tab = loadTab()
  function pickTab(t: Tab) {
    tab = lastTab = t
    confirmQuit = false // a half-done confirm-twice never survives a tab change
    confirmForget = false
    try {
      sessionStorage.setItem(TAB_KEY, t)
    } catch {
      /* storage unavailable */
    }
  }

  let confirmQuit = false
  let speakerAsked: '' | 'mono' | 'right' = ''
  // A role change from the kiosk (or a failed ask) re-enables the buttons.
  $: ((player.speaker, player.speaker_channel), (speakerAsked = ''))
  let editingAvatar = false
  let draftAvatar: string | null = null
  let savingAvatar = false
  let avatarError = ''

  function editAvatar() {
    pickTab('you')
    draftAvatar = player.avatar ?? null
    avatarError = ''
    editingAvatar = true
  }

  async function saveAvatar() {
    savingAvatar = true
    avatarError = ''
    try {
      await onAvatar(draftAvatar)
      editingAvatar = false
    } catch {
      avatarError = "Couldn't save that avatar. Try again."
    } finally {
      savingAvatar = false
    }
  }

  let accountBusy = false
  let accountError = ''
  let confirmForget = false
  let enteringCode = false

  $: usingAccount = player.account_linked === true
  $: label = accountName || 'your Yui account'

  async function account(fn: () => Promise<void>): Promise<boolean> {
    accountBusy = true
    accountError = ''
    try {
      await fn()
      return true
    } catch (e) {
      accountError = friendlyError(e)
      return false
    } finally {
      accountBusy = false
    }
  }

  function forget() {
    if (!confirmForget) {
      confirmForget = true
      return
    }
    confirmForget = false
    void account(onAccountForget)
  }

  async function link(code: string) {
    if (await account(() => onAccountLink(code))) enteringCode = false
  }

  function quit() {
    if (!confirmQuit) {
      confirmQuit = true
      return
    }
    confirmQuit = false
    onQuit()
  }
</script>

<div class="scrim" role="presentation" on:click|self={onResume}>
  <div class="sheet kit-panel" role="dialog" aria-modal="true" aria-label="Home" style="--player:{colorHex(player.color)}">
    <header>
      <span class="slot" class:with-avatar={!!player.avatar}
        ><Avatar avatar={player.avatar} size={40} />{player.slot ? `P${player.slot}` : 'Spectator'}</span
      >
      <div class="who">
        <strong>{player.name}</strong>
        <span class="muted">{kioskName || 'Yui'}{player.host ? ' · host' : ''}</span>
      </div>
      {#if !editingAvatar}
        <button class="link" type="button" on:click={editAvatar}>{player.avatar ? 'Change avatar' : 'Add avatar'}</button>
      {/if}
    </header>

    <div class="tabs" role="tablist" aria-label="Home sections">
      {#each TABS as t (t.id)}
        <button class="tab" class:on={tab === t.id} type="button" role="tab" aria-selected={tab === t.id} on:click={() => pickTab(t.id)}
          ><Icon name={t.icon} size={18} />{t.label}</button
        >
      {/each}
    </div>

    {#if tab === 'game'}
      <button class="primary big" on:click={onResume}>{playing ? 'Resume' : 'Close'}</button>

      <!--
        Per-feature rows go here (one row per feature, e.g. reactions, snapshot,
        game-specific options). Keep Resume/Close first and Quit game last.
      -->

      {#if playing}
        <button class="danger-btn" on:click={quit}>{confirmQuit ? 'Tap again to quit' : 'Quit game'}</button>
      {/if}
    {:else if tab === 'sound'}
      <label class="volume">
        <span>Volume</span>
        <input
          style="--fill:{Math.round(volume * 100)}%"
          type="range"
          min="0"
          max="100"
          step="1"
          value={Math.round(volume * 100)}
          on:input={(e) => onVolume(Number(e.currentTarget.value) / 100)}
        />
        <span class="pct">{Math.round(volume * 100)}%</span>
      </label>

      {#if vibration !== null}
        <label class="vibration">
          <span>Vibration</span>
          <input type="checkbox" checked={vibration} on:change={(e) => onVibration(e.currentTarget.checked)} />
        </label>
      {/if}

      {#if player.speaker}
        <p class="speaker-now">
          {player.speaker_channel ? `This phone is the ${player.speaker_channel} speaker` : 'This phone is the speaker'}
        </p>
        {#if player.speaker_channel}
          <button
            class="secondary"
            disabled={speakerAsked !== ''}
            on:click={() => {
              speakerAsked = 'mono'
              onSpeaker()
            }}>{speakerAsked ? 'Switching the sound here…' : 'Play all the sound here'}</button
          >
        {/if}
      {:else}
        <button
          class="secondary"
          disabled={speakerAsked !== ''}
          on:click={() => {
            speakerAsked = 'mono'
            onSpeaker()
          }}>{speakerAsked === 'mono' ? 'Switching the sound here…' : 'Be the speaker'}</button
        >
        <button
          class="secondary"
          disabled={speakerAsked !== ''}
          on:click={() => {
            speakerAsked = 'right'
            onSpeaker('right')
          }}>{speakerAsked === 'right' ? 'Pairing as the right speaker…' : 'Be the right speaker'}</button
        >
      {/if}
    {:else}
      {#if editingAvatar}
        <section class="avatar-edit" aria-label="Avatar">
          <AvatarPicker bind:value={draftAvatar} disabled={savingAvatar} />
          {#if avatarError}<p class="err" role="alert">{avatarError}</p>{/if}
          <div class="avatar-actions">
            <button class="secondary" type="button" disabled={savingAvatar} on:click={() => (editingAvatar = false)}>Cancel</button>
            <button class="primary" type="button" disabled={savingAvatar || draftAvatar === (player.avatar ?? null)} on:click={saveAvatar}
              >{savingAvatar ? 'Saving…' : 'Save avatar'}</button
            >
          </div>
        </section>
      {/if}

      <section class="account" aria-label="Yui account">
        {#if enteringCode}
          <CodeEntry busy={accountBusy} error={accountError} onSubmit={link} onCancel={() => ((enteringCode = false), (accountError = ''))} />
        {:else if usingAccount || accountName}
          <p class="account-line">
            <strong>{usingAccount ? `${label} · playing as your account` : `${label}`}</strong>
          </p>
          {#if usingAccount}
            <button class="secondary" type="button" disabled={accountBusy} on:click={() => account(() => onAccountUse(false))}>Stop using it here</button>
          {:else}
            <button class="secondary" type="button" disabled={accountBusy} on:click={() => account(() => onAccountUse(true))}>Play as {label}</button>
          {/if}
          <button class="secondary danger" type="button" disabled={accountBusy} on:click={forget}>{confirmForget ? 'Tap again to forget this phone' : 'Forget this phone'}</button>
        {:else}
          <button class="secondary" type="button" on:click={() => (enteringCode = true)}>Use my Yui account</button>
        {/if}
        {#if accountError && !enteringCode}<p class="err" role="alert">{accountError}</p>{/if}
      </section>

    {/if}
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 20;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    background: var(--yui-scrim);
  }
  .sheet {
    width: 100%;
    max-width: 480px;
    max-height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 0 max(16px, env(safe-area-inset-right)) max(24px, env(safe-area-inset-bottom))
      max(16px, env(safe-area-inset-left));
    border-bottom: 0;
    border-radius: var(--yui-radius-shell) var(--yui-radius-shell) 0 0;
    animation: up 220ms var(--yui-ease-out);
  }
  @keyframes up {
    from {
      transform: translateY(40px);
      opacity: 0;
    }
  }
  /* The player's coloured title strip across the top of the panel. */
  header {
    --edge: color-mix(in srgb, var(--player), #000 26%);
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 0 calc(-1 * max(16px, env(safe-area-inset-left))) 4px;
    padding: 12px max(16px, env(safe-area-inset-left));
    border-bottom: 2px solid var(--edge);
    background: linear-gradient(180deg, color-mix(in srgb, var(--player), #fff 16%) 0 50%, var(--player) 50%);
    color: #fff;
  }
  .slot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    min-width: 60px;
    padding: 8px 12px;
    border: 2px solid var(--edge);
    border-radius: var(--yui-radius-hero);
    background: #fff;
    color: var(--ink);
    font-size: 1.3rem;
    font-weight: 900;
    text-align: center;
    box-shadow: 0 3px 0 var(--edge);
  }
  .slot.with-avatar {
    padding: 3px 12px 3px 3px;
  }
  .who {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    text-shadow: 0 2px 0 var(--edge);
  }
  .who .muted {
    color: rgba(255, 255, 255, 0.88);
    font-weight: 700;
    font-size: 0.9rem;
  }
  .who strong {
    font-size: 1.2rem;
    font-weight: 900;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .link {
    flex: 0 0 auto;
    padding: 6px 10px;
    border: 2px solid var(--edge);
    border-radius: var(--yui-radius-control);
    background: rgba(255, 255, 255, 0.92);
    color: var(--ink);
    font: inherit;
    font-weight: 800;
    font-size: 0.85rem;
    box-shadow: 0 3px 0 var(--edge);
  }
  .link:active {
    transform: translateY(3px);
    box-shadow: none;
  }
  /* Tab chips, like the tabs at the top of a game options panel. */
  .tabs {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    min-height: 46px;
    padding: 8px 6px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: var(--kit-well);
    color: var(--ink-muted);
    font-weight: 900;
    font-size: 1rem;
  }
  .tab.on {
    --edge: var(--yui-blue-lip);
    border-color: var(--edge);
    background: linear-gradient(180deg, #4cb4f2 0 50%, var(--kit-blue) 50%);
    color: #fff;
    text-shadow: 0 2px 0 var(--edge);
    box-shadow: 0 3px 0 var(--edge);
  }
  .avatar-edit {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 12px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: #fff;
  }
  .avatar-actions {
    display: grid;
    grid-template-columns: 1fr 1.4fr;
    gap: 10px;
  }
  .err {
    margin: 0;
    color: var(--kit-red);
    font-weight: 800;
  }
  .big {
    min-height: 72px;
    font-size: 1.35rem;
  }
  .speaker-now {
    margin: 0;
    padding: 12px 16px;
    border: 2px dashed var(--kit-panel-line);
    border-radius: var(--yui-radius-hero);
    text-align: center;
    font-weight: 800;
  }
  /* Settings rows: a white inset row, label left, control right. */
  .volume,
  .vibration {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 12px;
    min-height: 60px;
    padding: 8px 14px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: #fff;
    font-weight: 800;
  }
  .vibration {
    grid-template-columns: 1fr auto;
  }
  .pct {
    min-width: 3.2em;
    padding: 4px 6px;
    border-radius: var(--yui-radius-control);
    background: var(--kit-well);
    text-align: center;
    color: var(--ink);
    font-size: 0.9rem;
    font-variant-numeric: tabular-nums;
  }
  .account {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .account-line {
    margin: 0;
    padding: 0 4px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .danger {
    color: var(--kit-red);
  }
</style>
