<script lang="ts">
  import type { Player } from '@yui/protocol'
  import { colorHex } from './colors'
  import Avatar from './Avatar.svelte'
  import AvatarPicker from './AvatarPicker.svelte'

  export let player: Player
  export let kioskName = ''
  export let volume = 1
  export let onResume: () => void
  export let onQuit: () => void
  export let onVolume: (v: number) => void
  export let onSpeaker: () => void
  /** Saves a new avatar (null removes it). Rejects when the kiosk refused it. */
  export let onAvatar: (avatar: string | null) => Promise<void>

  let confirmQuit = false
  let speakerAsked = false
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

    <button class="primary big" on:click={onResume}>Resume</button>

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

    {#if player.speaker}
      <p class="speaker-now">This phone is the speaker</p>
    {:else}
      <button
        class="secondary"
        disabled={speakerAsked}
        on:click={() => {
          speakerAsked = true
          onSpeaker()
        }}>{speakerAsked ? 'Switching the sound here…' : 'Be the speaker'}</button
      >
    {/if}

    <button class="secondary danger" on:click={quit}>{confirmQuit ? 'Tap again to quit' : 'Quit game'}</button>
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
  .pct {
    min-width: 3.5em;
    text-align: right;
    color: var(--muted);
  }
  .danger {
    color: #ff9a9a;
  }
</style>
