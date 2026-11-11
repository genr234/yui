<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import type { ControlMessage, Hello, Node, Player } from '@yui/protocol'
  import { sketchState } from './controls/sketch'
  import LayoutView from './layout/LayoutView.svelte'
  import HomeSheet from './HomeSheet.svelte'
  import ProfileSheet from './ProfileSheet.svelte'
  import { enqueueAsk, type ProfileAsk } from './account'
  import Avatar from './Avatar.svelte'
  import SimGyro from './SimGyro.svelte'
  import { colorHex } from './colors'
  import { cableUrl, forgetAccount, getIce, grantProfile, linkAccount, simSlotFromQuery, updateAvatar, useAccount } from './api'
  import { InputModel } from './input/model'
  import { InputLoop } from './input/loop'
  import { MotionController } from './input/motion'
  import { BIT, type ControlSink } from './controls/sink'
  import { providePad } from './controls/context'
  import { expandTree, scanTree, wantsMotion } from './layout/presets'
  import { CableClient } from './net/cable'
  import { PadSession, type ConnState, type PeerLike } from './net/session'
  import {
    DeviceMotionSource,
    SimMotionSource,
    loadCalibration,
    motionNeedsPermission,
    motionSupported,
    remoteFlip,
    requestMotionPermission,
    saveCalibration,
  } from './platform/motion'
  import { hapticsSupported, playHaptic, pressTick, setVibrationEnabled, vibrationEnabled } from './platform/haptics'
  import { playSound, setMasterVolume, unlockAudio } from './platform/sound'
  import { keepAwake, wakeLockSupported } from './platform/wakelock'
  import { attachSpeakerStream, playSpeaker, speakerBlocked } from './platform/speaker'

  export let code: string
  export let player: Player
  export let kioskName = ''
  /** The kiosk hotspot from `welcome`; named when the link falls back. */
  let wifiSsid = ''
  /** Clock time the session ends for inactivity, or 0 (from `idle`). */
  let idleEndsAt = 0
  let idleLeft = 0
  /** The Yui account linked to this phone, if any (plan 10). */
  export let accountName: string | null = null
  /** Local layout preview (dev `?demo=layout`): no session, events go to `onDemoEvent`. */
  export let demoTree: Node | null = null
  export let onDemoEvent: (line: string) => void = () => {}
  export let onEnd: (state: 'kicked' | 'ended' | 'closed') => void = () => {}

  const sim = simSlotFromQuery() !== null
  const model = new InputModel()

  // ------------------------------------------------------------ orientation
  let landscape = false
  let angle = 0
  function readOrientation() {
    landscape = window.innerWidth > window.innerHeight
    const a = screen.orientation?.angle ?? Number((window as unknown as { orientation?: number }).orientation ?? 0)
    angle = Number.isFinite(a) ? a : 0
    if (sim && landscape) angle = 90
  }

  // ---------------------------------------------------------------- session
  let conn: ConnState = demoTree ? 'online' : 'connecting'
  let session: PadSession | null = null
  let tree: Node | null = demoTree
  let layoutVersion = -Infinity
  let sensitive = false
  // True while an app or native game owns the phones (kiosk `system` `playing`).
  let playing = false
  let homeOpen = false
  let volume = loadVolume()
  setMasterVolume(volume)
  let vibration = vibrationEnabled()
  function changeVibration(on: boolean) {
    vibration = on
    setVibrationEnabled(on)
    if (on) pressTick() // a small buzz to show it works
  }

  function hello(): Hello {
    return {
      t: 'hello',
      v: 1,
      caps: { motion: motionSupported() || sim, haptics: hapticsSupported(), wakeLock: wakeLockSupported() },
      screen: { w: Math.round(screen.width), h: Math.round(screen.height), orientation: landscape ? 'landscape' : 'portrait' },
    }
  }

  function send(msg: ControlMessage | Record<string, unknown>) {
    if (demoTree) {
      onDemoEvent(JSON.stringify(msg))
      return
    }
    session?.send(msg)
  }

  const sink: ControlSink = {
    input: model,
    event: (control, kind, value) => send(value === undefined ? { t: 'event', control, kind } : { t: 'event', control, kind, value }),
    text: (op, text) => send(text === undefined ? { t: 'text', op } : { t: 'text', op, text }),
    feedback: pressTick,
    now: () => performance.now(),
    setTimeout: (fn, ms) => setTimeout(fn, ms),
    clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
  }

  const loop = new InputLoop(model, (frame) => {
    session?.sendInput(frame)
  })
  // Enabled once the `input` channel opens (always in the local demo).
  loop.setEnabled(!!demoTree)

  let idleTimer: ReturnType<typeof setTimeout> | null = null
  // Counts the idle warning down once a second until it ends or is lifted.
  function tickIdle() {
    if (idleTimer) clearTimeout(idleTimer)
    idleTimer = null
    if (!idleEndsAt) return
    idleLeft = Math.max(0, Math.ceil((idleEndsAt - Date.now()) / 1000))
    if (idleLeft > 0) idleTimer = setTimeout(tickIdle, 1000)
  }

  function onControl(msg: ControlMessage) {
    switch (msg.t) {
      case 'welcome':
        player = msg.player
        kioskName = msg.kiosk_name
        wifiSsid = msg.wifi_ssid ?? ''
        layoutVersion = -Infinity // a new peer starts its own layout versions
        return
      case 'idle':
        idleEndsAt = msg.ends_in > 0 ? Date.now() + msg.ends_in * 1000 : 0
        tickIdle()
        return
      case 'layout':
        if (msg.version <= layoutVersion) return
        layoutVersion = msg.version
        model.releaseAll()
        sketchState.newLayout()
        tree = msg.tree
        return
      case 'player':
        player = msg.player
        return
      case 'haptic':
        playHaptic(msg)
        return
      case 'sound':
        playSound(msg.url, msg.volume ?? 1)
        return
      case 'profile_request':
        // Only a player who plays as an account can be asked; others say no at once.
        if (!player.account_linked) send({ t: 'profile_reply', app_id: msg.app_id, granted: false })
        else asks = enqueueAsk(asks, { app_id: msg.app_id, app_name: msg.app_name })
        return
      case 'system':
        if (msg.action === 'sensitive') sensitive = msg.value === true
        else if (msg.action === 'playing') playing = msg.value === true
        else if (msg.action === 'resume') homeOpen = false
        else if (msg.action === 'home') homeOpen = true
        else if (msg.action === 'volume' && typeof msg.value === 'number') {
          // The kiosk echoes the speaker volume (it applies the gain itself).
          volume = Math.max(0, Math.min(1, msg.value))
          setMasterVolume(volume)
        }
        return
    }
  }

  // ------------------------------------------------------ profile requests
  let asks: ProfileAsk[] = []
  let askBusy = false

  function reply(app_id: string, granted: boolean) {
    send({ t: 'profile_reply', app_id, granted })
  }

  /** Answers every waiting ask with no (the player left the account, or the session ended). */
  function declineAll() {
    for (const a of asks) reply(a.app_id, false)
    asks = []
  }

  async function answerAsk(allow: boolean) {
    const ask = asks[0]
    if (!ask || askBusy) return
    askBusy = true
    let granted = false
    if (allow) {
      try {
        if (!demoTree) await grantProfile(code, ask.app_id)
        granted = true
      } catch {
        granted = false
      }
    }
    // The queue may have been cleared while the request was in flight.
    if (asks[0]?.app_id === ask.app_id) {
      asks = asks.slice(1)
      reply(ask.app_id, granted)
    }
    askBusy = false
  }

  // ---------------------------------------------------------------- account
  function applyAccount(a: { account_name: string | null; account_linked: boolean }) {
    accountName = a.account_name ?? (a.account_linked ? accountName : null)
    player = { ...player, account_linked: a.account_linked }
  }
  // Leaving the account (here or from the kiosk) answers every waiting game with no.
  $: if (!player.account_linked && asks.length > 0) declineAll()
  async function accountUse(use: boolean) {
    if (demoTree) return applyAccount({ account_name: accountName, account_linked: use })
    applyAccount(await useAccount(code, use))
  }
  async function accountForget() {
    if (demoTree) return applyAccount({ account_name: null, account_linked: false })
    const a = await forgetAccount(code)
    accountName = null
    applyAccount(a)
  }
  async function accountLink(link_code: string) {
    applyAccount(await linkAccount(code, link_code))
  }

  /** Home sheet: rejoin with a new avatar (Rails broadcasts it to the kiosk). */
  async function changeAvatar(avatar: string | null) {
    if (demoTree) {
      player = { ...player, avatar }
      return
    }
    player = (await updateAvatar(code, avatar)).player
  }

  function startSession() {
    session = new PadSession(
      {
        cable: (handlers) => new CableClient(cableUrl(), { channel: 'PlaySessionChannel' }, handlers),
        peer: (config) => {
          const pc = new RTCPeerConnection(config)
          // The kiosk's audio track; silent unless this phone is the speaker.
          pc.addEventListener('track', (ev) => {
            if (ev.track.kind !== 'audio') return
            attachSpeakerStream(ev.streams[0] ?? new MediaStream([ev.track]))
          })
          return pc as unknown as PeerLike
        },
        ice: async () => (await getIce(code)).ice_servers,
        hello,
      },
      {
        state: (s) => {
          conn = s
          if (s === 'kicked' || s === 'ended' || s === 'closed') {
            model.releaseAll()
            loop.setEnabled(false)
            motion.stop()
            asks = []
            onEnd(s)
          }
        },
        control: onControl,
        player: (p) => (player = p),
        input: (ready) => {
          if (!ready) model.releaseAll()
          loop.setEnabled(ready)
        },
      },
    )
    session.start()
  }

  // ----------------------------------------------------------------- motion
  const simSource = sim ? new SimMotionSource() : null
  const motion = new MotionController(model, simSource ?? new DeviceMotionSource(), {
    needsPermission: !sim && motionNeedsPermission(),
    supported: sim || motionSupported(),
    flip: () => remoteFlip(angle),
    calibration: sim ? { accelSign: 1, gyroBias: [0, 0, 0] } : loadCalibration(),
    saveCalibration,
  })
  const motionView = motion.view

  providePad({
    sink,
    motion: motionView,
    recenter: (down) => {
      if (down) {
        motion.recenter()
        model.hold('recenter', 1 << BIT.Recenter)
      } else model.release('recenter')
    },
    calibrate: () => motion.calibrate(),
  })

  $: motionReady = $motionView.status === 'on' && $motionView.gyro
  $: shown = tree ? expandTree(tree, { landscape, motionReady }) : null
  $: info = scanTree(shown)
  $: needsGate = wantsMotion(tree) && $motionView.status === 'needs-permission'
  // Outside the reactive flush: setWanted updates motionView, which the
  // statements above read, and a store change mid-flush would not re-run them.
  $: wantMotion(info.motion, info.pointer)
  function wantMotion(on: boolean, pointer: boolean) {
    queueMicrotask(() => motion.setWanted(on, pointer))
  }
  $: loop.setStreaming(info.motion)

  async function allowMotion() {
    unlockAudio()
    motion.setPermission((await requestMotionPermission()) === 'granted')
  }

  // ------------------------------------------------------------ home sheet
  function openHome() {
    pressTick()
    unlockAudio()
    void playSpeaker()
    model.releaseAll()
    homeOpen = true
    send({ t: 'system', action: 'home' })
  }
  function resume() {
    homeOpen = false
    send({ t: 'system', action: 'resume' })
  }
  function quit() {
    homeOpen = false
    send({ t: 'system', action: 'quit' })
  }
  let volTimer: ReturnType<typeof setTimeout> | null = null
  function changeVolume(v: number) {
    volume = v
    setMasterVolume(v)
    try {
      localStorage.setItem('yui_pad_volume', String(v))
    } catch {
      /* storage unavailable */
    }
    if (volTimer) return
    volTimer = setTimeout(() => {
      volTimer = null
      send({ t: 'system', action: 'volume', value: volume })
    }, 150)
  }
  function loadVolume() {
    try {
      const v = Number(localStorage.getItem('yui_pad_volume'))
      if (localStorage.getItem('yui_pad_volume') !== null && v >= 0 && v <= 1) return v
    } catch {
      /* storage unavailable */
    }
    return 1
  }

  // -------------------------------------------------------------- lifecycle
  function onVisibility() {
    if (document.visibilityState === 'hidden') model.releaseAll()
    else session?.wake()
  }
  const wake = () => session?.wake()

  onMount(() => {
    readOrientation()
    window.addEventListener('resize', readOrientation)
    window.addEventListener('orientationchange', readOrientation)
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('online', wake)
    if (!demoTree) {
      void keepAwake()
      startSession()
    }
  })

  onDestroy(() => {
    window.removeEventListener('resize', readOrientation)
    window.removeEventListener('orientationchange', readOrientation)
    document.removeEventListener('visibilitychange', onVisibility)
    window.removeEventListener('online', wake)
    if (idleTimer) clearTimeout(idleTimer)
    session?.stop()
    loop.dispose()
    motion.stop()
  })

  $: swatch = colorHex(player.color)
  $: connLabel = {
    connecting: 'Connecting',
    online: 'Connected',
    fallback: 'Slow link',
    reconnecting: 'Reconnecting',
    kicked: 'Removed',
    ended: 'Ended',
    closed: 'Disconnected',
  }[conn]
</script>

<div class="pad" class:landscape style="--player:{swatch}">
  <header>
    <span class="slot" class:with-avatar={!!player.avatar}
      ><Avatar avatar={player.avatar} size={26} />{player.slot ? `P${player.slot}` : 'Spectator'}</span
    >
    <span class="name">{player.name}</span>
    {#if player.speaker}
      <span
        class="speaker-badge"
        class:paired={!!player.speaker_channel}
        title={player.speaker_channel ? `This phone plays the ${player.speaker_channel} channel` : 'This phone plays the sound'}
        aria-label={player.speaker_channel ? `${player.speaker_channel} speaker` : 'Speaker'}
      >
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 9v6h4l5 4V5L8 9zm12.5 3a4.5 4.5 0 0 0-2.5-4v8a4.5 4.5 0 0 0 2.5-4zM14 3.2v2.1a7 7 0 0 1 0 13.4v2.1a9 9 0 0 0 0-17.6z" /></svg>
        {#if player.speaker_channel}<b class="channel" aria-hidden="true">{player.speaker_channel === 'left' ? 'L' : 'R'}</b>{/if}
      </span>
    {/if}
    <span class="conn {conn}" title={connLabel}><span class="dot"></span><span class="txt">{connLabel}</span></span>
    <button class="home" type="button" aria-label="Home" on:click={openHome}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 11.5 12 4l9 7.5V20a1 1 0 0 1-1 1h-5v-6h-6v6H4a1 1 0 0 1-1-1z" /></svg>
    </button>
  </header>

  <main class="stage">
    {#if needsGate}
      <section class="gate">
        <h1>Motion controls</h1>
        <p class="muted">This game uses your phone's motion. Allow it to play.</p>
        <button class="primary" on:click={allowMotion}>Allow motion</button>
        <button class="secondary" on:click={() => motion.setPermission(false)}>Play without motion</button>
      </section>
    {:else if shown}
      <div class="layout">
        <LayoutView node={shown} />
      </div>
      {#if sim && info.motion && simSource}
        <SimGyro source={simSource} />
      {/if}
    {:else}
      <section class="waiting">
        {#if player.avatar}
          <div class="big-avatar"><Avatar avatar={player.avatar} size={112} /></div>
        {/if}
        <div class="badge">{player.slot ? `P${player.slot}` : '—'}</div>
        <h1>{conn === 'online' || conn === 'fallback' ? "You're in" : connLabel + '…'}</h1>
        <p class="muted">
          {player.host ? "You're the host. " : ''}Waiting for {kioskName || 'the kiosk'}…
        </p>
      </section>
    {/if}

    {#if player.speaker && $speakerBlocked}
      <button class="sound-gate" type="button" on:click={() => void playSpeaker()}>
        Tap to enable sound
        <span>This phone is the speaker</span>
      </button>
    {/if}

    {#if idleEndsAt}
      <div class="idle-warning" role="alert">
        <strong>Still playing?</strong>
        <span>Press any button. The session ends in {idleLeft} s.</span>
      </div>
    {/if}

    {#if conn === 'fallback'}
      <!-- Input frames only travel peer to peer, so on the fallback the
           buttons don't reach the game: say how to get a direct link. -->
      <div class="slow-link" role="status">
        <strong>Can't reach the kiosk directly</strong>
        {#if wifiSsid}
          <span>Join the Wi-Fi <b>{wifiSsid}</b> (scan its QR on the kiosk screen), then come back here.</span>
        {:else}
          <span>Join the same Wi-Fi as the kiosk, then come back here.</span>
        {/if}
      </div>
    {/if}

    {#if sensitive}
      <div class="sensitive" role="status">
        <strong>Use the screen for this</strong>
        <span>The kiosk is showing something private. Phones can't type or point here.</span>
      </div>
    {/if}
  </main>

  {#if homeOpen}
    <HomeSheet
      {player}
      {kioskName}
      {volume}
      onResume={resume}
      onQuit={quit}
      {playing}
      onVolume={changeVolume}
      vibration={hapticsSupported() ? vibration : null}
      onVibration={changeVibration}
      onSpeaker={(channel) => send(channel ? { t: 'system', action: 'be_speaker', channel } : { t: 'system', action: 'be_speaker' })}
      onAvatar={changeAvatar}
      {accountName}
      onAccountUse={accountUse}
      onAccountForget={accountForget}
      onAccountLink={accountLink}
    />
  {/if}
  {#if asks.length > 0 && player.account_linked}
    <ProfileSheet ask={asks[0]} account={accountName} busy={askBusy} onAnswer={answerAsk} />
  {/if}
</div>

<style>
  .pad {
    height: 100dvh;
    display: flex;
    flex-direction: column;
    padding: env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left);
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
    overflow: hidden;
  }
  header {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--line);
    background: rgba(13, 24, 37, 0.6);
  }
  .slot {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    border-radius: var(--yui-radius-badge);
    background: var(--player);
    color: #fff;
    font-weight: 800;
  }
  .slot.with-avatar {
    padding: 3px 12px 3px 3px;
  }
  .name {
    flex: 1;
    min-width: 0;
    font-weight: 700;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .conn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--muted);
    font-size: 0.8rem;
    font-weight: 600;
  }
  .conn .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--yui-orange);
  }
  .conn.online .dot {
    background: #3ddc84;
  }
  .conn.reconnecting .dot,
  .conn.closed .dot {
    background: var(--yui-red);
  }
  .conn.online .txt {
    display: none;
  }
  .speaker-badge {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 50%;
    background: var(--yui-chrome-fill);
  }
  .speaker-badge.paired {
    width: auto;
    min-width: 32px;
    padding: 0 9px 0 7px;
    border-radius: 16px;
    grid-auto-flow: column;
    gap: 4px;
  }
  .speaker-badge .channel {
    font-size: 0.8rem;
    line-height: 1;
    color: var(--fg);
  }
  .speaker-badge svg {
    width: 18px;
    height: 18px;
    fill: var(--fg);
  }
  .sound-gate {
    position: absolute;
    left: 14px;
    right: 14px;
    bottom: 14px;
    z-index: 4;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    min-height: 64px;
    padding: 12px 16px;
    border: 0;
    border-radius: var(--yui-radius-control, 14px);
    background: var(--player);
    color: #fff;
    font: inherit;
    font-weight: 800;
    font-size: 1.05rem;
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.35);
  }
  .sound-gate span {
    font-weight: 600;
    font-size: 0.85rem;
    opacity: 0.85;
  }
  .home {
    width: 48px;
    height: 48px;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 50%;
    background: var(--yui-chrome-fill);
    box-shadow: 0 0 0 2px var(--player) inset;
  }
  .home:active {
    background: var(--player);
  }
  .home svg {
    width: 24px;
    height: 24px;
    fill: var(--fg);
  }
  .stage {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .layout {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 14px 14px 18px;
    overflow: auto;
  }
  .layout > :global(*) {
    flex: 1 1 auto;
  }
  /* Landscape phones have ~320px of height: a slimmer header and controls. */
  .landscape header {
    padding: 4px 12px;
  }
  .landscape .home {
    width: 40px;
    height: 40px;
  }
  .landscape .layout {
    padding: 8px 18px 10px;
  }
  .landscape .layout :global(.pb:not(.round)) {
    min-height: 50px;
  }
  .landscape .layout :global(.live),
  .landscape .layout :global(.field input) {
    min-height: 48px;
  }
  .landscape .layout :global(.field input) {
    padding: 10px 14px;
  }
  .gate,
  .waiting {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    padding: 24px;
    text-align: center;
    max-width: 480px;
    margin: 0 auto;
  }
  .big-avatar {
    padding: 6px;
    border-radius: 50%;
    background: var(--player);
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.35);
    margin-bottom: -4px;
  }
  .badge {
    min-width: 168px;
    padding: 24px 32px;
    border-radius: var(--yui-radius-shell);
    background: var(--player);
    color: #fff;
    font-size: 4.2rem;
    font-weight: 900;
    line-height: 1;
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.35), 0 20px 50px rgba(0, 0, 0, 0.35);
  }
  .idle-warning {
    position: absolute;
    left: 12px;
    right: 12px;
    top: 12px;
    z-index: 4;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px 14px;
    border-radius: 14px;
    background: rgba(13, 24, 37, 0.92);
    text-align: center;
  }
  .idle-warning span {
    color: var(--muted);
  }
  .slow-link {
    position: absolute;
    left: 12px;
    right: 12px;
    bottom: 12px;
    z-index: 4;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px 14px;
    border-radius: 14px;
    background: rgba(13, 24, 37, 0.92);
    text-align: center;
  }
  .slow-link span {
    color: var(--muted);
  }
  .sensitive {
    position: absolute;
    inset: 0;
    z-index: 5;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 24px;
    text-align: center;
    background: rgba(13, 24, 37, 0.92);
    font-size: 1.05rem;
  }
  .sensitive strong {
    font-size: 1.5rem;
  }
  .sensitive span {
    color: var(--muted);
    max-width: 320px;
  }
</style>
