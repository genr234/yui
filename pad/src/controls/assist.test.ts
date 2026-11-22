import { describe, expect, test } from 'bun:test'
import { ClusterDriver, DpadDriver, clusterPick, dpadDirs, type ClusterTarget, FLICKER_MS } from './drivers'
import type { ControlSink, StatKey } from './sink'
import { BIT } from './sink'
import { recordingSink } from '../testing'

// The face diamond as the gamepad preset lays it out: 40 px radius, 84 px cells.
const R = 40
const N: ClusterTarget = { id: 'N', bits: 1 << BIT.North, x: 84, y: 0, r: R }
const W: ClusterTarget = { id: 'W', bits: 1 << BIT.West, x: 0, y: 84, r: R }
const E: ClusterTarget = { id: 'E', bits: 1 << BIT.East, x: 168, y: 84, r: R }
const S: ClusterTarget = { id: 'S', bits: 1 << BIT.South, x: 84, y: 168, r: R }
const DIAMOND = [N, W, E, S]
const sorted = (ids: string[]) => [...ids].sort()

function assisted(assist: boolean) {
  const r = recordingSink()
  const stats: StatKey[] = []
  let on = assist
  const sink: ControlSink = { ...r.sink, assist: () => on, stat: (k) => stats.push(k) }
  const count = (k: StatKey) => stats.filter((s) => s === k).length
  return { ...r, sink, stats, count, setAssist: (v: boolean) => (on = v) }
}

describe('clusterPick', () => {
  test('without assist only a button under the finger counts', () => {
    expect(clusterPick(DIAMOND, 84, 0)).toEqual(['N'])
    expect(clusterPick(DIAMOND, 84 + 39, 0)).toEqual(['N'])
    expect(clusterPick(DIAMOND, 126, 42)).toEqual([]) // the N–E gap
    // A held finger keeps its button wherever it goes (pointer capture).
    expect(clusterPick(DIAMOND, 168, 84, ['N'])).toEqual(['N'])
  })

  test('assist: the middle of a gap between neighbours holds both', () => {
    expect(sorted(clusterPick(DIAMOND, 126, 42, [], true))).toEqual(['E', 'N'])
    expect(sorted(clusterPick(DIAMOND, 42, 126, [], true))).toEqual(['S', 'W'])
  })

  test('assist: the rest of a gap goes to the nearer button', () => {
    expect(clusterPick(DIAMOND, 120, 30, [], true)).toEqual(['N'])
    expect(clusterPick(DIAMOND, 140, 70, [], true)).toEqual(['E'])
  })

  test('assist: opposite buttons never chord, the centre and far away press nothing', () => {
    expect(clusterPick(DIAMOND, 84, 84, [], true)).toEqual([])
    expect(clusterPick(DIAMOND, 400, 400, [], true)).toEqual([])
    // Just off W towards the centre: the nearest, not W+E.
    expect(clusterPick(DIAMOND, 44, 84, [], true)).toEqual(['W'])
  })

  test('assist: what a finger holds is sticky', () => {
    // Off the N–E line, a little nearer E: too close to call for a new
    // finger, but a finger already holding either keeps it.
    expect(clusterPick(DIAMOND, 149, 23, [], true)).toEqual([])
    expect(clusterPick(DIAMOND, 149, 23, ['N'], true)).toEqual(['N'])
    expect(clusterPick(DIAMOND, 149, 23, ['E'], true)).toEqual(['E'])
    // A held chord survives a wobble that would not start one.
    expect(clusterPick(DIAMOND, 114, 30, [], true)).toEqual(['N'])
    expect(sorted(clusterPick(DIAMOND, 114, 30, ['N', 'E'], true))).toEqual(['E', 'N'])
  })
})

describe('cluster driver', () => {
  test('assist: roll from one button through the chord to the next', () => {
    const r = assisted(true)
    const c = new ClusterDriver(r.sink, 'face')
    c.setTargets(DIAMOND)
    c.down(1, 84, 0)
    expect(r.input.buttons()).toBe(N.bits)
    c.move(1, 126, 42)
    expect(r.input.buttons()).toBe(N.bits | E.bits)
    c.move(1, 168, 84)
    expect(r.input.buttons()).toBe(E.bits)
    expect([...c.held]).toEqual(['E'])
    c.up(1)
    expect(r.input.buttons()).toBe(0)
    expect(r.count('chord')).toBe(1)
    expect(r.count('press')).toBe(2)
    expect(r.feedbackCount()).toBe(2)
  })

  test('assist: a touch in a gap is counted and resolved', () => {
    const r = assisted(true)
    const c = new ClusterDriver(r.sink, 'face')
    c.setTargets(DIAMOND)
    c.down(1, 120, 30)
    expect(r.input.buttons()).toBe(N.bits)
    c.up(1)
    c.down(2, 84, 84)
    expect(r.input.buttons()).toBe(0)
    expect(r.count('gap')).toBe(1)
    expect(r.count('gap_miss')).toBe(1)
  })

  test('without assist it is four plain buttons, and slides are only counted', () => {
    const r = assisted(false)
    const c = new ClusterDriver(r.sink, 'face')
    c.setTargets(DIAMOND)
    c.down(1, 84, 0)
    c.move(1, 126, 42)
    c.move(1, 168, 84) // onto E: still N, one slide
    c.move(1, 170, 86)
    expect(r.input.buttons()).toBe(N.bits)
    expect(r.count('slide')).toBe(1)
    c.up(1)
    c.down(2, 126, 42)
    expect(r.input.buttons()).toBe(0)
    expect(r.count('gap_miss')).toBe(1)
  })

  test('two fingers hold their own buttons', () => {
    const r = assisted(true)
    const c = new ClusterDriver(r.sink, 'face')
    c.setTargets(DIAMOND)
    c.down(1, 84, 168)
    c.down(2, 0, 84)
    expect(r.input.buttons()).toBe(S.bits | W.bits)
    c.up(1)
    expect(r.input.buttons()).toBe(W.bits)
    c.reset()
    expect(r.input.buttons()).toBe(0)
  })

  test('a quick tap then another button right after is a correction', () => {
    const r = assisted(true)
    const c = new ClusterDriver(r.sink, 'face')
    c.setTargets(DIAMOND)
    c.down(1, 84, 0)
    r.clock.advance(60)
    c.up(1)
    r.clock.advance(100)
    c.down(2, 168, 84)
    c.up(2)
    expect(r.count('correction')).toBe(1)
    // Same button again, or too late: not a correction.
    r.clock.advance(50)
    c.down(3, 168, 84)
    r.clock.advance(60)
    c.up(3)
    r.clock.advance(500)
    c.down(4, 84, 0)
    expect(r.count('correction')).toBe(1)
  })
})

const at = (deg: number, r = 1): [number, number] => [Math.cos((deg * Math.PI) / 180) * r, -Math.sin((deg * Math.PI) / 180) * r]

describe('dpad assist', () => {
  test('without assist the split is unchanged', () => {
    expect(dpadDirs(...at(27), true)).toEqual(['up', 'right'])
    expect(dpadDirs(...at(20), true)).toEqual(['right'])
    expect(dpadDirs(...at(0, 0.25), true, ['right'])).toEqual([])
  })

  test('diagonals are narrower', () => {
    expect(dpadDirs(...at(27), true, [], true)).toEqual(['right'])
    expect(dpadDirs(...at(35), true, [], true)).toEqual(['up', 'right'])
    expect(dpadDirs(...at(63), true, [], true)).toEqual(['up'])
    expect(dpadDirs(...at(200), true, [], true)).toEqual(['left'])
    expect(dpadDirs(...at(225), true, [], true)).toEqual(['down', 'left'])
  })

  test('the held direction keeps some extra angle, and the centre lets go later', () => {
    expect(dpadDirs(...at(38), true, ['right'], true)).toEqual(['right'])
    expect(dpadDirs(...at(42), true, ['right'], true)).toEqual(['up', 'right'])
    expect(dpadDirs(...at(22), true, ['up', 'right'], true)).toEqual(['up', 'right'])
    expect(dpadDirs(...at(0, 0.25), true, ['right'], true)).toEqual(['right'])
    expect(dpadDirs(...at(0, 0.25), true, [], true)).toEqual([])
    expect(dpadDirs(...at(0, 0.15), true, ['right'], true)).toEqual([])
    // 4-way keeps its direction up to 55° off.
    expect(dpadDirs(...at(140), false, ['up'], true)).toEqual(['up'])
    expect(dpadDirs(...at(140), false, ['up'])).toEqual(['left'])
  })

  test('driver: assist steadies a wobbling thumb; flicker and centre touches are counted', () => {
    const plain = assisted(false)
    const d = new DpadDriver(plain.sink, 'dp')
    d.down(1, ...at(20))
    plain.clock.advance(30)
    d.move(1, ...at(25)) // over the 22.5° line: up-right
    plain.clock.advance(30)
    d.move(1, ...at(20)) // back: right, a flicker
    expect(plain.count('dpad_flicker')).toBe(1)
    expect(plain.count('dpad_change')).toBe(3)

    const helped = assisted(true)
    const d2 = new DpadDriver(helped.sink, 'dp')
    d2.down(1, ...at(20))
    helped.clock.advance(30)
    d2.move(1, ...at(25))
    helped.clock.advance(30)
    d2.move(1, ...at(20))
    expect(d2.current).toEqual(['right'])
    expect(helped.count('dpad_change')).toBe(1)
    expect(helped.count('dpad_flicker')).toBe(0)

    d2.up(1)
    d2.down(2, 0.1, 0.1)
    expect(helped.count('dpad_deadzone')).toBe(1)
  })

  test('a slow return is not a flicker', () => {
    const r = assisted(false)
    const d = new DpadDriver(r.sink, 'dp')
    d.down(1, ...at(20))
    r.clock.advance(30)
    d.move(1, ...at(25))
    r.clock.advance(FLICKER_MS + 10)
    d.move(1, ...at(20))
    expect(r.count('dpad_flicker')).toBe(0)
  })
})
