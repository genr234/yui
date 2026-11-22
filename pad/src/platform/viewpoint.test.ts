import { describe, expect, test } from 'bun:test'
import type { Vec3 } from '@yui/protocol'
import {
  ViewPointer,
  describeViewpoint,
  homography,
  parseViewpoint,
  viewpointFromScan,
  type Pt,
  type Viewpoint,
} from './viewpoint'
import { PointerIntegrator } from './motion'
import { planeToWorld, readShot, see, shoot, standAt } from '../input/scanScene'

const DEG = 180 / Math.PI
const UP: Vec3 = [0, -1, 0] // world: +y is down
const norm = (v: Vec3): Vec3 => {
  const n = Math.hypot(v[0], v[1], v[2])
  return [v[0] / n, v[1] / n, v[2] / n]
}
const dot = (a: Vec3, b: Vec3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
const cross = (a: Vec3, b: Vec3): Vec3 => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]]

/** Azimuth and elevation (degrees) of the line from `at` to a plane point, measured from the line to the game's centre. */
function aimAngles(at: Vec3, target: Pt): { yaw: number; pitch: number } {
  const dir = (p: Pt) => {
    const w = planeToWorld(p)
    return norm([w[0] - at[0], w[1] - at[1], w[2] - at[2]])
  }
  const c = dir([0, 0])
  const ahead = norm([c[0] - dot(c, UP) * UP[0], c[1] - dot(c, UP) * UP[1], c[2] - dot(c, UP) * UP[2]])
  const right = cross(ahead, UP)
  const v = dir(target)
  const az = Math.atan2(dot(v, right), dot(v, ahead))
  return { yaw: -az * DEG, pitch: (Math.asin(dot(v, UP)) - Math.asin(dot(c, UP))) * DEG }
}

/** Turns a pointer from the centre to `target` the way a wrist would: yaw and pitch together, in small steps. */
function aim(p: { recenter(): void; update(g: Vec3, dt: number): [number, number] }, at: Vec3, target: Pt) {
  const { yaw, pitch } = aimAngles(at, target)
  p.recenter()
  const n = 20
  let out: [number, number] = [0, 0]
  for (let i = 0; i < n; i++) out = p.update([(pitch / n) * 10, (yaw / n) * 10, 0], 100)
  return out
}

function scan(shot: ReturnType<typeof see>, image: Pt[] = shot.truth.image, focal = shot.focal): Viewpoint {
  const vp = viewpointFromScan(shot.truth.plane, image, { w: shot.w, h: shot.h }, focal)
  if (typeof vp === 'string') throw new Error(vp)
  return vp
}

const TARGETS: Pt[] = [
  [0.9, 0],
  [-0.9, 0],
  [0, -0.9],
  [0, 0.9],
  [0.7, 0.7],
  [-0.7, -0.7],
  [-0.6, 0.8],
]

describe('viewpoint', () => {
  test('homography maps the four points', () => {
    const src: Pt[] = [
      [0, 0],
      [1, 0],
      [1, 1],
      [0, 1],
    ]
    const dst: Pt[] = [
      [10, 20],
      [110, 30],
      [100, 140],
      [5, 120],
    ]
    const H = homography(src, dst)!
    for (let i = 0; i < 4; i++) {
      const [x, y] = src[i]
      const w = H[6] * x + H[7] * y + H[8]
      expect((H[0] * x + H[1] * y + H[2]) / w).toBeCloseTo(dst[i][0], 6)
      expect((H[3] * x + H[4] * y + H[5]) / w).toBeCloseTo(dst[i][1], 6)
    }
  })

  for (const [side, above] of [
    [0, 0],
    [-50, 0],
    [40, 15],
    [65, -20],
  ]) {
    test(`standing ${side}° to the side, ${above}° up: where they stood, and every aim lands where it points`, () => {
      const at = standAt(1.6, side, above)
      const vp = scan(see({ at }))
      const d = describeViewpoint(vp)
      expect(d.side).toBeCloseTo(side, 3)
      expect(d.above).toBeCloseTo(above, 3)
      expect(d.widths).toBeCloseTo(1.6 / 0.6, 3)
      const p = new ViewPointer(vp)
      for (const t of TARGETS) {
        const got = aim(p, at, t)
        expect(got[0]).toBeCloseTo(t[0], 3)
        expect(got[1]).toBeCloseTo(t[1], 3)
      }
    })
  }

  test('from the code as read off a camera frame, and a camera 6% wider than assumed, still within a few percent', () => {
    const at = standAt(1.5, -45, 10)
    const shot = shoot({ at, focal: 0.665, w: 720, h: 1280 })
    const read = readShot(shot)!
    const fit = viewpointFromScan(read.plane, read.image, { w: shot.w, h: shot.h }, 0.71 * Math.max(shot.w, shot.h))
    if (typeof fit === 'string') throw new Error(fit)
    const vp = fit
    expect(Math.abs(describeViewpoint(vp).side - -45)).toBeLessThan(4)
    const p = new ViewPointer(vp)
    for (const t of TARGETS) {
      const got = aim(p, at, t)
      expect(Math.abs(got[0] - t[0])).toBeLessThan(0.1)
      expect(Math.abs(got[1] - t[1])).toBeLessThan(0.1)
    }
  }, 30_000)

  test('the corner squares keep the aim right from further back, where the code alone drifts', () => {
    // Reading noise: a QR reader's corners wander a couple of pixels; a square's centre far less.
    let seed = 3
    const gauss = () => {
      let t = 0
      for (let i = 0; i < 6; i++) {
        seed = (seed * 1664525 + 1013904223) >>> 0
        t += seed / 2 ** 32
      }
      return (t - 3) * Math.SQRT2
    }
    const worst = (at: Vec3, withSquares: boolean) => {
      const shot = see({ at, w: 1080, h: 1920 })
      const n = withSquares ? 8 : 4
      let out = 0
      for (let k = 0; k < 60; k++) {
        const image = shot.truth.image.slice(0, n).map(([x, y], i) => {
          const s = i < 4 ? 2.5 : 0.7
          return [x + gauss() * s, y + gauss() * s] as Pt
        })
        const vp = viewpointFromScan(shot.truth.plane.slice(0, n), image, { w: 1080, h: 1920 })
        if (typeof vp === 'string') return Infinity
        const p = new ViewPointer(vp)
        for (const t of TARGETS) {
          const g = aim(p, at, t)
          out = Math.max(out, Math.hypot(g[0] - t[0], g[1] - t[1]))
        }
      }
      return out / 2 // as a share of the screen
    }
    const far = standAt(3, -30)
    expect(worst(far, false)).toBeGreaterThan(0.12) // the code alone: more than an eighth of the screen off
    expect(worst(far, true)).toBeLessThan(0.06)
    expect(worst(standAt(1.5, 45), true)).toBeLessThan(0.04)
  })

  test('seen from the side, the far edge is not where turn-angle aiming puts it', () => {
    // What the edge-aiming calibration measures from 50° to the left, and where its pointer lands.
    const at = standAt(1.2, -50)
    const right = aimAngles(at, [1, 0])
    const top = aimAngles(at, [0, -1])
    const linear = new PointerIntegrator()
    linear.setRanges(Math.abs(right.yaw), Math.abs(top.pitch))
    const left = aim(linear, at, [-0.5, 0])
    expect(Math.abs(left[0] - -0.5)).toBeGreaterThan(0.12) // it lands at about −0.66
    // The scanned viewpoint gets it.
    const p = new ViewPointer(scan(see({ at })))
    expect(aim(p, at, [-0.5, 0])[0]).toBeCloseTo(-0.5, 3)
  })

  test('aim stops at the edges and comes straight back', () => {
    const at = standAt(1.4, 30)
    const p = new ViewPointer(scan(see({ at })))
    p.recenter()
    for (let i = 0; i < 30; i++) p.update([0, -40, 0], 100) // far past the right edge
    expect(p.value()[0]).toBeCloseTo(1, 2)
    const { yaw } = aimAngles(at, [0.5, 0])
    const back = aimAngles(at, [1, 0]).yaw - yaw
    for (let i = 0; i < 10; i++) p.update([0, -back, 0], 100)
    expect(p.value()[0]).toBeCloseTo(0.5, 1)
    for (let i = 0; i < 30; i++) p.update([-40, 0, 0], 100) // down past the bottom
    expect(p.value()[1]).toBeCloseTo(1, 2)
    p.recenter()
    expect(p.value()).toEqual([0, 0])
  })

  test('ranges: degrees from the centre to the right and top edges', () => {
    const at = standAt(1.5, 0)
    const r = new ViewPointer(scan(see({ at }))).ranges()
    expect(r.yaw).toBeCloseTo(Math.atan(0.3 / 1.5) * DEG, 1)
    expect(r.pitch).toBeCloseTo(Math.atan((0.3 * 9) / 16 / 1.5) * DEG, 1)
  })

  test('refuses what is not a phone looking at the kiosk', () => {
    const shot = see({ at: standAt(1.5, 20) })
    const frame = { w: shot.w, h: shot.h }
    // Corners mixed up: not a picture of a flat game.
    const im = shot.truth.image
    expect(typeof viewpointFromScan(shot.truth.plane, [im[1], im[0], im[2], im[3]], frame)).toBe('string')
    // Edge-on.
    const edge = see({ at: standAt(1.5, 80) })
    expect(viewpointFromScan(edge.truth.plane, edge.truth.image, frame)).toBe('scan-angle')
  })

  test('up is the game\'s own up, however the phone is turned', () => {
    for (const roll of [0, 30, 90, 180]) {
      const shot = see({ at: standAt(1.5, -30), roll })
      const vp = scan(shot)
      for (let i = 0; i < 3; i++) expect(vp.up[i]).toBeCloseTo(shot.up[i], 6)
    }
  })

  test('stored viewpoints are checked', () => {
    const vp = scan(see({ at: standAt(1.5, 10) }))
    expect(parseViewpoint(JSON.parse(JSON.stringify(vp)))).toEqual(vp)
    expect(parseViewpoint({ m: [1, 2, 3], up: [0, 1, 0] })).toBeNull()
    expect(parseViewpoint({ m: [0, 0, 0, 0, 0, 0, 0, 0, 0], up: [0, 1, 0] })).toBeNull()
    expect(parseViewpoint({ m: vp.m, up: [0, 0, 0] })).toBeNull()
    expect(parseViewpoint(null)).toBeNull()
  })
})
