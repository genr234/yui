<script lang="ts">
  import type { Player } from '@yui/protocol'
  import { colorHex } from './colors'
  import Avatar from './Avatar.svelte'
  import AvatarPicker from './AvatarPicker.svelte'
  import CodeEntry from './CodeEntry.svelte'
  import { friendlyError } from './api'

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

  let confirmQuit = false
  let speakerAsked: '' | 'mono' | 'right' = ''
  // A role change from the kiosk (or a failed ask) re-enables the buttons.
  $: ((player.speaker, player.speaker_channel), (speakerAsked = ''))
  let editingAvatar = false
  let draftAvatar: string | null = null
  let savingAvatar = false
  let avatarError = ''

  function editAvatar() {
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
  <div class="sheet" role="dialog" aria-modal="true" aria-label="Home" style="--player:{colorHex(player.color)}">
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

    <button class="primary big" on:click={onResume}>{playing ? 'Resume' : 'Close'}</button>

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

    <label class="volume">
      <span>Volume</span>
      <input
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

    {#if playing}
      <button class="secondary danger" on:click={quit}>{confirmQuit ? 'Tap again to quit' : 'Quit game'}</button>
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
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
  }
  .sheet {
    width: 100%;
    max-width: 480px;
    max-height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 20px max(20px, env(safe-area-inset-right)) max(24px, env(safe-area-inset-bottom))
      max(20px, env(safe-area-inset-left));
    border-radius: var(--yui-radius-shell) var(--yui-radius-shell) 0 0;
    background: var(--yui-chrome);
    background-color: #142233;
    box-shadow: 0 -20px 60px rgba(0, 0, 0, 0.45), 0 0 0 1px var(--line) inset;
    animation: up 220ms var(--yui-ease-out);
  }
  @keyframes up {
    from {
      transform: translateY(40px);
      opacity: 0;
    }
  }
  header {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .slot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    min-width: 64px;
    padding: 10px 14px;
    border-radius: var(--yui-radius-card);
    background: var(--player);
    color: #fff;
    font-size: 1.4rem;
    font-weight: 800;
    text-align: center;
  }
  .slot.with-avatar {
    padding: 4px 14px 4px 4px;
  }
  .who {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .link {
    flex: 0 0 auto;
    border: 0;
    background: none;
    color: var(--yui-blue-soft);
    font: inherit;
    font-weight: 700;
    font-size: 0.9rem;
  }
  .avatar-edit {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px;
    border-radius: var(--yui-radius-hero);
    background: var(--yui-chrome-fill);
  }
  .avatar-actions {
    display: grid;
    grid-template-columns: 1fr 1.4fr;
    gap: 10px;
  }
  .err {
    margin: 0;
    color: #ff9a9a;
    font-weight: 600;
  }
  .who strong {
    font-size: 1.2rem;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .big {
    min-height: 72px;
    font-size: 1.3rem;
  }
  .speaker-now {
    margin: 0;
    padding: 12px 16px;
    border-radius: 14px;
    background: var(--yui-chrome-fill);
    text-align: center;
    font-weight: 700;
  }
  .volume {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    border-radius: var(--yui-radius-hero);
    background: var(--yui-chrome-fill);
    font-weight: 700;
  }
  .volume input {
    padding: 0;
    border: 0;
    background: none;
    accent-color: var(--player);
    height: 32px;
  }
  .vibration {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-radius: var(--yui-radius-hero);
    background: var(--yui-chrome-fill);
    font-weight: 700;
  }
  .vibration input {
    width: 24px;
    height: 24px;
    padding: 0;
    accent-color: var(--player);
  }
  .pct {
    min-width: 3.5em;
    text-align: right;
    color: var(--muted);
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
    color: #ff9a9a;
  }
</style>
