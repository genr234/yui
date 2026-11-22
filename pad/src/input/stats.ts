import type { Node } from '@yui/protocol'
import type { StatKey } from '../controls/sink'

/**
 * Touch-quality counters for the `input_stats` message: how often fingers
 * land on nothing, between face buttons, slide, flicker the d-pad, and so on.
 * They only ever hold counts and rounded positions on the layout, never what
 * was pressed when, so they say how the controls feel, not how anyone plays.
 *
 * Counts pile up until `take()` hands them over and starts a new window. The
 * pad sends a window every FLUSH_MS while something was counted, and when it
 * hides or the session ends.
 */
export const FLUSH_MS = 30_000
/** Dead-touch positions kept per window (the oldest go first). */
export const MAX_DEAD_AT = 32

export interface InputStatsMsg {
  t: 'input_stats'
  /** How long this window ran. */
  window_ms: number
  /** The preset name (or "custom") and orientation, e.g. "gamepad:portrait". */
  layout: string
  /** Whether touch assist was on for the whole window. */
  assist: boolean
  counts: Partial<Record<StatKey, number>>
  /** Where dead touches landed, 0…1 of the layout area, two decimals. */
  dead_at?: [number, number][]
}

export class InputStats {
  private counts: Partial<Record<StatKey, number>> = {}
  private deadAt: [number, number][] = []
  private startedAt: number
  private layout = 'none'
  private assist = false

  constructor(private now: () => number = () => performance.now()) {
    this.startedAt = now()
  }

  get empty() {
    return Object.keys(this.counts).length === 0
  }

  count(key: StatKey) {
    this.counts[key] = (this.counts[key] ?? 0) + 1
  }

  /** A finger landed on no control, at (x, y) in 0…1 of the layout. */
  dead(x: number, y: number) {
    this.count('dead')
    const round = (v: number) => Math.round(Math.max(0, Math.min(1, v)) * 100) / 100
    this.deadAt.push([round(x), round(y)])
    if (this.deadAt.length > MAX_DEAD_AT) this.deadAt.shift()
  }

  /**
   * The layout or the assist switch changed. Counts from different layouts
   * or assist settings must not mix, so the window so far is returned (if it
   * has anything) and a new one starts.
   */
  setContext(layout: string, assist: boolean): InputStatsMsg | null {
    if (layout === this.layout && assist === this.assist) return null
    const done = this.take()
    this.layout = layout
    this.assist = assist
    return done
  }

  /** The window so far, or null when nothing was counted; starts a new window either way. */
  take(): InputStatsMsg | null {
    const now = this.now()
    const msg: InputStatsMsg | null = this.empty
      ? null
      : {
          t: 'input_stats',
          window_ms: Math.max(0, Math.round(now - this.startedAt)),
          layout: this.layout,
          assist: this.assist,
          counts: this.counts,
          ...(this.deadAt.length ? { dead_at: this.deadAt } : {}),
        }
    this.counts = {}
    this.deadAt = []
    this.startedAt = now
    return msg
  }
}

/** "gamepad:landscape" for a tree holding a gamepad preset; "custom:…" for an app's own layout. */
export function layoutName(tree: Node | null, landscape: boolean): string {
  const orientation = landscape ? 'landscape' : 'portrait'
  if (!tree) return `none:${orientation}`
  const find = (n: Node, depth: number): string | null => {
    if (depth > 32) return null
    if (n.type === 'preset' && typeof n.props?.name === 'string') return n.props.name.slice(0, 32)
    for (const c of n.children ?? []) {
      const hit = find(c, depth + 1)
      if (hit) return hit
    }
    return null
  }
  return `${find(tree, 0) ?? 'custom'}:${orientation}`
}

const ASSIST_KEY = 'yui_pad_touch_assist'

/** The Home sheet's Touch assist switch; on unless this phone turned it off. */
export function assistEnabled(): boolean {
  try {
    return localStorage.getItem(ASSIST_KEY) !== 'off'
  } catch {
    return true
  }
}

export function setAssistEnabled(on: boolean) {
  try {
    if (on) localStorage.removeItem(ASSIST_KEY)
    else localStorage.setItem(ASSIST_KEY, 'off')
  } catch {
    // private mode: the switch just won't stick
  }
}
