import type { Vec3 } from '@yui/protocol'

/**
 * Live input state of the pad, merged from every control that is being held.
 *
 * Each control writes under its own key, so two controls holding the same bit
 * (or a control released while another still holds it) never clobber each
 * other. The input loop reads `frame()` and turns it into a 48-byte packet.
 *
 * Axis signs on the wire (contracts §3): sticks +x right, +y up; pointer +x
 * right, +y down (screen coordinates), both −32767…32767 with 0 = centre.
 */
export type Side = 'left' | 'right'

export interface FrameState {
  buttons: number
  lx: number
  ly: number
  rx: number
  ry: number
  accel: Vec3
  gyro: Vec3
  px: number
  py: number
}

const AXIS_MAX = 32767

export const clampAxis = (v: number) => Math.max(-AXIS_MAX, Math.min(AXIS_MAX, Math.round(v)))

export class InputModel {
  private holds = new Map<string, number>()
  private sticks = new Map<string, { side: Side; x: number; y: number }>()
  private pointers = new Map<string, { x: number; y: number }>()
  private motionPointer: { x: number; y: number } | null = null
  private accel: Vec3 = [0, 0, 0]
  private gyro: Vec3 = [0, 0, 0]
  private listeners = new Set<() => void>()

  /** Called after any change (press, release, axis, motion sample). */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  private changed() {
    for (const fn of this.listeners) fn()
  }

  /** Hold `bits` under `key`; 0 releases. */
  hold(key: string, bits: number) {
    const prev = this.holds.get(key) ?? 0
    if (bits === prev) return
    if (bits) this.holds.set(key, bits >>> 0)
    else this.holds.delete(key)
    this.changed()
  }

  release(key: string) {
    this.hold(key, 0)
  }

  /** Stick axes in −1…1 (+y up). */
  stick(key: string, side: Side, x: number, y: number) {
    this.sticks.set(key, { side, x: clampAxis(x * AXIS_MAX), y: clampAxis(y * AXIS_MAX) })
    this.changed()
  }

  stickRelease(key: string) {
    if (this.sticks.delete(key)) this.changed()
  }

  /** Pointer from a touch surface, −1…1 (+y down). */
  pointer(key: string, x: number, y: number) {
    this.pointers.set(key, { x: clampAxis(x * AXIS_MAX), y: clampAxis(y * AXIS_MAX) })
    this.changed()
  }

  pointerRelease(key: string) {
    if (this.pointers.delete(key)) this.changed()
  }

  /** A motion sample: accel in g, gyro in deg/s, optional pointer −1…1. */
  motion(accel: Vec3, gyro: Vec3, pointer: [number, number] | null) {
    this.accel = accel
    this.gyro = gyro
    this.motionPointer = pointer ? { x: clampAxis(pointer[0] * AXIS_MAX), y: clampAxis(pointer[1] * AXIS_MAX) } : null
    this.changed()
  }

  clearMotion() {
    this.accel = [0, 0, 0]
    this.gyro = [0, 0, 0]
    this.motionPointer = null
    this.changed()
  }

  /** True while any control is being touched (buttons, sticks, touch pointers). */
  touching(): boolean {
    return this.holds.size > 0 || this.sticks.size > 0 || this.pointers.size > 0
  }

  buttons(): number {
    let b = 0
    for (const bits of this.holds.values()) b |= bits
    return b >>> 0
  }

  /** Drop everything a control holds (layout swapped, page hidden, connection lost). */
  releaseAll() {
    const had = this.touching()
    this.holds.clear()
    this.sticks.clear()
    this.pointers.clear()
    if (had) this.changed()
  }

  frame(): FrameState {
    const out: FrameState = {
      buttons: this.buttons(),
      lx: 0,
      ly: 0,
      rx: 0,
      ry: 0,
      accel: [...this.accel] as Vec3,
      gyro: [...this.gyro] as Vec3,
      px: 0,
      py: 0,
    }
    // The newest writer of a side wins (Map keeps insertion order; re-set moves nothing,
    // so take the last entry per side).
    for (const s of this.sticks.values()) {
      if (s.side === 'left') {
        out.lx = s.x
        out.ly = s.y
      } else {
        out.rx = s.x
        out.ry = s.y
      }
    }
    let p: { x: number; y: number } | null = this.motionPointer
    for (const t of this.pointers.values()) p = t // touch aim overrides motion while touched
    if (p) {
      out.px = p.x
      out.py = p.y
    }
    return out
  }
}
