// Test helpers (bun test only): a fake clock and a recording control sink.
import type { EventKind, TextOp } from '@yui/protocol'
import { InputModel } from './input/model'
import type { ControlSink } from './controls/sink'

export class FakeClock {
  t = 0
  wallT = 1_700_000_000_000
  private seq = 0
  private timers = new Map<number, { at: number; fn: () => void; every?: number }>()

  now = () => this.t
  wall = () => this.wallT + this.t

  setTimeout = (fn: () => void, ms: number) => {
    const id = ++this.seq
    this.timers.set(id, { at: this.t + ms, fn })
    return id
  }
  clearTimeout = (h: unknown) => {
    this.timers.delete(h as number)
  }
  setInterval = (fn: () => void, ms: number) => {
    const id = ++this.seq
    this.timers.set(id, { at: this.t + ms, fn, every: ms })
    return id
  }
  clearInterval = (h: unknown) => {
    this.timers.delete(h as number)
  }
  get pending() {
    return this.timers.size
  }

  /** Advance time, firing due timers in order. */
  advance(ms: number) {
    const end = this.t + ms
    for (;;) {
      let next: [number, { at: number; fn: () => void; every?: number }] | null = null
      for (const e of this.timers) if (e[1].at <= end && (!next || e[1].at < next[1].at)) next = e
      if (!next) break
      const [id, timer] = next
      this.t = timer.at
      if (timer.every) timer.at += timer.every
      else this.timers.delete(id)
      timer.fn()
    }
    this.t = end
  }
}

export type Sent =
  | { t: 'event'; control: string; kind: EventKind; value?: unknown }
  | { t: 'text'; op: TextOp; text?: string }

export function recordingSink(clock = new FakeClock()) {
  const sent: Sent[] = []
  let feedback = 0
  const input = new InputModel()
  const sink: ControlSink = {
    input,
    event: (control, kind, value) => sent.push(value === undefined ? { t: 'event', control, kind } : { t: 'event', control, kind, value }),
    text: (op, text) => sent.push(text === undefined ? { t: 'text', op } : { t: 'text', op, text }),
    feedback: () => feedback++,
    now: clock.now,
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clearTimeout,
  }
  return { sink, sent, input, clock, feedbackCount: () => feedback }
}
