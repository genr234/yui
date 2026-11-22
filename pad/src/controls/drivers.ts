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

// Touch assist only. A finger on glass has nothing to feel, so it wanders:
// diagonals get half the room of a straight direction (an accidental
// down-right is a crouch in most platformers), the direction held keeps a
// little extra angle before it lets go, and the centre releases a bit closer
// in than it engages, so a resting thumb does not chatter.
export const DPAD_RELEASE = 0.2
export const DPAD_CARDINAL_HALF = 30
export const DPAD_DIAGONAL_HALF = 15
export const DPAD_HYSTERESIS = 10
/** Direction A, then B, then A again within this is counted as a flicker. */
export const FLICKER_MS = 120

const SECTORS: Dir[][] = [['right'], ['up', 'right'], ['up'], ['up', 'left'], ['left'], ['down', 'left'], ['down'], ['down', 'right']]
const sameDirs = (a: Dir[], b: Dir[]) => a.length === b.length && a.every((d) => b.includes(d))
/** Smallest angle between two bearings, in degrees. */
const angleBetween = (a: number, b: number) => {
  const d = Math.abs(a - b) % 360
  return d > 180 ? 360 - d : d
}

/**
 * Directions for a touch at (x, y) in −1…1 of the pad (screen coords, +y down).
 * Without `assist` it is the plain split: eight 45° sectors (four 90° ones
 * when 4-way) and one dead-zone radius. With it, see the constants above;
 * `prev` is what the pad reports now, for the hysteresis.
 */
export function dpadDirs(x: number, y: number, eightWay: boolean, prev: Dir[] = [], assist = false): Dir[] {
  const r = Math.hypot(x, y)
  if (r < (assist && prev.length ? DPAD_RELEASE : DPAD_DEADZONE)) return []
  const deg = (Math.atan2(-y, x) * 180) / Math.PI // 0 = right, 90 = up
  const a = (deg + 360) % 360
  if (!eightWay) {
    if (assist && prev.length === 1) {
      const held = SECTORS.findIndex((s) => sameDirs(s, prev))
      if (held % 2 === 0 && angleBetween(a, held * 45) <= 45 + DPAD_HYSTERESIS) return prev
    }
    if (a >= 45 && a < 135) return ['up']
    if (a >= 135 && a < 225) return ['left']
    if (a >= 225 && a < 315) return ['down']
    return ['right']
  }
  if (!assist) return SECTORS[Math.round(a / 45) % 8]
  const half = (i: number) => (i % 2 ? DPAD_DIAGONAL_HALF : DPAD_CARDINAL_HALF)
  const held = prev.length ? SECTORS.findIndex((s) => sameDirs(s, prev)) : -1
  if (held >= 0 && angleBetween(a, held * 45) <= half(held) + DPAD_HYSTERESIS) return prev
  let i = Math.round(a / 45) % 8
  if (i % 2 && angleBetween(a, i * 45) > DPAD_DIAGONAL_HALF) {
    // Outside the narrow diagonal: the straight direction on that side.
    const ccw = (i + 1) % 8
    const cw = (i + 7) % 8
    i = angleBetween(a, ccw * 45) < angleBetween(a, cw * 45) ? ccw : cw
  }
  return SECTORS[i]
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
  /** The last two direction sets and when they started, for flicker stats. */
  private history: { dirs: Dir[]; at: number }[] = []

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
    if (Math.hypot(x, y) < DPAD_DEADZONE) this.sink.stat?.('dpad_deadzone')
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
    const assist = this.sink.assist?.() ?? false
    this.set(dpadDirs(x, y, this.mode === 'bits', this.dirs, assist).filter((d) => this.allowed.includes(d)))
  }

  private set(next: Dir[]) {
    const prev = this.dirs
    const added = next.filter((d) => !prev.includes(d))
    const removed = prev.filter((d) => !next.includes(d))
    if (!added.length && !removed.length) return
    this.dirs = next
    this.noteChange(next)
    if (added.length) this.sink.feedback()
    if (this.mode === 'bits') {
      this.sink.input.hold(this.key, next.reduce((b, d) => b | (1 << DIR_BIT[d]), 0))
      return
    }
    for (const d of removed) this.sink.event(this.id, 'release', d)
    for (const d of added) this.sink.event(this.id, 'press', d)
  }

  private noteChange(next: Dir[]) {
    if (!this.sink.stat) return
    const now = this.sink.now()
    if (next.length) this.sink.stat('dpad_change')
    // A → B → A while the finger stays down, B lasting under FLICKER_MS.
    const [older, last] = this.history
    if (next.length && older && last && last.dirs.length && older.dirs.length && sameDirs(older.dirs, next) && now - last.at < FLICKER_MS) {
      this.sink.stat('dpad_flicker')
    }
    this.history = next.length ? [...this.history.slice(-1), { dirs: next, at: now }] : []
  }
}

// ------------------------------------------------------------------ cluster

/** One face button the cluster can press: centre and radius in px, and its bits. */
export interface ClusterTarget {
  id: string
  bits: number
  x: number
  y: number
  r: number
}

// Distances are in button radii. With the face diamond's geometry (cells about
// two radii apart), the gap between two neighbours is about one radius wide.
/** A touch further than this from every button presses nothing. */
export const CLUSTER_REACH = 2
/** Outside every button, the nearest must win by this much or nothing is pressed (the diamond's centre). */
export const CLUSTER_TIE = 0.2
/** Buttons whose centres are closer than this are neighbours (the diamond's sides, not its diagonals). */
export const CHORD_NEIGHBOUR = 3.6
/**
 * The chord zone: off both buttons, in the middle of the line between them
 * (within CHORD_ALONG of halfway) and within CHORD_SIDE of it. A finger already
 * holding the chord keeps it over a wider zone (the _HELD values). The rest of
 * the gap goes to the nearer button, so a sloppy single press stays single.
 */
export const CHORD_MIN = 1
export const CHORD_ALONG = 0.1
export const CHORD_SIDE = 0.5
export const CHORD_ALONG_HELD = 0.2
export const CHORD_SIDE_HELD = 0.75
/** What a finger already holds counts as this much closer, so it does not chatter between two. */
export const CLUSTER_STICKY = 0.3
/** A tap shorter than this followed within CORRECTION_GAP_MS by another button is a correction. */
export const CORRECTION_TAP_MS = 150
export const CORRECTION_GAP_MS = 200

/**
 * Which buttons a touch at (x, y) presses. `held` is what that finger holds
 * now. Without `assist`: only a button the touch is on, and the finger keeps
 * the button it landed on (like a plain button with pointer capture), so
 * `held` wins while it is non-empty.
 */
export function clusterPick(targets: ClusterTarget[], x: number, y: number, held: string[] = [], assist = false): string[] {
  const raw = targets.map((t) => ({ t, d: Math.hypot(x - t.x, y - t.y) / (t.r || 1) }))
  if (!assist) {
    if (held.length) return held
    const on = raw.filter((e) => e.d <= 1).sort((a, b) => a.d - b.d)[0]
    return on ? [on.t.id] : []
  }
  const scored = raw.map((e) => ({ ...e, s: held.includes(e.t.id) ? e.d - CLUSTER_STICKY : e.d })).sort((a, b) => a.s - b.s)
  const [n1, n2] = scored
  if (!n1) return []
  if (n2 && n1.d > CHORD_MIN && n2.d > CHORD_MIN) {
    const r = (n1.t.r + n2.t.r) / 2 || 1
    const vx = n2.t.x - n1.t.x
    const vy = n2.t.y - n1.t.y
    const len = Math.hypot(vx, vy)
    if (len > 0 && len / r < CHORD_NEIGHBOUR) {
      const along = ((x - n1.t.x) * vx + (y - n1.t.y) * vy) / (len * len)
      const side = Math.abs((x - n1.t.x) * vy - (y - n1.t.y) * vx) / len / r
      const holding = held.length === 2 && held.includes(n1.t.id) && held.includes(n2.t.id)
      const maxAlong = holding ? CHORD_ALONG_HELD : CHORD_ALONG
      const maxSide = holding ? CHORD_SIDE_HELD : CHORD_SIDE
      if (Math.abs(along - 0.5) <= maxAlong && side <= maxSide) return [n1.t.id, n2.t.id]
    }
  }
  if (n1.s > CLUSTER_REACH) return []
  // Off every button the nearest must clearly win, unless the finger already holds it.
  if (n1.d <= 1 || !n2 || held.includes(n1.t.id) || n2.s - n1.s >= CLUSTER_TIE) return [n1.t.id]
  return []
}

/**
 * The face buttons as one touch area. Without assist it behaves like separate
 * buttons. With it, a touch between buttons presses the nearest, a finger can
 * roll from one button onto the next, and resting it in the gap between two
 * neighbours holds both (run + jump). Every finger holds its own bits.
 */
export class ClusterDriver {
  /** Per finger: what it holds, when it landed, and (assist off) the button it is over. */
  private ptrs = new Map<number, { ids: string[]; downAt: number; over?: string }>()
  private targets: ClusterTarget[] = []
  private lastTap: { id: string; at: number } | null = null
  private key: string

  constructor(
    private sink: ControlSink,
    id: string,
  ) {
    this.key = `cluster:${id}`
  }

  /** Where the buttons are now (measured on each touch, so a resize is picked up). */
  setTargets(targets: ClusterTarget[]) {
    this.targets = targets
  }

  /** Every button some finger holds. */
  get held(): Set<string> {
    const out = new Set<string>()
    for (const p of this.ptrs.values()) for (const id of p.ids) out.add(id)
    return out
  }

  private get assist() {
    return this.sink.assist?.() ?? false
  }

  down(pointer: number, x: number, y: number) {
    if (this.ptrs.has(pointer)) return
    const now = this.sink.now()
    const ids = clusterPick(this.targets, x, y, [], this.assist)
    const onButton = this.targets.some((t) => Math.hypot(x - t.x, y - t.y) <= t.r)
    if (!onButton) this.sink.stat?.(ids.length ? 'gap' : 'gap_miss')
    if (ids.length === 2) this.sink.stat?.('chord')
    if (ids.length === 1 && this.lastTap && this.lastTap.id !== ids[0] && now - this.lastTap.at <= CORRECTION_GAP_MS) {
      this.sink.stat?.('correction')
    }
    this.lastTap = null
    this.ptrs.set(pointer, { ids: [], downAt: now })
    this.apply(pointer, ids)
  }

  move(pointer: number, x: number, y: number) {
    const p = this.ptrs.get(pointer)
    if (!p) return
    const assist = this.assist
    if (!assist) {
      // Count what assist would have rolled onto: the finger is now on another button.
      const on = this.targets.find((t) => !p.ids.includes(t.id) && Math.hypot(x - t.x, y - t.y) <= t.r)
      if (p.ids.length && on && on.id !== p.over) this.sink.stat?.('slide')
      p.over = on?.id
      return
    }
    const ids = clusterPick(this.targets, x, y, p.ids, true)
    if (sameIds(ids, p.ids)) return
    if (ids.length === 2) this.sink.stat?.('chord')
    else if (ids.length === 1 && p.ids.length && !p.ids.includes(ids[0])) this.sink.stat?.('slide')
    this.apply(pointer, ids)
  }

  up(pointer: number, cancelled = false) {
    const p = this.ptrs.get(pointer)
    if (!p) return
    this.ptrs.delete(pointer)
    this.sink.input.release(`${this.key}:${pointer}`)
    const now = this.sink.now()
    if (!cancelled && p.ids.length === 1 && now - p.downAt <= CORRECTION_TAP_MS) this.lastTap = { id: p.ids[0], at: now }
  }

  reset() {
    for (const p of [...this.ptrs.keys()]) this.up(p, true)
  }

  private apply(pointer: number, ids: string[]) {
    const p = this.ptrs.get(pointer)
    if (!p) return
    const before = this.held
    p.ids = ids
    const after = this.held
    let added = 0
    for (const id of after) if (!before.has(id)) added++
    if (added) {
      this.sink.feedback()
      for (let i = 0; i < added; i++) this.sink.stat?.('press')
    }
    const bits = this.targets.filter((t) => ids.includes(t.id)).reduce((b, t) => b | t.bits, 0)
    this.sink.input.hold(`${this.key}:${pointer}`, bits)
  }
}

const sameIds = (a: string[], b: string[]) => a.length === b.length && a.every((x) => b.includes(x))

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
