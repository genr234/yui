import { describe, expect, test } from 'bun:test'
import { SurfaceTouches, contentRect, toU16 } from './surface'

describe('contentRect', () => {
  test('wider element: pillarbox', () => {
    expect(contentRect(1000, 480, 854, 480)).toEqual({ x: (1000 - 854) / 2, y: 0, w: 854, h: 480 })
  })
  test('taller element: letterbox', () => {
    const r = contentRect(427, 400, 854, 480)
    expect(r.x).toBe(0)
    expect(r.w).toBe(427)
    expect(r.h).toBeCloseTo(240)
    expect(r.y).toBeCloseTo(80)
  })
  test('unknown video size fills the element; empty element is empty', () => {
    expect(contentRect(300, 200, 0, 0)).toEqual({ x: 0, y: 0, w: 300, h: 200 })
    expect(contentRect(0, 200, 854, 480).w).toBe(0)
  })
})

describe('toU16', () => {
  const rect = { x: 0, y: 80, w: 427, h: 240 }
  test('corners and centre of the content rectangle', () => {
    expect(toU16(rect, 0, 80)).toEqual({ x: 0, y: 0 })
    expect(toU16(rect, 427, 320)).toEqual({ x: 65535, y: 65535 })
    const c = toU16(rect, 213.5, 200)
    expect(c.x).toBeGreaterThan(32766)
    expect(c.x).toBeLessThan(32769)
    expect(c.y).toBeGreaterThan(32766)
    expect(c.y).toBeLessThan(32769)
  })
  test('clamps outside the image', () => {
    expect(toU16(rect, -50, 10)).toEqual({ x: 0, y: 0 })
    expect(toU16(rect, 900, 999)).toEqual({ x: 65535, y: 65535 })
  })
})

describe('SurfaceTouches', () => {
  const rect = { x: 0, y: 80, w: 400, h: 240 }
  test('a down in the letterbox is ignored', () => {
    const t = new SurfaceTouches()
    expect(t.down(1, rect, 100, 40)).toBe(false)
    expect(t.count).toBe(0)
    expect(t.points()[0].down).toBe(false)
  })
  test('tracks two fingers, ignores a third, and releases by id', () => {
    const t = new SurfaceTouches()
    expect(t.down(1, rect, 0, 80)).toBe(true)
    expect(t.down(2, rect, 400, 320)).toBe(true)
    expect(t.down(3, rect, 200, 200)).toBe(false)
    expect(t.points()).toEqual([
      { down: true, x: 0, y: 0 },
      { down: true, x: 65535, y: 65535 },
    ])
    expect(t.up(1)).toBe(true)
    expect(t.points()[0]).toEqual({ down: false, x: 0, y: 0 })
    expect(t.down(3, rect, 200, 200)).toBe(true) // takes the freed slot 0
    expect(t.points()[0].down).toBe(true)
  })
  test('a move leaving the image is clamped, an unknown pointer is ignored', () => {
    const t = new SurfaceTouches()
    t.down(1, rect, 200, 200)
    expect(t.move(1, rect, 999, -20)).toBe(true)
    expect(t.points()[0]).toEqual({ down: true, x: 65535, y: 0 })
    expect(t.move(9, rect, 1, 1)).toBe(false)
    t.clear()
    expect(t.count).toBe(0)
  })
})
