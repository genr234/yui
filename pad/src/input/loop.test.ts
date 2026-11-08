import { describe, expect, test } from 'bun:test'
import { decodeInput, seqNewer, INPUT_SIZE } from '@yui/protocol'
import { InputModel } from './model'
import { FRAME_MS, InputLoop } from './loop'
import { FakeClock } from '../testing'

function setup() {
  const clock = new FakeClock()
  const model = new InputModel()
  const frames: ArrayBuffer[] = []
  const loop = new InputLoop(model, (f) => frames.push(f), clock)
  return { clock, model, frames, loop, decoded: () => frames.map((f) => decodeInput(f)) }
}

describe('InputModel', () => {
  test('buttons OR across holders, release by key', () => {
    const m = new InputModel()
    m.hold('a', 1)
    m.hold('b', 1 | 4)
    m.release('b')
    expect(m.buttons()).toBe(1)
    m.release('a')
    expect(m.buttons()).toBe(0)
    expect(m.touching()).toBe(false)
  })

  test('sticks map to sides, +y up, clamped to i16 range', () => {
    const m = new InputModel()
    m.stick('s1', 'left', 1, -1)
    m.stick('s2', 'right', -0.5, 2)
    const f = m.frame()
    expect([f.lx, f.ly, f.rx, f.ry]).toEqual([32767, -32767, -16383, 32767])
    m.stickRelease('s1')
    expect(m.frame().lx).toBe(0)
  })

  test('touch pointer overrides the motion pointer while touched', () => {
    const m = new InputModel()
    m.motion([0, 0, 1], [1, 2, 3], [0.5, 0])
    expect(m.frame().px).toBe(16384)
    m.pointer('tp', -1, 1)
    expect([m.frame().px, m.frame().py]).toEqual([-32767, 32767])
    m.pointerRelease('tp')
    expect(m.frame().px).toBe(16384)
    expect(m.frame().accel).toEqual([0, 0, 1])
  })
})

describe('InputLoop', () => {
  test('idle until something is held', () => {
    const s = setup()
    s.loop.setEnabled(true)
    s.clock.advance(1000)
    expect(s.frames.length).toBe(0)
    expect(s.loop.running).toBe(false)
  })

  test('press sends at once, then ~60 Hz, then one final frame on release', () => {
    const s = setup()
    s.model.hold('btn', 1)
    expect(s.frames.length).toBe(1) // immediate
    expect(s.decoded()[0].buttons).toBe(1)
    s.clock.advance(1000)
    const held = s.frames.length
    expect(held).toBeGreaterThanOrEqual(59)
    expect(held).toBeLessThanOrEqual(62)
    s.model.release('btn')
    expect(s.frames.length).toBe(held + 1)
    expect(s.decoded().at(-1)!.buttons).toBe(0)
    expect(s.loop.running).toBe(false)
    s.clock.advance(500)
    expect(s.frames.length).toBe(held + 1)
  })

  test('every frame is 48 bytes with increasing seq', () => {
    const s = setup()
    s.model.stick('st', 'left', 0.5, 0.5)
    s.clock.advance(200)
    const d = s.decoded()
    expect(s.frames.every((f) => f.byteLength === INPUT_SIZE)).toBe(true)
    for (let i = 1; i < d.length; i++) expect(seqNewer(d[i].seq, d[i - 1].seq)).toBe(true)
    expect(d[0].lx).toBe(16384)
  })

  test('seq is anchored to wall time so a reloaded pad is not stale', () => {
    const a = setup()
    a.model.hold('b', 1)
    const first = a.decoded()[0].seq
    // Same wall clock 10 s later, fresh loop (page reload): its seq is newer.
    const b = setup()
    b.clock.wallT = a.clock.wallT + 10_000
    const loop2 = new InputLoop(b.model, (f) => b.frames.push(f), b.clock)
    b.model.hold('b', 1)
    expect(seqNewer(decodeInput(b.frames.at(-1)!).seq, first)).toBe(true)
    loop2.dispose()
  })

  test('button edges while running go out immediately (not before MIN_GAP)', () => {
    const s = setup()
    s.model.stick('st', 'left', 1, 0)
    s.clock.advance(FRAME_MS * 2 + 5)
    const n = s.frames.length
    s.model.hold('b', 2)
    expect(s.frames.length).toBe(n + 1)
    s.model.hold('c', 4) // same instant: waits for the tick
    expect(s.frames.length).toBe(n + 1)
  })

  test('a motion layout streams without touches; turning it off sends a final frame', () => {
    const s = setup()
    s.loop.setStreaming(true)
    s.clock.advance(500)
    expect(s.frames.length).toBeGreaterThanOrEqual(29)
    const n = s.frames.length
    s.loop.setStreaming(false)
    expect(s.frames.length).toBe(n + 1)
    s.clock.advance(500)
    expect(s.frames.length).toBe(n + 1)
  })

  test('disabled (no input channel) sends nothing', () => {
    const s = setup()
    s.loop.setEnabled(false)
    s.model.hold('b', 1)
    s.clock.advance(200)
    expect(s.frames.length).toBe(0)
    s.loop.setEnabled(true)
    expect(s.frames.length).toBe(1)
  })
})
