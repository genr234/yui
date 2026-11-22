import { describe, expect, test } from 'bun:test'
import { findAimCodes, parseAimCode, regionsToRead, type SeenCode } from './aimCodes'
import { kioskCodes, readShot, shoot, standAt } from './scanScene'

const SLOW = 30_000

describe('aim code', () => {
  const spots: Array<[string, Parameters<typeof shoot>[0]]> = [
    ['square in front, 1.5 m', { at: standAt(1.5, 0) }],
    ['45° to the left', { at: standAt(1.4, -45) }],
    ['60° to the right', { at: standAt(2.2, 60) }],
    ['to the side and below (a child)', { at: standAt(1.2, 35, -25) }],
    ['far back, 4 m', { at: standAt(4, -20) }],
    ['phone rolled 25°, aimed off-centre', { at: standAt(1.5, 25), roll: 25, look: [0.3, -0.2] }],
    ['phone held sideways (picture turned 90°)', { at: standAt(1.5, -30), roll: 90 }],
    ['landscape frame, noisy', { at: standAt(1.6, -30), w: 1280, h: 720, noise: 10 }],
  ]
  for (const [name, o] of spots) {
    test(
      `${name}: read, corners within a few pixels`,
      () => {
        const shot = shoot({ w: 720, h: 1280, ...o })
        const code = readShot(shot)
        expect(code).not.toBeNull()
        const err = Math.max(...code!.image.map((p, i) => Math.hypot(p[0] - shot.truth.image[i][0], p[1] - shot.truth.image[i][1])))
        expect(err).toBeLessThan(4)
      },
      SLOW,
    )
  }

  test('once seen, the next frame reads around it', () => {
    const frame = { w: 720, h: 1280 }
    const last: SeenCode['image'] = [
      [300, 600],
      [400, 600],
      [400, 700],
      [300, 700],
    ]
    const [first] = regionsToRead(frame, last, 0)
    expect(first).toMatchObject({ sx: 260, sy: 560, sw: 180, sh: 180, dw: 180, dh: 180 })
    // Never upscaled, never bigger than READ_MAX.
    for (const r of regionsToRead({ w: 3024, h: 4032 }, null, 1)) expect(Math.max(r.dw, r.dh)).toBeLessThanOrEqual(1024)
  })

  test('nothing of ours in the frame: nothing found', () => {
    const shot = shoot({ w: 360, h: 640, at: standAt(1.5, 0), look: [0, -6] })
    expect(findAimCodes(shot.rgba, shot.w, shot.h)).toEqual([])
  })

  test('only our codes count, and the text is where the code is', () => {
    expect(parseAimCode('https://example.com')).toBeNull()
    expect(parseAimCode('YUIAIM:0.1:0.2:0.3')).toBeNull()
    expect(parseAimCode('YUIAIM:0.5:0.2:0.1:0.3')).toBeNull() // x1 < x0
    expect(parseAimCode('YUIAIM:-0.900:-0.600:-0.300:0.600')).toEqual({ x0: -0.9, y0: -0.6, x1: -0.3, y1: 0.6 })
    for (const c of kioskCodes()) expect(parseAimCode(c.text)).not.toBeNull()
  })
})
