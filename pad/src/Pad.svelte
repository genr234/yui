<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import type { ControlMessage, Hello, Node, Player } from '@yui/protocol'
  import LayoutView from './layout/LayoutView.svelte'
  import HomeSheet from './HomeSheet.svelte'
  import SimGyro from './SimGyro.svelte'
  import { colorHex } from './colors'
  import { cableUrl, getIce, simSlotFromQuery } from './api'
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
  import { hapticsSupported, playPattern, pressTick } from './platform/haptics'
  import { playSound, setMasterVolume, unlockAudio } from './platform/sound'
  import { keepAwake, wakeLockSupported } from './platform/wakelock'

  export let code: string
  export let player: Player
  export let kioskName = ''
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
  let homeOpen = false
  let volume = loadVolume()
  setMasterVolume(volume)

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

  function onControl(msg: ControlMessage) {
    switch (msg.t) {
      case 'welcome':
        player = msg.player
        kioskName = msg.kiosk_name
        layoutVersion = -Infinity // a new peer starts its own layout versions
        return
      case 'layout':
        if (msg.version <= layoutVersion) return
        layoutVersion = msg.version
        model.releaseAll()
        tree = msg.tree
        return
      case 'player':
        player = msg.player
        return
      case 'haptic':
        playPattern(msg.pattern)
        return
      case 'sound':
        playSound(msg.url, msg.volume ?? 1)
        return
      case 'system':
        if (msg.action === 'sensitive') sensitive = msg.value === true
        else if (msg.action === 'resume') homeOpen = false
        else if (msg.action === 'home') homeOpen = true
        return
    }
  }

  function startSession() {
    session = new PadSession(
      {
        cable: (handlers) => new CableClient(cableUrl(), { channel: 'PlaySessionChannel' }, handlers),
        peer: (config) => new RTCPeerConnection(config) as unknown as PeerLike,
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
  $: motion.setWanted(info.motion, info.pointer)
  $: loop.setStreaming(info.motion)

  async function allowMotion() {
    unlockAudio()
    motion.setPermission((await requestMotionPermission()) === 'granted')
  }

  // ------------------------------------------------------------ home sheet
  function openHome() {
    pressTick()
    unlockAudio()
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
    <span class="slot">{player.slot ? `P${player.slot}` : 'Spectator'}</span>
    <span class="name">{player.name}</span>
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
        <div class="badge">{player.slot ? `P${player.slot}` : '—'}</div>
        <h1>{conn === 'online' || conn === 'fallback' ? "You're in" : connLabel + '…'}</h1>
        <p class="muted">
          {player.host ? "You're the host. " : ''}Waiting for {kioskName || 'the kiosk'}…
        </p>
      </section>
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
      onVolume={changeVolume}
      onSpeaker={() => send({ t: 'system', action: 'be_speaker' })}
    />
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
    padding: 6px 12px;
    border-radius: var(--yui-radius-badge);
    background: var(--player);
    color: #fff;
    font-weight: 800;
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
  .landscape .layout {
    padding: 10px 18px;
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
