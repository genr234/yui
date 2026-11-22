import { describe, expect, test } from 'bun:test'
import { detectScanMarkers, findMarkers, toGray } from './markers'
import { shoot, standAt } from './scanScene'

const worst = (found: number[][], truth: number[][]) =>
  Math.max(...found.map((p, i) => Math.hypot(p[0] - truth[i][0], p[1] - truth[i][1])))

describe('scan markers', () => {
  const spots: Array<[string, Parameters<typeof shoot>[0]]> = [
    ['square in front, 1.5 m', { at: standAt(1.5, 0) }],
    ['45° to the left', { at: standAt(1.4, -45) }],
    ['60° to the right, close', { at: standAt(1.0, 60) }],
    ['to the side and below (a child)', { at: standAt(1.2, 35, -25) }],
    ['far back, small in the frame', { at: standAt(3.2, -20) }],
    ['phone rolled 20°, aimed off-centre', { at: standAt(1.5, 25), roll: 20, look: [0.3, -0.2] }],
    ['landscape frame, noisy', { at: standAt(1.6, -30), w: 480, h: 360, noise: 14 }],
  ]
  for (const [name, o] of spots) {
    test(`${name}: all four found, in order, within a pixel or two`, () => {
      const shot = shoot(o)
      const found = detectScanMarkers(shot.gray, shot.w, shot.h)
      expect(found).not.toBeNull()
      expect(worst(found!, shot.truth)).toBeLessThan(2)
    })
  }

  test('the label, the dimmed shell, a ceiling light and a window are not markers', () => {
    const shot = shoot({ at: standAt(1.5, 10) })
    expect(findMarkers(shot.gray, shot.w, shot.h)).toHaveLength(4)
  })

  test('nothing to find: no quad', () => {
    const shot = shoot({ at: standAt(1.5, 0), look: [0, -6] }) // pointing at the ceiling
    expect(detectScanMarkers(shot.gray, shot.w, shot.h)).toBeNull()
  })

  test('toGray: luma', () => {
    expect([...toGray([255, 255, 255, 255, 0, 0, 0, 255, 255, 0, 0, 255], 3, 1)]).toEqual([255, 0, 76])
  })
})
