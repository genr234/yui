import { encodeTouch, seqNewer, type TouchPoint } from '@yui/protocol'
import type { LoopTimers } from './loop'
import { browserTimers } from './loop'

export type TouchPoints = [TouchPoint, TouchPoint]

const UP: TouchPoint = { down: false, x: 0, y: 0 }
const same = (a: TouchPoint, b: TouchPoint) => a.down === b.down && a.x === b.x && a.y === b.y

export const TOUCH_MS = 1000 / 60
/** The release frame is sent this many times, one tick apart (the channel is lossy). */
export const RELEASE_REPEATS = 3
export const RELEASE_GAP_MS = 16

/**
 * Sends 20-byte touch frames (contracts C1). It sends on every change and at
 * 60 Hz while any point is down; when the last point lifts, the release frame
 * goes out three times 16 ms apart. `seq` is its own sequence (separate from
 * the state frames), anchored to wall time like `InputLoop` so a reloaded pad
 * does not start behind what the kiosk last accepted.
 */
export class TouchSender {
  private handle: unknown = null
  private seq: number
  private current: TouchPoints = [UP, UP]
  private enabled = true
  /** Release frames still to send once nothing is down. */
  private releases = 0
  private mode: 'held' | 'release' | null = null

  constructor(
    private send: (frame: ArrayBuffer) => void,
    private timers: LoopTimers = browserTimers,
  ) {
    this.seq = (Math.floor(timers.wall() / 16) - 1) & 0xffff
  }

  get running() {
    return this.handle !== null
  }

  private get down() {
    return this.current.some((p) => p.down)
  }

  /** Disabled while the pad has no input channel; nothing is sent. */
  setEnabled(on: boolean) {
    this.enabled = on
    if (!on) {
      this.stop()
      this.releases = 0
      this.current = [UP, UP]
    }
  }

  /** The fingers on the surface now (down points carry 0…65535 coordinates). */
  update(points: TouchPoints) {
    if (!this.enabled) return
    const next: TouchPoints = [norm(points[0]), norm(points[1])]
    const changed = !same(next[0], this.current[0]) || !same(next[1], this.current[1])
    if (!changed) return
    const wasDown = this.down
    this.current = next
    if (this.down) {
      this.releases = 0
      this.emit()
      if (this.mode !== 'held') this.start('held', TOUCH_MS)
    } else if (wasDown) {
      // The last finger lifted: the release frame now and two more, 16 ms apart.
      this.emit()
      this.releases = RELEASE_REPEATS - 1
      this.start('release', RELEASE_GAP_MS)
    }
  }

  /** Lift everything (the surface went away, the layout changed). */
  cancel() {
    this.update([UP, UP])
  }

  dispose() {
    this.stop()
    this.enabled = false
  }

  /** One timer tick (exposed for tests). */
  tick() {
    if (!this.running) return
    if (this.down) return this.emit()
    if (this.releases > 0) {
      this.releases--
      this.emit()
    }
    if (this.releases <= 0) this.stop()
  }

  private start(mode: 'held' | 'release', ms: number) {
    this.stop()
    this.mode = mode
    this.handle = this.timers.setInterval(() => this.tick(), ms)
  }

  private stop() {
    if (this.handle !== null) this.timers.clearInterval(this.handle)
    this.handle = null
    this.mode = null
  }

  private nextSeq(): number {
    let next = (this.seq + 1) & 0xffff
    const anchor = Math.floor(this.timers.wall() / 16) & 0xffff
    if (seqNewer(anchor, next)) next = anchor
    this.seq = next
    return next
  }

  private emit() {
    this.send(encodeTouch({ seq: this.nextSeq(), tMs: Math.floor(this.timers.now()) >>> 0, points: this.current }))
  }
}

// A point that is not down has x = y = 0 on the wire.
const norm = (p: TouchPoint): TouchPoint => (p.down ? { down: true, x: p.x & 0xffff, y: p.y & 0xffff } : UP)
