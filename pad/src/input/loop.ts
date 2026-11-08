import { encodeInput, seqNewer } from '@yui/protocol'
import type { InputModel } from './model'

/** Clock + timer seam so tests can drive the loop by hand. */
export interface LoopTimers {
  now(): number // ms, monotonic
  wall(): number // ms since epoch
  setInterval(fn: () => void, ms: number): unknown
  clearInterval(handle: unknown): void
}

export const browserTimers: LoopTimers = {
  now: () => performance.now(),
  wall: () => Date.now(),
  setInterval: (fn, ms) => setInterval(fn, ms),
  clearInterval: (h) => clearInterval(h as ReturnType<typeof setInterval>),
}

export const FRAME_MS = 1000 / 60
/** Shortest gap between two frames; a button edge inside this waits for the next tick. */
export const MIN_GAP_MS = 4

/**
 * Sends 48-byte input frames at ~60 Hz under the contracts §3 rules:
 * running while any stick, absolute trackpad, bit button or d-pad is held, or
 * while the layout has a `motion` node (`setStreaming(true)`); one final frame
 * when that stops; otherwise idle. Button edges go out immediately so a press
 * never waits a whole tick.
 *
 * `seq` only ever increases (u16, wraps). It is anchored to wall time
 * (one step per 16 ms) so a reloaded pad does not start behind the seq the
 * kiosk last accepted from this player and get dropped as stale.
 */
export class InputLoop {
  private handle: unknown = null
  private streaming = false
  private seq: number
  private lastSent = -Infinity
  private lastButtons = 0
  private unsubscribe: () => void
  private enabled = true

  constructor(
    private model: InputModel,
    private send: (frame: ArrayBuffer) => void,
    private timers: LoopTimers = browserTimers,
  ) {
    this.seq = (Math.floor(timers.wall() / 16) - 1) & 0xffff
    this.unsubscribe = model.subscribe(() => this.onChange())
  }

  get running() {
    return this.handle !== null
  }

  /** The layout has a motion node: stream continuously. */
  setStreaming(on: boolean) {
    if (this.streaming === on) return
    this.streaming = on
    this.onChange()
  }

  /** Disabled while the pad has no input channel; nothing is sent and the loop stops. */
  setEnabled(on: boolean) {
    this.enabled = on
    if (!on) this.stop()
    else this.onChange()
  }

  dispose() {
    this.unsubscribe()
    this.stop()
  }

  private active() {
    return this.enabled && (this.streaming || this.model.touching())
  }

  private onChange() {
    if (!this.enabled) return
    if (this.active()) {
      if (!this.running) {
        this.handle = this.timers.setInterval(() => this.tick(), FRAME_MS)
        this.emit()
        return
      }
      if (this.model.buttons() !== this.lastButtons && this.timers.now() - this.lastSent >= MIN_GAP_MS) this.emit()
      return
    }
    if (this.running) {
      // Final frame carries the released state, then go idle.
      this.emit()
      this.stop()
    }
  }

  /** One timer tick (exposed for tests). */
  tick() {
    if (!this.running) return
    this.emit()
    if (!this.active()) this.stop()
  }

  private stop() {
    if (this.handle !== null) this.timers.clearInterval(this.handle)
    this.handle = null
  }

  private nextSeq(): number {
    let next = (this.seq + 1) & 0xffff
    const anchor = Math.floor(this.timers.wall() / 16) & 0xffff
    if (seqNewer(anchor, next)) next = anchor
    this.seq = next
    return next
  }

  private emit() {
    const f = this.model.frame()
    const now = this.timers.now()
    this.lastSent = now
    this.lastButtons = f.buttons
    this.send(
      encodeInput({
        seq: this.nextSeq(),
        tMs: Math.floor(now) >>> 0,
        buttons: f.buttons,
        lx: f.lx,
        ly: f.ly,
        rx: f.rx,
        ry: f.ry,
        accel: f.accel,
        gyro: f.gyro,
        px: f.px,
        py: f.py,
      }),
    )
  }
}
