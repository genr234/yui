import { describe, expect, test } from 'bun:test'
import { findAimCodes, findSquare, parseAimCode, regionsToRead, type SeenCode } from './aimCodes'
import { kioskCodes, readShot, shoot, standAt } from './scanScene'

const SLOW = 30_000

describe('aim code', () => {
  const spots: Array<[string, Parameters<typeof shoot>[0]]> = [
    ['square in front, 1.5 m', { at: standAt(1.5, 0) }],
    ['45° to the left', { at: standAt(1.4, -45) }],
    ['60° to the right (a 1080p frame, as phones give)', { at: standAt(2.2, 60), w: 1080, h: 1920 }],
    ['to the side and below (a child)', { at: standAt(1.2, 35, -25) }],
    ['far back, 4 m (1080p)', { at: standAt(4, -20), w: 1080, h: 1920 }],
    ['phone rolled 25°, aimed off-centre', { at: standAt(1.5, 25), roll: 25, look: [0.3, -0.2] }],
    ['phone held sideways (picture turned 90°)', { at: standAt(1.5, -30), roll: 90 }],
    ['landscape frame, noisy', { at: standAt(1.6, -30), w: 1280, h: 720, noise: 10 }],
  ]
  for (const [name, o] of spots) {
    test(
      `${name}: read, code corners within a few pixels, every square found within one`,
      () => {
        const shot = shoot({ w: 720, h: 1280, ...o })
        const read = readShot(shot)
        expect(read).not.toBeNull()
        const truthOf = (p: number[]) => shot.truth.image[shot.truth.plane.findIndex((q) => q[0] === p[0] && q[1] === p[1])]
        const err = (i: number) => {
          const t = truthOf(read!.plane[i])
          return Math.hypot(read!.image[i][0] - t[0], read!.image[i][1] - t[1])
        }
        for (let i = 0; i < 4; i++) expect(err(i)).toBeLessThan(4)
        expect(read!.plane.length).toBe(8)
        for (let i = 4; i < 8; i++) expect(err(i)).toBeLessThan(1)
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

  test('a square: its centre, or nothing when it is cut off, missing or not solid', () => {
    const win = (draw: (x: number, y: number) => number, n = 40) => {
      const g = new Uint8Array(n * n)
      for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) g[y * n + x] = draw(x + 0.5, y + 0.5)
      return g
    }
    const sq = (cx: number, cy: number, s: number) => (x: number, y: number) => (Math.abs(x - cx) < s && Math.abs(y - cy) < s ? 240 : 15)
    const c = findSquare(win(sq(21.3, 18.6, 6)), 40, 40)!
    expect(c[0]).toBeCloseTo(21.3, 0)
    expect(c[1]).toBeCloseTo(18.6, 0)
    expect(findSquare(win(sq(2, 20, 6)), 40, 40)).toBeNull() // cut by the window's edge
    expect(findSquare(win(() => 20), 40, 40)).toBeNull() // nothing there
    expect(findSquare(win((x, y) => (Math.abs(x - 20) < 8 && Math.abs(y - 20) < 8 && (Math.floor(x) + Math.floor(y)) % 2 ? 240 : 15)), 40, 40)).toBeNull() // a pattern, not a square
  })

  test('nothing of ours in the frame: nothing found', () => {
    const shot = shoot({ w: 360, h: 640, at: standAt(1.5, 0), look: [0, -6] })
    expect(findAimCodes(shot.rgba, shot.w, shot.h)).toEqual([])
  })

  test('only our codes count, and the text is where the code is', () => {
    expect(parseAimCode('https://example.com')).toBeNull()
    expect(parseAimCode('YUIAIM:0.1:0.2:0.3')).toBeNull()
    expect(parseAimCode('YUIAIM:0.5:0.2:0.1:0.3')).toBeNull() // x1 < x0
    // The first version: the code's corners, no squares.
    expect(parseAimCode('YUIAIM:-0.900:-0.600:-0.300:0.600')).toEqual({ x0: -0.9, y0: -0.6, x1: -0.3, y1: 0.6, squares: [] })
    expect(parseAimCode('YUIAIM2:.422:.750:.900:.850')).toEqual({
      x0: -0.422,
      y0: -0.75,
      x1: 0.422,
      y1: 0.75,
      squares: [
        [-0.9, -0.85],
        [0.9, -0.85],
        [0.9, 0.85],
        [-0.9, 0.85],
      ],
    })
    expect(parseAimCode('YUIAIM2:.422:.750:.900')).toBeNull()
    expect(parseAimCode('YUIAIM2:.422:.750:1.900:.850')).toBeNull()
    for (const c of kioskCodes()) expect(parseAimCode(c.text)).not.toBeNull()
  })
})
