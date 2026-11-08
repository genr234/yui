/**
 * A small ActionCable client (actioncable-v1-json) for one channel
 * subscription. Auth is the `yui_player` cookie the browser sends with the
 * WebSocket handshake. Reconnects with backoff after drops, and treats a
 * socket with no server ping for `staleMs` as dead (phones switching networks
 * often leave a half-open socket behind).
 */

export interface SocketLike {
  readonly readyState: number
  send(data: string): void
  close(): void
  onopen: ((ev: unknown) => void) | null
  onmessage: ((ev: { data: unknown }) => void) | null
  onclose: ((ev: unknown) => void) | null
  onerror: ((ev: unknown) => void) | null
}

export type SocketFactory = (url: string, protocols: string[]) => SocketLike

export interface Timers {
  now(): number
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(handle: unknown): void
}

export const browserTimers: Timers = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
}

export interface CableHandlers {
  /** A channel broadcast for our subscription. */
  message(msg: Record<string, unknown>): void
  /** The subscription is confirmed (after every (re)connect). */
  subscribed(): void
  /** The socket dropped; a reconnect is scheduled. */
  down(): void
  /** The server refused us for good (rejected subscription or `reconnect: false`). */
  closed(reason: string): void
}

export const CABLE_PROTOCOLS = ['actioncable-v1-json', 'actioncable-unsupported']
const OPEN = 1
const BACKOFF_MS = [500, 1000, 2000, 4000, 8000]

export class CableClient {
  private socket: SocketLike | null = null
  private identifier: string
  private attempt = 0
  private retryTimer: unknown = null
  private staleTimer: unknown = null
  private lastSeen = 0
  private stopped = true
  private confirmed = false

  constructor(
    private url: string,
    channel: Record<string, unknown>,
    private handlers: CableHandlers,
    private socketFactory: SocketFactory = (u, p) => new WebSocket(u, p) as unknown as SocketLike,
    private timers: Timers = browserTimers,
    private staleMs = 8000,
  ) {
    this.identifier = JSON.stringify(channel)
  }

  get connected() {
    return this.confirmed && this.socket?.readyState === OPEN
  }

  start() {
    if (!this.stopped) return
    this.stopped = false
    this.open()
  }

  /** Close for good (kicked, ended, page leaving). */
  stop() {
    this.stopped = true
    this.clearTimers()
    this.drop()
  }

  /** Reconnect right away (network came back, tab visible again). */
  reconnectNow() {
    if (this.stopped) return
    if (this.connected && this.timers.now() - this.lastSeen < this.staleMs) return
    this.clearTimers()
    this.drop()
    this.attempt = 0
    this.open()
  }

  /** `perform action` on the channel; false when the socket is not ready. */
  perform(action: string, data: Record<string, unknown>): boolean {
    if (!this.connected || !this.socket) return false
    this.socket.send(JSON.stringify({ command: 'message', identifier: this.identifier, data: JSON.stringify({ ...data, action }) }))
    return true
  }

  private open() {
    let socket: SocketLike
    try {
      socket = this.socketFactory(this.url, CABLE_PROTOCOLS)
    } catch {
      this.scheduleRetry()
      return
    }
    this.socket = socket
    this.confirmed = false
    this.lastSeen = this.timers.now()
    socket.onopen = () => {
      this.lastSeen = this.timers.now()
      this.watchStale()
    }
    socket.onmessage = (ev) => {
      if (socket !== this.socket) return
      this.lastSeen = this.timers.now()
      this.onFrame(ev.data)
    }
    socket.onclose = () => {
      if (socket !== this.socket) return
      this.lost()
    }
    socket.onerror = () => {}
  }

  private onFrame(data: unknown) {
    let frame: Record<string, unknown>
    try {
      frame = JSON.parse(String(data))
    } catch {
      return
    }
    if (!frame || typeof frame !== 'object') return
    switch (frame.type) {
      case 'welcome':
        this.socket?.send(JSON.stringify({ command: 'subscribe', identifier: this.identifier }))
        return
      case 'ping':
        return
      case 'confirm_subscription':
        if (frame.identifier !== this.identifier) return
        this.confirmed = true
        this.attempt = 0
        this.handlers.subscribed()
        return
      case 'reject_subscription':
        if (frame.identifier !== this.identifier) return
        this.finish('rejected')
        return
      case 'disconnect':
        if (frame.reconnect === false) this.finish(typeof frame.reason === 'string' && frame.reason ? frame.reason : 'disconnect')
        else this.lost()
        return
    }
    if (frame.identifier !== this.identifier) return
    const msg = frame.message
    if (msg && typeof msg === 'object' && !Array.isArray(msg)) this.handlers.message(msg as Record<string, unknown>)
  }

  private finish(reason: string) {
    this.stop()
    this.handlers.closed(reason)
  }

  private lost() {
    if (this.stopped) return
    const was = this.confirmed
    this.drop()
    this.clearTimers()
    if (was || this.attempt === 0) this.handlers.down()
    this.scheduleRetry()
  }

  private scheduleRetry() {
    if (this.stopped) return
    const wait = BACKOFF_MS[Math.min(this.attempt, BACKOFF_MS.length - 1)]
    this.attempt++
    this.retryTimer = this.timers.setTimeout(() => {
      this.retryTimer = null
      this.open()
    }, wait)
  }

  private watchStale() {
    if (this.staleTimer !== null) this.timers.clearTimeout(this.staleTimer)
    this.staleTimer = this.timers.setTimeout(() => {
      this.staleTimer = null
      if (this.stopped) return
      if (this.timers.now() - this.lastSeen >= this.staleMs) this.lost()
      else this.watchStale()
    }, this.staleMs / 2)
  }

  private drop() {
    const s = this.socket
    this.socket = null
    this.confirmed = false
    if (s) {
      s.onopen = s.onmessage = s.onclose = s.onerror = null
      try {
        s.close()
      } catch {
        /* already closed */
      }
    }
  }

  private clearTimers() {
    if (this.retryTimer !== null) this.timers.clearTimeout(this.retryTimer)
    if (this.staleTimer !== null) this.timers.clearTimeout(this.staleTimer)
    this.retryTimer = this.staleTimer = null
  }
}
