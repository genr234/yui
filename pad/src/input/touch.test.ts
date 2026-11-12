import { describe, expect, test } from 'bun:test'
import { decodeTouch, seqNewer, TOUCH_SIZE } from '@yui/protocol'
import { RELEASE_GAP_MS, TOUCH_MS, TouchSender, type TouchPoints } from './touch'
import { FakeClock } from '../testing'

const UP = { down: false, x: 0, y: 0 }
const at = (x: number, y: number) => ({ down: true, x, y })

function setup() {
  const clock = new FakeClock()
  const frames: ArrayBuffer[] = []
  const sender = new TouchSender((f) => frames.push(f), clock)
  return { clock, frames, sender, decoded: () => frames.map((f) => decodeTouch(f)) }
}

describe('TouchSender', () => {
  test('sends 20-byte frames on change and at 60 Hz while down', () => {
    const s = setup()
    s.sender.update([at(100, 200), UP])
    expect(s.frames.length).toBe(1)
    expect(s.frames[0]!.byteLength).toBe(TOUCH_SIZE)
    s.clock.advance(TOUCH_MS * 3 + 1)
    expect(s.frames.length).toBe(4)
    expect(s.decoded().every((f) => f.points[0].down && f.points[0].x === 100)).toBe(true)
  })

  test('a move sends at once and does not restart the cadence', () => {
    const s = setup()
    s.sender.update([at(1, 1), UP])
    s.clock.advance(5)
    s.sender.update([at(2, 2), UP])
    expect(s.frames.length).toBe(2)
    s.clock.advance(TOUCH_MS - 5 + 0.5)
    expect(s.frames.length).toBe(3) // the tick that was already due
  })

  test('an unchanged update sends nothing', () => {
    const s = setup()
    s.sender.update([at(1, 1), UP])
    s.sender.update([at(1, 1), UP])
    expect(s.frames.length).toBe(1)
  })

  test('the release frame goes out 3 times, 16 ms apart, then stops', () => {
    const s = setup()
    s.sender.update([at(9, 9), UP])
    s.clock.advance(1)
    const before = s.frames.length
    s.sender.update([UP, UP])
    expect(s.frames.length).toBe(before + 1)
    s.clock.advance(RELEASE_GAP_MS)
    s.clock.advance(RELEASE_GAP_MS)
    s.clock.advance(RELEASE_GAP_MS * 4)
    const rel = s.decoded().slice(before)
    expect(rel.length).toBe(3)
    expect(rel.every((f) => !f.points[0].down && !f.points[1].down && f.points[0].x === 0)).toBe(true)
    expect(s.sender.running).toBe(false)
    expect(s.clock.pending).toBe(0)
  })

  test('a point that is up is sent as 0,0 and two points are kept apart', () => {
    const s = setup()
    s.sender.update([at(10, 20), { down: false, x: 55, y: 66 }])
    s.sender.update([at(10, 20), at(30, 40)])
    const f = s.decoded()
    expect(f[0]!.points).toEqual([at(10, 20), UP])
    expect(f[1]!.points).toEqual([at(10, 20), at(30, 40)])
  })

  test('seq has its own series, increases, and is anchored to wall time', () => {
    const s = setup()
    s.sender.update([at(1, 1), UP])
    s.clock.advance(100)
    const seqs = s.decoded().map((f) => f.seq)
    for (let i = 1; i < seqs.length; i++) expect(seqNewer(seqs[i]!, seqs[i - 1]!)).toBe(true)
    expect(seqs[0]).toBe(Math.floor(s.clock.wallT / 16) & 0xffff)
  })

  test('disabled: nothing is sent; disabling lifts the fingers locally', () => {
    const s = setup()
    s.sender.setEnabled(false)
    s.sender.update([at(1, 1), UP])
    expect(s.frames.length).toBe(0)
    s.sender.setEnabled(true)
    s.sender.update([at(1, 1), UP])
    expect(s.frames.length).toBe(1)
    s.sender.setEnabled(false)
    expect(s.sender.running).toBe(false)
  })

  test('cancel releases', () => {
    const s = setup()
    s.sender.update([at(1, 1), UP] as TouchPoints)
    s.sender.cancel()
    expect(s.decoded().at(-1)!.points[0].down).toBe(false)
  })
})
