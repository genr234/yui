import { describe, expect, test } from 'bun:test'
import { FaceTracker } from './tracker'
import type { Classifier, GrayImage } from './pico'

const W = 192
const H = 144
const image: GrayImage = { pixels: new Uint8Array(W * H), nrows: H, ncols: W, ldim: W }

/** A fake detector: scores windows close to the planted faces (pixels: row, col, size). */
function scene(faces: { r: number; c: number; s: number }[]) {
  let calls = 0
  const classify: Classifier = (r, c, s) => {
    calls++
    for (const f of faces) {
      if (Math.abs(r - f.r) < f.s * 0.12 && Math.abs(c - f.c) < f.s * 0.12 && Math.abs(Math.log(s / f.s)) < 0.12) return 2
    }
    return -1
  }
  return { classify, faces, calls: () => calls }
}

function run(t: FaceTracker, frames: number) {
  let out = null
  for (let i = 0; i < frames; i++) out = t.update(image)
  return out
}

describe('face tracker', () => {
  test('a full search is spread over frames, then the face is followed', () => {
    const sc = scene([{ r: 100, c: 99, s: 37 }])
    const t = new FaceTracker(sc.classify)
    expect(t.update(image)).toBeNull()
    expect(t.update(image)).toBeNull()
    expect(t.update(image)).toBeNull()
    const f = t.update(image)!
    expect(f.x).toBeCloseTo(99 / W, 1)
    expect(f.y).toBeCloseTo(100 / H, 1)
    expect(t.tracking).toBe(true)
  })

  test('following scans far less than searching', () => {
    const sc = scene([{ r: 100, c: 99, s: 37 }])
    const t = new FaceTracker(sc.classify)
    run(t, 4)
    const afterSearch = sc.calls()
    run(t, 4)
    expect(sc.calls() - afterSearch).toBeLessThan(afterSearch / 2)
  })

  test('follows a moving face, smoothly', () => {
    const sc = scene([{ r: 72, c: 96, s: 36 }])
    const t = new FaceTracker(sc.classify)
    run(t, 4)
    for (let i = 0; i < 12; i++) {
      sc.faces[0].c += 3 // leaning right
      t.update(image)
    }
    expect(t.update(image)!.x * W).toBeGreaterThan(125)
  })

  test('a missed frame keeps the face; a long miss searches again', () => {
    const sc = scene([{ r: 72, c: 96, s: 36 }])
    const t = new FaceTracker(sc.classify)
    const found = run(t, 4)!
    const saved = sc.faces.pop()!
    expect(t.update(image)).toEqual(found)
    expect(t.update(image)).toEqual(found)
    expect(t.update(image)).toBeNull()
    expect(t.tracking).toBe(false)
    sc.faces.push(saved)
    expect(run(t, 4)).not.toBeNull()
  })

  test('the nearest person plays, and a passer-by does not take over', () => {
    const sc = scene([
      { r: 60, c: 40, s: 24 },
      { r: 80, c: 120, s: 48 },
    ])
    const t = new FaceTracker(sc.classify)
    const f = run(t, 4)!
    expect(f.x * W).toBeCloseTo(120, -1)
    sc.faces.push({ r: 70, c: 30, s: 60 }) // someone bigger walks in far away
    expect(run(t, 3)!.x * W).toBeCloseTo(120, -1)
  })
})
