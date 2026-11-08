import type { TextOp } from '@yui/protocol'
import type { Side } from '../input/model'
import { BIT, type ControlSink } from './sink'

/*
 * Framework-free behaviour of each control. Svelte components only translate
 * pointer events (with capture per pointer, so several fingers work at once)
 * into calls on these drivers; tests drive them directly.
 */

export const TAP_MS = 300
export const TAP_SLOP_PX = 10

// ------------------------------------------------------------------ button

/**
 * `bits` ≠ 0: those bits are held in the input frame while pressed, and no
 * events are sent. `bits` = 0: `press` / `release` events, plus `tap` after a
 * short press. Several fingers on one button count as one press.
 */
export class ButtonDriver {
  private pointers = new Set<number>()
  private pressedAt = 0
  private key: string

  constructor(
    private sink: ControlSink,
    private id: string,
    private bits = 0,
  ) {
    this.key = `button:${id}`
  }

  get pressed() {
    return this.pointers.size > 0
  }

  down(pointer: number) {
    if (this.pointers.has(pointer)) return
    this.pointers.add(pointer)
    if (this.pointers.size > 1) return
    this.pressedAt = this.sink.now()
    this.sink.feedback()
    if (this.bits) this.sink.input.hold(this.key, this.bits)
    else if (this.id) this.sink.event(this.id, 'press')
  }

  up(pointer: number, cancelled = false) {
    if (!this.pointers.delete(pointer) || this.pointers.size > 0) return
    if (this.bits) {
      this.sink.input.release(this.key)
      return
    }
    if (!this.id) return
    this.sink.event(this.id, 'release')
    if (!cancelled && this.sink.now() - this.pressedAt <= TAP_MS) this.sink.event(this.id, 'tap')
  }

  /** Let go without a tap (layout replaced, page hidden). */
  reset() {
    for (const p of [...this.pointers]) this.up(p, true)
  }
}

// -------------------------------------------------------------------- dpad

export type Dir = 'up' | 'down' | 'left' | 'right'
export const DIR_BIT: Record<Dir, number> = { up: BIT.Up, down: BIT.Down, left: BIT.Left, right: BIT.Right }
export const DPAD_DEADZONE = 0.3

/** Directions for a touch at (x, y) in −1…1 of the pad (screen coords, +y down). */
export function dpadDirs(x: number, y: number, eightWay: boolean): Dir[] {
  if (Math.hypot(x, y) < DPAD_DEADZONE) return []
  const deg = (Math.atan2(-y, x) * 180) / Math.PI // 0 = right, 90 = up
  const a = (deg + 360) % 360
  if (!eightWay) {
    if (a >= 45 && a < 135) return ['up']
    if (a >= 135 && a < 225) return ['left']
    if (a >= 225 && a < 315) return ['down']
    return ['right']
  }
  const sector = Math.round(a / 45) % 8
  return (
    [['right'], ['up', 'right'], ['up'], ['up', 'left'], ['left'], ['down', 'left'], ['down'], ['down', 'right']] as Dir[][]
  )[sector]
}

/**
 * `bits` mode (default): 8-way, bits 13–16 in the input frame.
 * `events` mode: 4-way, `press` / `release` with the direction as `value`.
 * The newest finger on the pad steers it.
 */
export class DpadDriver {
  private active: number | null = null
  private dirs: Dir[] = []
  private key: string

  constructor(
    private sink: ControlSink,
    private id: string,
    private mode: 'bits' | 'events' = 'bits',
    private allowed: Dir[] = ['up', 'down', 'left', 'right'],
    private rotate: 0 | 90 = 0,
  ) {
    this.key = `dpad:${id}`
  }

  get current(): Dir[] {
    return this.dirs
  }

  down(pointer: number, x: number, y: number) {
    this.active = pointer
    this.update(x, y)
  }

  move(pointer: number, x: number, y: number) {
    if (pointer === this.active) this.update(x, y)
  }

  up(pointer: number) {
    if (pointer !== this.active) return
    this.active = null
    this.set([])
  }

  reset() {
    this.active = null
    this.set([])
  }

  private update(x: number, y: number) {
    // rotate 90: the pad is a remote held sideways (top to the screen's left),
    // so screen-up is the remote's Right.
    if (this.rotate === 90) [x, y] = [-y, x]
    this.set(dpadDirs(x, y, this.mode === 'bits').filter((d) => this.allowed.includes(d)))
  }

  private set(next: Dir[]) {
    const prev = this.dirs
    const added = next.filter((d) => !prev.includes(d))
    const removed = prev.filter((d) => !next.includes(d))
    if (!added.length && !removed.length) return
    this.dirs = next
    if (added.length) this.sink.feedback()
    if (this.mode === 'bits') {
      this.sink.input.hold(this.key, next.reduce((b, d) => b | (1 << DIR_BIT[d]), 0))
      return
    }
    for (const d of removed) this.sink.event(this.id, 'release', d)
    for (const d of added) this.sink.event(this.id, 'press', d)
  }
}

// ------------------------------------------------------------------- stick

export const STICK_DEADZONE = 0.08

/** Offset (px, screen coords) → axes in −1…1 with +y up, clamped to the unit circle, radial dead zone. */
export function stickAxes(dx: number, dy: number, radius: number): [number, number] {
  if (radius <= 0) return [0, 0]
  let x = dx / radius
  let y = -dy / radius
  const r = Math.hypot(x, y)
  if (r < STICK_DEADZONE) return [0, 0]
  const scaled = Math.min(1, (r - STICK_DEADZONE) / (1 - STICK_DEADZONE))
  x = (x / r) * scaled
  y = (y / r) * scaled
  return [x, y]
}

/**
 * A floating stick: the centre is where the finger lands, so there is no dead
 * spot to find by feel. Axes go into the frame (`left` → lx/ly, `right` →
 * rx/ry) and centre on release. With `click_bit`, a quick tap without moving
 * pulses that bit for `CLICK_MS`.
 */
export const CLICK_MS = 100

export class StickDriver {
  private active: number | null = null
  private ox = 0
  private oy = 0
  private downAt = 0
  private travel = 0
  private key: string
  private clickTimer: unknown = null
  knob: [number, number] = [0, 0]

  constructor(
    private sink: ControlSink,
    id: string,
    private side: Side,
    private clickBit: number | null,
    public radius = 60,
  ) {
    this.key = `stick:${id}`
  }

  get origin(): [number, number] {
    return [this.ox, this.oy]
  }

  get touching() {
    return this.active !== null
  }

  down(pointer: number, x: number, y: number) {
    if (this.active !== null) return
    this.active = pointer
    this.ox = x
    this.oy = y
    this.downAt = this.sink.now()
    this.travel = 0
    this.knob = [0, 0]
    this.sink.input.stick(this.key, this.side, 0, 0)
  }

  move(pointer: number, x: number, y: number) {
    if (pointer !== this.active) return
    const [ax, ay] = stickAxes(x - this.ox, y - this.oy, this.radius)
    this.travel = Math.max(this.travel, Math.hypot(ax, ay))
    this.knob = [ax, ay]
    this.sink.input.stick(this.key, this.side, ax, ay)
  }

  up(pointer: number, cancelled = false) {
    if (pointer !== this.active) return
    this.active = null
    this.knob = [0, 0]
    this.sink.input.stickRelease(this.key)
    if (cancelled || this.clickBit === null) return
    if (this.sink.now() - this.downAt <= TAP_MS && this.travel < 0.2) {
      const key = `${this.key}:click`
      this.sink.feedback()
      this.sink.input.hold(key, 1 << this.clickBit)
      if (this.clickTimer !== null) this.sink.clearTimeout(this.clickTimer)
      this.clickTimer = this.sink.setTimeout(() => {
        this.clickTimer = null
        this.sink.input.release(key)
      }, CLICK_MS)
    }
  }

  reset() {
    if (this.active !== null) this.up(this.active, true)
  }
}

// ---------------------------------------------------------------- trackpad

export const TRACKPAD_FLUSH_MS = 1000 / 60

/**
 * `relative`: `change` events `{dx, dy, fingers}` (px since the last change,
 * at most one per frame; 1 finger = move, 2 = scroll) and `tap {fingers}`.
 * `absolute`: the first finger's position is the frame pointer while touched
 * (−1…1, +y down), plus `press` / `release` events.
 */
export class TrackpadDriver {
  private pts = new Map<number, { x: number; y: number }>()
  private downAt = 0
  private maxFingers = 0
  private moved = 0
  private acc = { dx: 0, dy: 0, fingers: 1 }
  private lastFlush = -Infinity
  private timer: unknown = null
  private absPointer: number | null = null
  private key: string

  constructor(
    private sink: ControlSink,
    private id: string,
    private mode: 'relative' | 'absolute' = 'relative',
  ) {
    this.key = `trackpad:${id}`
  }

  get fingers() {
    return this.pts.size
  }

  /** x, y in px; nx, ny in −1…1 of the surface (absolute mode). */
  down(pointer: number, x: number, y: number, nx = 0, ny = 0) {
    if (this.pts.has(pointer)) return
    if (this.pts.size === 0) {
      this.downAt = this.sink.now()
      this.maxFingers = 0
      this.moved = 0
    }
    this.flush()
    this.pts.set(pointer, { x, y })
    this.maxFingers = Math.max(this.maxFingers, this.pts.size)
    if (this.mode === 'absolute' && this.absPointer === null) {
      this.absPointer = pointer
      this.sink.input.pointer(this.key, nx, ny)
      this.sink.event(this.id, 'press')
    }
  }

  move(pointer: number, x: number, y: number, nx = 0, ny = 0) {
    const p = this.pts.get(pointer)
    if (!p) return
    const dx = x - p.x
    const dy = y - p.y
    p.x = x
    p.y = y
    this.moved += Math.hypot(dx, dy)
    if (this.mode === 'absolute') {
      if (pointer === this.absPointer) this.sink.input.pointer(this.key, nx, ny)
      return
    }
    const fingers = Math.min(2, this.pts.size)
    if (fingers !== this.acc.fingers) this.flush()
    this.acc.fingers = fingers
    // Two fingers: the scroll amount is their average movement.
    this.acc.dx += dx / fingers
    this.acc.dy += dy / fingers
    this.schedule()
  }

  up(pointer: number, cancelled = false) {
    if (!this.pts.delete(pointer)) return
    if (this.mode === 'absolute' && pointer === this.absPointer) {
      this.absPointer = null
      this.sink.input.pointerRelease(this.key)
      this.sink.event(this.id, 'release')
    }
    if (this.pts.size > 0) return
    this.flush()
    if (this.mode === 'relative' && !cancelled && this.sink.now() - this.downAt <= TAP_MS && this.moved < TAP_SLOP_PX) {
      this.sink.event(this.id, 'tap', { fingers: this.maxFingers })
    }
  }

  reset() {
    for (const p of [...this.pts.keys()]) this.up(p, true)
  }

  private schedule() {
    const wait = this.lastFlush + TRACKPAD_FLUSH_MS - this.sink.now()
    if (wait <= 0) {
      this.flush()
      return
    }
    if (this.timer === null) {
      this.timer = this.sink.setTimeout(() => {
        this.timer = null
        this.flush()
      }, wait)
    }
  }

  private flush() {
    if (this.timer !== null) {
      this.sink.clearTimeout(this.timer)
      this.timer = null
    }
    const dx = Math.trunc(this.acc.dx)
    const dy = Math.trunc(this.acc.dy)
    if (dx === 0 && dy === 0) return
    // Whole pixels only; the remainder rides along to the next change.
    this.acc.dx -= dx
    this.acc.dy -= dy
    this.lastFlush = this.sink.now()
    this.sink.event(this.id, 'change', { dx, dy, fingers: this.acc.fingers })
  }
}

// ---------------------------------------------------------------- keyboard

/** Ops that turn `prev` into `next` at the end of the text (backspaces, then an insert). */
export function diffText(prev: string, next: string): { op: TextOp; text?: string }[] {
  const a = Array.from(prev)
  const b = Array.from(next)
  let k = 0
  while (k < a.length && k < b.length && a[k] === b[k]) k++
  const ops: { op: TextOp; text?: string }[] = []
  for (let i = k; i < a.length; i++) ops.push({ op: 'backspace' })
  if (k < b.length) ops.push({ op: 'insert', text: b.slice(k).join('') })
  return ops
}

/** Leading character that keeps a "Backspace on empty field" visible as an input change. */
export const SENTINEL = '​'
const MAX_LIVE = 200

/**
 * Live typing: the hidden field always holds SENTINEL + what was typed since
 * the last Enter. Each `input` event is diffed, so autocorrect, IME
 * composition and paste all come out as backspaces + inserts.
 */
export class LiveKeyboard {
  private content = ''

  constructor(private sink: ControlSink) {}

  get value() {
    return SENTINEL + this.content
  }

  /** Returns the value the field should now hold. */
  input(raw: string): string {
    let next: string
    if (raw.startsWith(SENTINEL)) {
      next = raw.slice(SENTINEL.length)
      for (const o of diffText(this.content, next)) this.sink.text(o.op, o.text)
    } else {
      // The sentinel itself was deleted: Backspace at the start or on an empty field.
      next = raw.split(SENTINEL).join('')
      const ops = diffText(this.content, next)
      if (!ops.length) ops.push({ op: 'backspace' })
      for (const o of ops) this.sink.text(o.op, o.text)
    }
    this.content = Array.from(next).slice(-MAX_LIVE).join('')
    return this.value
  }

  enter(): string {
    this.sink.text('enter')
    this.content = ''
    return this.value
  }
}
