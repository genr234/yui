import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { faceSpots, findFace, toGray } from './face'
import { clusterDetections, runCascade, unpackCascade, type Detection } from './pico'

const cascade = new Uint8Array(readFileSync(new URL('./facefinder.bin', import.meta.url)))

describe('face finder', () => {
  test('the cascade loads and finds nothing in a flat or noisy frame', () => {
    const classify = unpackCascade(cascade)
    const size = 120
    const flat = { pixels: new Uint8Array(size * size).fill(128), nrows: size, ncols: size, ldim: size }
    expect(findFace(flat, classify)).toBeNull()

    let seed = 7
    const noise = new Uint8Array(size * size).map(() => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) & 255)
    expect(findFace({ ...flat, pixels: noise }, classify)).toBeNull()
  })

  test('the search covers every size and stays inside the image', () => {
    const seen: number[] = []
    const image = { pixels: new Uint8Array(100 * 100), nrows: 100, ncols: 100, ldim: 100 }
    runCascade(
      image,
      (r, c, s) => {
        expect(r - s / 2).toBeGreaterThanOrEqual(0)
        expect(c + s / 2).toBeLessThanOrEqual(101)
        if (!seen.includes(s)) seen.push(s)
        return -1
      },
      { shiftfactor: 0.1, minsize: 25, maxsize: 100, scalefactor: 1.1 },
    )
    expect(seen[0]).toBe(25)
    expect(seen.length).toBeGreaterThan(10)
  })

  test('overlapping detections merge into one face', () => {
    const dets: Detection[] = [
      [50, 50, 40, 3],
      [51, 49, 42, 5],
      [10, 90, 12, 1],
    ]
    const clusters = clusterDetections(dets, 0.2)
    expect(clusters.length).toBe(2)
    expect(clusters[0][3]).toBe(8)
    expect(clusters[0][0]).toBeCloseTo(50.5)
  })

  test('gray conversion weighs green most', () => {
    const rgba = new Uint8ClampedArray([255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 100, 100, 100, 255])
    expect([...toGray(rgba, 2, 2).pixels]).toEqual([51, 178, 25, 100])
  })

  test('eyes sit above the nose, the mouth below, mirrored about the centre', () => {
    const s = faceSpots({ x: 0.5, y: 0.5, size: 0.6 })
    expect(s.eyes[0].x + s.eyes[1].x).toBeCloseTo(1)
    expect(s.eyes[0].y).toBeLessThan(s.nose.y)
    expect(s.nose.y).toBeLessThan(s.mouth.y)
    expect(s.eyes[0].r).toBeGreaterThan(0.05)
  })
})
