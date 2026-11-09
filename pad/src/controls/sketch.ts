// Pure logic for the `sketch` pad node (contracts §6): stroke state, event
// batching, coordinate normalisation and Catmull-Rom smoothing. No DOM here.
import type { ControlSink } from './sink'

export type SketchPoint = [x: number, y: number, t: number]

export interface Stroke {
  n: number
  color: string
  /** Brush size in px at 360 px drawing width (scales with width). */
  size: number
  points: SketchPoint[]
}

export const MAX_EVENTS_PER_SEC = 30
export const MIN_SEND_GAP_MS = Math.ceil(1000 / MAX_EVENTS_PER_SEC)
export const MAX_POINTS_PER_EVENT = 64
export const CLEAR_CONFIRM_MS = 3000
export const BASE_WIDTH = 360
/** The drawing area keeps this aspect ratio (width / height). */
export const ASPECT = 4 / 3

const clamp01 = (v: number) => (v < 0 ? 0 : v > 1 ? 1 : v)
const round = (v: number) => Math.round(v * 10000) / 10000

/** A pointer position as 0–1 of the drawing area, clamped. */
export function normalize(clientX: number, clientY: number, rect: { left: number; top: number; width: number; height: number }): [number, number] {
  if (!rect.width || !rect.height) return [0, 0]
  return [clamp01((clientX - rect.left) / rect.width), clamp01((clientY - rect.top) / rect.height)]
}

/** The largest 4:3 box that fits in w × h (letterboxed). */
export function fitAspect(w: number, h: number): { width: number; height: number } {
  if (w <= 0 || h <= 0) return { width: 0, height: 0 }
  return w / h > ASPECT ? { width: h * ASPECT, height: h } : { width: w, height: w / ASPECT }
}

/** Brush px for a drawing area `width` px wide. */
export const scaleSize = (size: number, width: number) => Math.max(0.5, (size * width) / BASE_WIDTH)

export interface Bezier {
  c1: [number, number]
  c2: [number, number]
  to: [number, number]
}

/**
 * Catmull-Rom through `pts` as cubic Béziers, starting at pts[0]. Endpoints are
 * duplicated so the curve passes through every point.
 */
export function catmullRom(pts: ReadonlyArray<readonly [number, number]>): Bezier[] {
  const out: Bezier[] = []
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(0, i - 1)]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[Math.min(pts.length - 1, i + 2)]
    out.push({
      c1: [p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6],
      c2: [p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6],
      to: [p2[0], p2[1]],
    })
  }
  return out
}

/** Stroke state shared across layouts: same sketch `id` keeps strokes, a different one starts blank. */
export class SketchState {
  id = ''
  strokes: Stroke[] = []
  /** Last stroke number issued; reset by `newLayout()`. */
  counter = 0

  bind(id: string) {
    if (id !== this.id) {
      this.id = id
      this.strokes = []
      this.counter = 0
    }
  }
  /** A new layout arrived: stroke numbers count from 1 again. */
  newLayout() {
    this.counter = 0
  }
}

export const sketchState = new SketchState()

type Outgoing = { stroke: Stroke; points: SketchPoint[] } | { submit: number }

export class SketchEngine {
  private queue: Outgoing[] = []
  private current: Stroke | null = null
  private startedAt = 0
  private lastSend = -Infinity
  private timer: unknown = null
  private clearTimer: unknown = null
  clearArmed = false
  /** Called after any local state change so the view can repaint. */
  onChange: () => void = () => {}

  constructor(
    private sink: ControlSink,
    private id: string,
    readonly state: SketchState,
  ) {}

  get drawing() {
    return this.current !== null
  }

  begin(color: string, size: number, x: number, y: number) {
    if (this.current) this.end()
    const stroke: Stroke = { n: ++this.state.counter, color, size, points: [] }
    this.current = stroke
    this.startedAt = this.sink.now()
    this.state.strokes.push(stroke)
    this.sink.feedback()
    this.add(x, y)
  }

  add(x: number, y: number) {
    const s = this.current
    if (!s) return
    const pt: SketchPoint = [round(x), round(y), Math.max(0, Math.round(this.sink.now() - this.startedAt))]
    s.points.push(pt)
    const last = this.queue[this.queue.length - 1]
    if (last && 'stroke' in last && last.stroke === s) last.points.push(pt)
    else this.queue.push({ stroke: s, points: [pt] })
    this.pump()
    this.onChange()
  }

  end() {
    const s = this.current
    if (!s) return
    this.current = null
    this.queue.push({ submit: s.n })
    this.pump()
    this.onChange()
  }

  /** Remove the last stroke locally and tell the kiosk. */
  undo() {
    if (this.current) this.end()
    this.state.strokes.pop()
    this.sink.event(this.id, 'tap', 'undo')
    this.disarm()
    this.onChange()
  }

  /** First press arms ("Tap again"); a second within 3 s clears. Returns whether it cleared. */
  pressClear(): boolean {
    if (!this.clearArmed) {
      this.clearArmed = true
      this.clearTimer = this.sink.setTimeout(() => {
        this.clearArmed = false
        this.clearTimer = null
        this.onChange()
      }, CLEAR_CONFIRM_MS)
      this.onChange()
      return false
    }
    this.disarm()
    if (this.current) this.end()
    this.state.strokes = []
    this.sink.event(this.id, 'tap', 'clear')
    this.onChange()
    return true
  }

  private disarm() {
    if (this.clearTimer !== null) this.sink.clearTimeout(this.clearTimer)
    this.clearTimer = null
    this.clearArmed = false
  }

  /** Send queued output: at most one `change` per MIN_SEND_GAP_MS, ≤ 64 points each. Submits follow their points. */
  private pump() {
    if (this.timer !== null) return
    for (;;) {
      const head = this.queue[0]
      if (!head) return
      if ('submit' in head) {
        this.queue.shift()
        this.sink.event(this.id, 'submit', { stroke: head.submit })
        continue
      }
      const wait = this.lastSend + MIN_SEND_GAP_MS - this.sink.now()
      if (wait > 0) {
        this.timer = this.sink.setTimeout(() => {
          this.timer = null
          this.pump()
        }, wait)
        return
      }
      const points = head.points.splice(0, MAX_POINTS_PER_EVENT)
      if (!head.points.length) this.queue.shift()
      this.lastSend = this.sink.now()
      const { n, color, size } = head.stroke
      this.sink.event(this.id, 'change', { stroke: n, color, size, points })
    }
  }

  /** Drop timers (component destroyed). Unsent points are lost; strokes stay in the shared state. */
  dispose() {
    if (this.timer !== null) this.sink.clearTimeout(this.timer)
    this.timer = null
    this.queue = []
    this.disarm()
  }
}
