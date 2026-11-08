import { parseControl, type ControlMessage, type Hello, type Player } from '@yui/protocol'
import { browserTimers, type CableHandlers, type Timers } from './cable'

/*
 * The pad's side of a play session (contracts §2 signaling, §3 channels).
 *
 * Cable (PlaySessionChannel, cookie auth):
 *   pad → kiosk   perform signal {type: answer|ice|hello|control, data}
 *   kiosk → pad   {type: offer|ice|bye|control, data}, {type: player_updated, player},
 *                 {type: kicked}, {type: session_ended}
 * WebRTC: the kiosk offers and creates `control` (reliable) and `input`
 * (unordered, no retransmits). The pad answers, trickles ICE both ways, sends
 * the protocol `hello` when `control` opens, and answers `ping` with `pong`.
 *
 * Fallback: when ICE fails (or `control` has not opened `FALLBACK_MS` after
 * the answer), control objects go over the cable as `type: "control"`.
 * Input frames never do. While in fallback the pad re-sends the `hello`
 * signal every `RENEGOTIATE_MS` so the kiosk tries a fresh peer.
 */

export interface ChannelLike {
  readonly label: string
  readonly readyState: string
  binaryType: string
  send(data: string | ArrayBuffer): void
  close(): void
  onopen: ((ev: unknown) => void) | null
  onclose: ((ev: unknown) => void) | null
  onmessage: ((ev: { data: unknown }) => void) | null
}

export interface PeerLike {
  ondatachannel: ((ev: { channel: ChannelLike }) => void) | null
  onicecandidate: ((ev: { candidate: { toJSON(): unknown } | null }) => void) | null
  oniceconnectionstatechange: ((ev: unknown) => void) | null
  onconnectionstatechange: ((ev: unknown) => void) | null
  readonly iceConnectionState: string
  readonly connectionState: string
  setRemoteDescription(desc: { type: string; sdp: string }): Promise<void>
  createAnswer(): Promise<{ type: string; sdp?: string }>
  setLocalDescription(desc: { type: string; sdp?: string }): Promise<void>
  addIceCandidate(candidate: unknown): Promise<void>
  close(): void
}

export interface CableLike {
  readonly connected: boolean
  start(): void
  stop(): void
  reconnectNow(): void
  perform(action: string, data: Record<string, unknown>): boolean
}

export type ConnState = 'connecting' | 'online' | 'fallback' | 'reconnecting' | 'kicked' | 'ended' | 'closed'

export interface SessionDeps {
  cable(handlers: CableHandlers): CableLike
  peer(config: { iceServers: { urls: string[]; username?: string; credential?: string }[] }): PeerLike
  ice(): Promise<{ urls: string[]; username?: string; credential?: string }[]>
  hello(): Hello
  timers?: Timers
}

export interface SessionEvents {
  state(s: ConnState): void
  /** Kiosk control messages other than ping (answered here). */
  control(msg: ControlMessage): void
  player(p: Player): void
  /** The `input` channel opened (true) or went away (false). */
  input(ready: boolean): void
}

export const FALLBACK_MS = 15000
export const RENEGOTIATE_MS = 30000
const ICE_TTL_MS = 10 * 60 * 1000

export class PadSession {
  private cable: CableLike
  private peer: PeerLike | null = null
  private gen = 0
  private control: ChannelLike | null = null
  private inputCh: ChannelLike | null = null
  private remoteSet = false
  private pending: unknown[] = []
  private fallback = false
  private fallbackTimer: unknown = null
  private renegotiateTimer: unknown = null
  private iceCache: { at: number; servers: Awaited<ReturnType<SessionDeps['ice']>> } | null = null
  private timers: Timers
  private _state: ConnState = 'connecting'
  private done = false

  constructor(
    private deps: SessionDeps,
    private events: SessionEvents,
  ) {
    this.timers = deps.timers ?? browserTimers
    this.cable = deps.cable({
      message: (m) => this.onCable(m),
      subscribed: () => this.onSubscribed(),
      down: () => this.onCableDown(),
      closed: () => this.onCableClosed(),
    })
  }

  get state() {
    return this._state
  }

  get inputReady() {
    return this.inputCh?.readyState === 'open'
  }

  start() {
    this.cable.start()
  }

  stop() {
    this.done = true
    this.cable.stop()
    this.closePeer()
    this.clearFallback()
  }

  /** Network back / tab visible: check the cable right away. */
  wake() {
    if (!this.done) this.cable.reconnectNow()
  }

  /** Send a control object over the data channel, or over the cable in fallback. */
  send(msg: ControlMessage | Record<string, unknown>): boolean {
    if (this.done) return false
    if (!this.fallback && this.control?.readyState === 'open') {
      this.control.send(JSON.stringify(msg))
      return true
    }
    if (this.fallback) return this.cable.perform('signal', { type: 'control', data: msg as Record<string, unknown> })
    return false
  }

  sendInput(frame: ArrayBuffer): boolean {
    if (this.inputCh?.readyState !== 'open') return false
    try {
      this.inputCh.send(frame)
      return true
    } catch {
      return false
    }
  }

  // ------------------------------------------------------------ cable side

  private setState(s: ConnState) {
    if (this.done && s !== 'kicked' && s !== 'ended' && s !== 'closed') return
    if (s === this._state) return
    this._state = s
    this.events.state(s)
  }

  private signal(type: string, data: Record<string, unknown> = {}) {
    return this.cable.perform('signal', { type, data })
  }

  private onSubscribed() {
    // A (re)subscribe: ask the kiosk to (re)negotiate if it has no live peer for us.
    this.signal('hello')
    if (this.control?.readyState === 'open' && !this.fallback) this.setState('online')
    else if (this.fallback) this.setState('fallback')
    else this.setState('connecting')
  }

  private onCableDown() {
    // A working data channel survives a cable drop; only show it when it matters.
    if (this.control?.readyState === 'open' && !this.fallback) return
    this.setState('reconnecting')
  }

  private onCableClosed() {
    if (this._state === 'kicked' || this._state === 'ended') return
    this.end('closed')
  }

  private end(s: 'kicked' | 'ended' | 'closed') {
    this.done = true
    this.closePeer()
    this.clearFallback()
    this.cable.stop()
    this._state = s
    this.events.state(s)
  }

  private onCable(m: Record<string, unknown>) {
    if (this.done) return
    const data = m.data && typeof m.data === 'object' && !Array.isArray(m.data) ? (m.data as Record<string, unknown>) : {}
    switch (m.type) {
      case 'offer':
        void this.onOffer(data)
        return
      case 'ice':
        this.onRemoteIce(data)
        return
      case 'bye':
        this.closePeer()
        this.clearFallback()
        this.setState(this.cable.connected ? 'connecting' : 'reconnecting')
        return
      case 'control':
        this.onControlText(JSON.stringify(data))
        return
      case 'player_updated':
        if (m.player && typeof m.player === 'object') this.events.player(m.player as Player)
        return
      case 'kicked':
        this.end('kicked')
        return
      case 'session_ended':
        this.end('ended')
        return
    }
  }

  // ------------------------------------------------------------- WebRTC side

  private async iceServers() {
    if (this.iceCache && this.timers.now() - this.iceCache.at < ICE_TTL_MS) return this.iceCache.servers
    try {
      const servers = await this.deps.ice()
      this.iceCache = { at: this.timers.now(), servers }
      return servers
    } catch {
      return this.iceCache?.servers ?? []
    }
  }

  private async onOffer(data: Record<string, unknown>) {
    if (typeof data.sdp !== 'string') return
    this.closePeer()
    const gen = ++this.gen
    this.pending = []
    this.remoteSet = false
    const iceServers = await this.iceServers()
    if (gen !== this.gen || this.done) return

    const pc = this.deps.peer({ iceServers })
    this.peer = pc
    pc.ondatachannel = (ev) => {
      if (gen === this.gen) this.adoptChannel(ev.channel)
    }
    pc.onicecandidate = (ev) => {
      if (gen !== this.gen || !ev.candidate) return
      this.signal('ice', ev.candidate.toJSON() as Record<string, unknown>)
    }
    const watch = () => {
      if (gen !== this.gen) return
      if (pc.iceConnectionState === 'failed' || pc.connectionState === 'failed') this.enterFallback()
    }
    pc.oniceconnectionstatechange = watch
    pc.onconnectionstatechange = watch

    try {
      await pc.setRemoteDescription({ type: 'offer', sdp: data.sdp })
      if (gen !== this.gen) return
      this.remoteSet = true
      for (const c of this.pending.splice(0)) await pc.addIceCandidate(c).catch(() => {})
      const answer = await pc.createAnswer()
      if (gen !== this.gen) return
      await pc.setLocalDescription(answer)
      if (gen !== this.gen) return
      this.signal('answer', { type: 'answer', sdp: answer.sdp ?? '' })
    } catch {
      if (gen === this.gen) this.enterFallback()
      return
    }
    if (this.fallbackTimer !== null) this.timers.clearTimeout(this.fallbackTimer)
    this.fallbackTimer = this.timers.setTimeout(() => {
      this.fallbackTimer = null
      if (gen === this.gen && this.control?.readyState !== 'open') this.enterFallback()
    }, FALLBACK_MS)
  }

  private onRemoteIce(data: Record<string, unknown>) {
    if (typeof data.candidate !== 'string' || !data.candidate) return
    if (!this.peer || !this.remoteSet) {
      this.pending.push(data)
      return
    }
    void this.peer.addIceCandidate(data).catch(() => {})
  }

  private adoptChannel(ch: ChannelLike) {
    ch.binaryType = 'arraybuffer'
    if (ch.label === 'control') {
      this.control = ch
      const open = () => {
        if (this.control !== ch) return
        this.clearFallback()
        this.setState('online')
        ch.send(JSON.stringify(this.deps.hello()))
      }
      ch.onopen = open
      ch.onmessage = (ev) => {
        if (this.control === ch && typeof ev.data === 'string') this.onControlText(ev.data)
      }
      ch.onclose = () => {
        if (this.control !== ch) return
        this.control = null
        if (!this.fallback && !this.done) this.setState(this.cable.connected ? 'connecting' : 'reconnecting')
      }
      if (ch.readyState === 'open') open()
    } else if (ch.label === 'input') {
      this.inputCh = ch
      ch.onopen = () => {
        if (this.inputCh === ch) this.events.input(true)
      }
      ch.onclose = () => {
        if (this.inputCh !== ch) return
        this.inputCh = null
        this.events.input(false)
      }
      if (ch.readyState === 'open') this.events.input(true)
    }
  }

  private onControlText(text: string) {
    let msg: ControlMessage
    try {
      msg = parseControl(text)
    } catch {
      return // malformed or a type this pad does not know
    }
    if (msg.t === 'ping') {
      this.send({ t: 'pong', id: msg.id, ts: msg.ts })
      return
    }
    this.events.control(msg)
  }

  private enterFallback() {
    if (this.done) return
    if (!this.fallback) {
      this.fallback = true
      this.setState('fallback')
      this.send(this.deps.hello())
    }
    if (this.renegotiateTimer === null) {
      this.renegotiateTimer = this.timers.setTimeout(() => {
        this.renegotiateTimer = null
        if (!this.fallback || this.done) return
        this.signal('hello')
        this.enterFallback() // re-arm
      }, RENEGOTIATE_MS)
    }
  }

  private clearFallback() {
    this.fallback = false
    if (this.fallbackTimer !== null) this.timers.clearTimeout(this.fallbackTimer)
    if (this.renegotiateTimer !== null) this.timers.clearTimeout(this.renegotiateTimer)
    this.fallbackTimer = this.renegotiateTimer = null
  }

  private closePeer() {
    this.gen++
    const pc = this.peer
    this.peer = null
    this.remoteSet = false
    const hadInput = this.inputCh !== null
    for (const ch of [this.control, this.inputCh]) {
      if (!ch) continue
      ch.onopen = ch.onclose = ch.onmessage = null
      try {
        ch.close()
      } catch {
        /* closed */
      }
    }
    this.control = null
    this.inputCh = null
    if (hadInput) this.events.input(false)
    if (pc) {
      pc.ondatachannel = pc.onicecandidate = pc.oniceconnectionstatechange = pc.onconnectionstatechange = null
      try {
        pc.close()
      } catch {
        /* closed */
      }
    }
  }
}
