import { describe, expect, test } from 'bun:test'
import { recordingSink } from '../testing'
import { CLEAR_CONFIRM_MS, SketchEngine, SketchState, catmullRom, fitAspect, normalize, scaleSize } from './sketch'

function setup(id = 's') {
  const r = recordingSink()
  const state = new SketchState()
  state.bind(id)
  return { ...r, state, eng: new SketchEngine(r.sink, id, state) }
}

describe('geometry', () => {
  test('normalize is 0–1 of the rect and clamps', () => {
    const rect = { left: 10, top: 20, width: 200, height: 100 }
    expect(normalize(110, 70, rect)).toEqual([0.5, 0.5])
    expect(normalize(-5, 500, rect)).toEqual([0, 1])
    expect(normalize(1, 1, { left: 0, top: 0, width: 0, height: 0 })).toEqual([0, 0])
  })
  test('fitAspect letterboxes 4:3', () => {
    expect(fitAspect(400, 300)).toEqual({ width: 400, height: 300 })
    expect(fitAspect(400, 1000)).toEqual({ width: 400, height: 300 })
    expect(fitAspect(1000, 300)).toEqual({ width: 400, height: 300 })
  })
  test('brush size scales with width', () => {
    expect(scaleSize(6, 360)).toBe(6)
    expect(scaleSize(6, 720)).toBe(12)
  })
  test('catmullRom passes through points with matching tangents', () => {
    const segs = catmullRom([
      [0, 0],
      [3, 0],
      [6, 3],
    ])
    expect(segs.length).toBe(2)
    expect(segs[0].to).toEqual([3, 0])
    expect(segs[0].c1).toEqual([0.5, 0])
    expect(segs[1].to).toEqual([6, 3])
    expect(catmullRom([[1, 1]])).toEqual([])
  })
})

describe('events', () => {
  test('first change goes out immediately, then batches at ≤30/s', () => {
    const { eng, sent, clock, feedbackCount } = setup()
    eng.begin('#f00', 6, 0.1, 0.2)
    expect(feedbackCount()).toBe(1)
    expect(sent).toEqual([{ t: 'event', control: 's', kind: 'change', value: { stroke: 1, color: '#f00', size: 6, points: [[0.1, 0.2, 0]] } }])
    clock.advance(5)
    eng.add(0.2, 0.2)
    clock.advance(5)
    eng.add(0.3, 0.2)
    expect(sent.length).toBe(1)
    clock.advance(30)
    expect(sent.length).toBe(2)
    const v = (sent[1] as any).value
    expect(v.points).toEqual([
      [0.2, 0.2, 5],
      [0.3, 0.2, 10],
    ])
  })

  test('never more than 30 change events in a second', () => {
    const { eng, sent, clock } = setup()
    eng.begin('#000', 4, 0, 0)
    for (let i = 1; i < 250; i++) {
      clock.advance(4)
      eng.add(i / 250, 0.5)
    }
    const changes = sent.filter((s: any) => s.kind === 'change').length
    expect(changes).toBeLessThanOrEqual(30)
    expect(changes).toBeGreaterThan(20)
  })

  test('batches hold at most 64 points', () => {
    const { eng, sent, clock } = setup()
    eng.begin('#000', 4, 0, 0)
    for (let i = 0; i < 150; i++) eng.add(0.5, 0.5)
    clock.advance(500)
    const sizes = sent.map((s: any) => s.value.points.length)
    expect(Math.max(...sizes)).toBe(64)
    expect(sizes.reduce((a, b) => a + b, 0)).toBe(151)
  })

  test('end sends submit after the last points, and stroke numbers count up', () => {
    const { eng, sent, clock } = setup()
    eng.begin('#000', 4, 0, 0)
    clock.advance(10)
    eng.add(1, 1)
    eng.end()
    expect(sent.map((s: any) => s.kind)).toEqual(['change'])
    clock.advance(40)
    expect(sent.map((s: any) => s.kind)).toEqual(['change', 'change', 'submit'])
    expect((sent[2] as any).value).toEqual({ stroke: 1 })
    eng.begin('#000', 4, 0, 0)
    clock.advance(40)
    expect((sent[3] as any).value.stroke).toBe(2)
  })

  test('t is ms since the stroke began', () => {
    const { eng, sent, clock } = setup()
    clock.advance(1000)
    eng.begin('#000', 4, 0, 0)
    clock.advance(40)
    eng.add(0.5, 0.5)
    clock.advance(40)
    expect((sent[1] as any).value.points[0][2]).toBe(40)
  })
})

describe('undo and clear', () => {
  test('undo pops the last stroke locally and sends tap undo', () => {
    const { eng, sent, state } = setup()
    eng.begin('#000', 4, 0, 0)
    eng.end()
    eng.begin('#000', 4, 1, 1)
    eng.end()
    expect(state.strokes.length).toBe(2)
    eng.undo()
    expect(state.strokes.length).toBe(1)
    expect(sent[sent.length - 1]).toEqual({ t: 'event', control: 's', kind: 'tap', value: 'undo' })
  })

  test('clear needs a second tap within 3 s', () => {
    const { eng, sent, clock, state } = setup()
    eng.begin('#000', 4, 0, 0)
    eng.end()
    const before = sent.length
    expect(eng.pressClear()).toBe(false)
    expect(eng.clearArmed).toBe(true)
    expect(state.strokes.length).toBe(1)
    clock.advance(CLEAR_CONFIRM_MS + 1)
    expect(eng.clearArmed).toBe(false)
    expect(eng.pressClear()).toBe(false)
    clock.advance(1000)
    expect(eng.pressClear()).toBe(true)
    expect(state.strokes.length).toBe(0)
    expect(sent.length).toBe(before + 1)
    expect(sent[sent.length - 1]).toEqual({ t: 'event', control: 's', kind: 'tap', value: 'clear' })
  })
})

describe('layout persistence', () => {
  test('same id keeps strokes, different id starts blank, counter resets per layout', () => {
    const { eng, state } = setup('a')
    eng.begin('#000', 4, 0, 0)
    eng.end()
    state.newLayout()
    state.bind('a')
    expect(state.strokes.length).toBe(1)
    expect(state.counter).toBe(0)
    state.bind('b')
    expect(state.strokes.length).toBe(0)
  })
})
