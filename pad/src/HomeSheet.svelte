<script lang="ts">
  import type { MotionView } from './input/motion'
  import type { Player, Peer } from '@yui/protocol'
  import { colorHex } from './colors'
  import Avatar from './Avatar.svelte'
  import CreatureBuilder from './CreatureBuilder.svelte'
  import CodeEntry from './CodeEntry.svelte'
  import { friendlyError } from './api'
  import Icon, { type IconName } from './Icon.svelte'
  import SeatGive from './SeatGive.svelte'
  import SettingIcon from './SettingIcon.svelte'
  import { emptySeat, type SeatState } from './seatPass'

  export let player: Player
  export let kioskName = ''
  export let volume = 1
  export let onResume: () => void
  export let onQuit: () => void
  /** Something (an app or a game) is running on the kiosk, so Quit means something. */
  export let playing = false
  export let onVolume: (v: number) => void
  /** Set while the game uses motion: its status, for the Motion row. */
  export let motion: MotionView | null = null
  /** Re-zero the pointer where the phone points now (closes the menu). */
  export let onRecenter: () => void = () => {}
  /** Open the calibration screen (closes the menu). */
  export let onCalibrate: () => void = () => {}
  /** Take the sound here; with a channel, join the current speaker as that half of a stereo pair. */
  export let onSpeaker: (channel?: 'left' | 'right') => void
  /** Vibration on/off for this phone; null hides the switch (no haptics here). */
  export let vibration: boolean | null = null
  export let onVibration: (on: boolean) => void = () => {}
  /** Saves a new avatar (null removes it). Rejects when the kiosk refused it. */
  /** Other players may poke this phone. */
  export let allowPokes = true
  export let onPokes: (on: boolean) => void = () => {}
  export let onAvatar: (avatar: string | null) => Promise<void>
  /** The Yui account linked to this phone, if any (plan 10). */
  export let accountName: string | null = null
  /** Play as the account (true) or as a guest (false) this session. Rejects on failure. */
  export let onAccountUse: (use: boolean) => Promise<void> = async () => {}
  /** Forget the link on this phone. */
  export let onAccountForget: () => Promise<void> = async () => {}
  /** Redeem a phone code. */
  export let onAccountLink: (code: string) => Promise<void> = async () => {}

  /** Pass your seat (see seatPass.ts): spectators this phone may offer its seat to. */
  export let spectators: Peer[] = []
  export let seat: SeatState = emptySeat
  export let seatSeconds = 0
  export let onSeatOffer: (to: string) => void = () => {}
  export let onSeatCancel: () => void = () => {}
  export let onSeatDismiss: () => void = () => {}

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
  let savingAvatar = false
  let avatarError = ''

  function editAvatar() {
    avatarError = ''
    editingAvatar = true
  }

  async function saveAvatar(next: string) {
    if (next === (player.avatar ?? null)) {
      editingAvatar = false
      return
    }
    savingAvatar = true
    avatarError = ''
    try {
      await onAvatar(next)
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

  $: motionOn = motion?.status === 'on' && motion.calibrated
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

  function motionLabel(m: MotionView): string {
    if (m.status === 'denied') return 'Blocked in this browser'
    if (m.status === 'unsupported') return 'This phone has no motion sensor'
    if (m.status === 'skipped') return 'Skipped: touch controls'
    if (m.status === 'needs-permission') return 'Needs permission'
    if (!m.calibrated) return 'Needs calibrating'
    return m.gyro ? 'Aiming with the phone' : 'No gyroscope: aim by touch'
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

{#if editingAvatar}
  <CreatureBuilder
    value={player.avatar ?? null}
    color={colorHex(player.color)}
    busy={savingAvatar}
    error={avatarError}
    doneLabel="Save"
    onCancel={() => (editingAvatar = false)}
    onDone={saveAvatar}
  />
{/if}

<div class="scrim" role="presentation" on:click|self={onResume}>
  <div class="sheet kit-panel" role="dialog" aria-modal="true" aria-label="Home" style="--player:{colorHex(player.color)}">
    <header>
      <span class="slot" class:with-avatar={!!player.avatar}
        ><Avatar avatar={player.avatar} color={colorHex(player.color)} size={40} />{player.slot ? `P${player.slot}` : 'Spectator'}</span
      >
      <div class="who">
        <strong>{player.name}</strong>
        <span class="muted">{kioskName || 'Yui'}{player.host ? ' · host' : ''}</span>
      </div>
      {#if !editingAvatar}
        <button class="link" type="button" on:click={editAvatar}>{player.avatar ? 'Change creature' : 'Make a creature'}</button>
      {/if}
      <!-- Always reachable, whichever tab the sheet reopens on. -->
      <button class="close" type="button" aria-label={playing ? 'Resume' : 'Close'} on:click={onResume}><Icon name="cross" size={18} /></button>
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
        Per-feature sections go here (one group per feature, e.g. reactions,
        snapshot, game-specific options). Keep Resume/Close first and Quit game last.
      -->
      {#if motion}
        <section class="set-section" aria-label="Motion">
          <h3 class="set-title">Motion</h3>
          <div class="set-group">
            <div class="set-row">
              <span class="set-badge"><SettingIcon name="tilt" /></span>
              <span class="set-label"><span>Motion</span><span class="set-sub">{motionLabel(motion)}</span></span>
              <span class="set-pill" class:on={motionOn}>{motionOn ? 'On' : 'Off'}</span>
            </div>
            {#if motionOn}
              <button class="set-row" type="button" on:click={onRecenter}>
                <span class="set-badge"><SettingIcon name="target" /></span>
                <span class="set-label"><span>Recenter aim</span></span>
                <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
              </button>
            {/if}
            {#if motion.status === 'on' || motion.status === 'skipped'}
              <button class="set-row" type="button" on:click={onCalibrate}>
                <span class="set-badge"><SettingIcon name="refresh" /></span>
                <span class="set-label"><span>{motion.calibrated ? 'Recalibrate' : 'Calibrate'}</span></span>
                <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
              </button>
            {/if}
          </div>
        </section>
      {/if}

      {#if player.slot || seat.outgoing || seat.result}
        <SeatGive {spectators} {seat} secondsLeft={seatSeconds} onOffer={onSeatOffer} onCancel={onSeatCancel} onDismissResult={onSeatDismiss} />
      {/if}

      {#if playing}
        <button class="danger-btn" on:click={quit}>{confirmQuit ? 'Tap again to quit' : 'Quit game'}</button>
      {/if}
    {:else if tab === 'sound'}
      <section class="set-section" aria-label="Sound">
        <h3 class="set-title">Sound</h3>
        <div class="set-group">
          <label class="set-row volume">
            <span class="set-badge"><SettingIcon name="volume" /></span>
            <span class="set-label"><span>Volume</span></span>
            <span class="set-value pct">{Math.round(volume * 100)}%</span>
            <input
              style="--fill:{Math.round(volume * 100)}%"
              type="range"
              min="0"
              max="100"
              step="1"
              aria-label="Volume"
              value={Math.round(volume * 100)}
              on:input={(e) => onVolume(Number(e.currentTarget.value) / 100)}
            />
          </label>
          {#if vibration !== null}
            <label class="set-row">
              <span class="set-badge"><SettingIcon name="pulse" /></span>
              <span class="set-label"><span>Vibration</span></span>
              <input class="set-switch" type="checkbox" role="switch" checked={vibration} on:change={(e) => onVibration(e.currentTarget.checked)} />
            </label>
          {/if}
        </div>
      </section>

      <section class="set-section" aria-label="Speaker">
        <h3 class="set-title">Speaker</h3>
        <div class="set-group">
          {#if player.speaker}
            <div class="set-row">
              <span class="set-badge"><SettingIcon name="speaker" /></span>
              <span class="set-label"
                ><span>{player.speaker_channel ? `This phone is the ${player.speaker_channel} speaker` : 'This phone is the speaker'}</span></span
              >
              <span class="set-pill on">On</span>
            </div>
            {#if player.speaker_channel}
              <button
                class="set-row"
                type="button"
                disabled={speakerAsked !== ''}
                on:click={() => {
                  speakerAsked = 'mono'
                  onSpeaker()
                }}
              >
                <span class="set-badge"><SettingIcon name="volume" /></span>
                <span class="set-label"><span>{speakerAsked ? 'Switching the sound here…' : 'Play all the sound here'}</span></span>
                <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
              </button>
            {/if}
          {:else}
            <button
              class="set-row"
              type="button"
              disabled={speakerAsked !== ''}
              on:click={() => {
                speakerAsked = 'mono'
                onSpeaker()
              }}
            >
              <span class="set-badge"><SettingIcon name="speaker" /></span>
              <span class="set-label"><span>{speakerAsked === 'mono' ? 'Switching the sound here…' : 'Be the speaker'}</span></span>
              <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
            </button>
            <button
              class="set-row"
              type="button"
              disabled={speakerAsked !== ''}
              on:click={() => {
                speakerAsked = 'right'
                onSpeaker('right')
              }}
            >
              <span class="set-badge"><SettingIcon name="swap" /></span>
              <span class="set-label"><span>{speakerAsked === 'right' ? 'Pairing as the right speaker…' : 'Be the right speaker'}</span></span>
              <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
            </button>
          {/if}
        </div>
      </section>
    {:else}
      <section class="set-section" aria-label="Play">
        <h3 class="set-title">Play</h3>
        <div class="set-group">
          <label class="set-row">
            <span class="set-badge"><SettingIcon name="bell" /></span>
            <span class="set-label"><span>Allow pokes</span></span>
            <input class="set-switch" type="checkbox" role="switch" checked={allowPokes} on:change={(e) => onPokes(e.currentTarget.checked)} />
          </label>
        </div>
      </section>

      <section class="set-section" aria-label="Yui account">
        <h3 class="set-title">Account</h3>
        {#if enteringCode}
          <CodeEntry busy={accountBusy} error={accountError} onSubmit={link} onCancel={() => ((enteringCode = false), (accountError = ''))} />
        {:else}
          <div class="set-group">
            {#if usingAccount || accountName}
              <div class="set-row">
                <span class="set-badge"><SettingIcon name="user" /></span>
                <span class="set-label"><span>{label}</span></span>
                <span class="set-pill" class:on={usingAccount}>{usingAccount ? 'Playing' : 'Linked'}</span>
              </div>
              {#if usingAccount}
                <button class="set-row" type="button" disabled={accountBusy} on:click={() => account(() => onAccountUse(false))}>
                  <span class="set-badge"><SettingIcon name="logout" /></span>
                  <span class="set-label"><span>Stop using it here</span></span>
                  <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
                </button>
              {:else}
                <button class="set-row" type="button" disabled={accountBusy} on:click={() => account(() => onAccountUse(true))}>
                  <span class="set-badge"><SettingIcon name="link" /></span>
                  <span class="set-label"><span>Play as {label}</span></span>
                  <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
                </button>
              {/if}
              <button class="set-row danger" type="button" disabled={accountBusy} on:click={forget}>
                <span class="set-badge"><SettingIcon name="logout" /></span>
                <span class="set-label"><span>{confirmForget ? 'Tap again to forget this phone' : 'Forget this phone'}</span></span>
              </button>
            {:else}
              <button class="set-row" type="button" on:click={() => (enteringCode = true)}>
                <span class="set-badge"><SettingIcon name="link" /></span>
                <span class="set-label"><span>Use my Yui account</span></span>
                <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
              </button>
            {/if}
          </div>
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
  /* A red round close button on the panel's corner, like a game options window. */
  .close {
    flex: 0 0 auto;
    width: 38px;
    height: 38px;
    display: grid;
    place-items: center;
    padding: 0;
    border: 2px solid #b8263a;
    border-radius: 50%;
    background: linear-gradient(180deg, #f2606f 0 50%, var(--kit-red) 50%);
    color: #fff;
    box-shadow: 0 3px 0 #b8263a, inset 0 0 0 2px rgba(255, 255, 255, 0.35);
  }
  .close:active {
    transform: translateY(3px);
    box-shadow: inset 0 0 0 2px rgba(255, 255, 255, 0.35);
  }
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
  .big {
    min-height: 72px;
    font-size: 1.35rem;
  }
  .err {
    margin: 0;
    padding: 0 16px;
    color: var(--kit-red);
    font-weight: 800;
  }
  /* Volume: label and percentage on top, a full-width slider under them. */
  .volume {
    flex-wrap: wrap;
    row-gap: 2px;
    padding-bottom: 10px;
  }
  .volume input {
    flex: 1 0 100%;
    height: 32px;
    margin: 0;
  }
  .volume input::-webkit-slider-runnable-track {
    height: 8px;
    box-shadow: none;
  }
  .volume input::-moz-range-track {
    height: 8px;
  }
  /* A round white knob, like the switches. */
  .volume input::-webkit-slider-thumb {
    width: 26px;
    height: 26px;
    margin-top: -9px;
    border: 0;
    border-radius: 50%;
    background: #fff;
    box-shadow: 0 1px 4px rgba(20, 30, 50, 0.3), 0 0 0 0.5px rgba(20, 30, 50, 0.08);
  }
  .volume input::-moz-range-thumb {
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: 50%;
    background: #fff;
    box-shadow: 0 1px 4px rgba(20, 30, 50, 0.3);
  }
  .pct {
    min-width: 3em;
    justify-content: flex-end;
  }
</style>
