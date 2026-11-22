import { describe, expect, test } from 'bun:test'
import type { Vec3 } from '@yui/protocol'
import {
  ViewPointer,
  cameraUp,
  describeViewpoint,
  homography,
  parseViewpoint,
  viewpointFromScan,
  type Pt,
  type Viewpoint,
} from './viewpoint'
import { PointerIntegrator } from './motion'
import { detectScanMarkers } from '../input/markers'
import { planeToWorld, shoot, standAt, type Shot } from '../input/scanScene'

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

function scan(shot: Shot, corners: Pt[] = shot.truth, focal = shot.focal): Viewpoint {
  const vp = viewpointFromScan(corners, { w: shot.w, h: shot.h }, shot.up, focal)
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
      const shot = shoot({ at })
      const vp = scan(shot)
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

  test('from the detected markers, and a camera 6% wider than assumed, still within a few percent', () => {
    const at = standAt(1.5, -45, 10)
    const shot = shoot({ at, focal: 0.665 })
    const corners = detectScanMarkers(shot.gray, shot.w, shot.h)!
    const vp = scan(shot, corners, 0.71 * Math.max(shot.w, shot.h))
    expect(describeViewpoint(vp).side).toBeCloseTo(-45, -1) // ±5°
    const p = new ViewPointer(vp)
    for (const t of TARGETS) {
      const got = aim(p, at, t)
      expect(Math.abs(got[0] - t[0])).toBeLessThan(0.08)
      expect(Math.abs(got[1] - t[1])).toBeLessThan(0.08)
    }
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
    const p = new ViewPointer(scan(shoot({ at })))
    expect(aim(p, at, [-0.5, 0])[0]).toBeCloseTo(-0.5, 3)
  })

  test('aim stops at the edges and comes straight back', () => {
    const at = standAt(1.4, 30)
    const p = new ViewPointer(scan(shoot({ at })))
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
    const r = new ViewPointer(scan(shoot({ at }))).ranges()
    expect(r.yaw).toBeCloseTo(Math.atan(0.3 / 1.5) * DEG, 1)
    expect(r.pitch).toBeCloseTo(Math.atan((0.3 * 9) / 16 / 1.5) * DEG, 1)
  })

  test('refuses what is not a phone looking at the kiosk', () => {
    const shot = shoot({ at: standAt(1.5, 20) })
    const frame = { w: shot.w, h: shot.h }
    // Upside down: gravity says the game's top is down.
    expect(viewpointFromScan(shot.truth, frame, [-shot.up[0], -shot.up[1], -shot.up[2]])).toBe('scan-tilted')
    // Corners mixed up.
    const mixed = [shot.truth[1], shot.truth[0], shot.truth[2], shot.truth[3]]
    expect(typeof viewpointFromScan(mixed, frame, shot.up)).toBe('string')
    // Edge-on.
    const edge = shoot({ at: standAt(1.5, 80) })
    expect(viewpointFromScan(edge.truth, frame, edge.up)).toBe('scan-angle')
  })

  test('up in the camera from up in the device, for each screen angle', () => {
    // Upright portrait phone, camera at the kiosk: device +y is up, which is the picture's −y.
    expect(cameraUp([0, 1, 0], 0)).toEqual([0, -1, -0])
    // Landscape: at 90 the displayed top is the device's +x, at 270 its −x.
    expect(cameraUp([1, 0, 0], 90)[1]).toBeCloseTo(-1)
    expect(cameraUp([-1, 0, 0], 270)[1]).toBeCloseTo(-1)
  })

  test('stored viewpoints are checked', () => {
    const vp = scan(shoot({ at: standAt(1.5, 10) }))
    expect(parseViewpoint(JSON.parse(JSON.stringify(vp)))).toEqual(vp)
    expect(parseViewpoint({ m: [1, 2, 3], up: [0, 1, 0] })).toBeNull()
    expect(parseViewpoint({ m: [0, 0, 0, 0, 0, 0, 0, 0, 0], up: [0, 1, 0] })).toBeNull()
    expect(parseViewpoint({ m: vp.m, up: [0, 0, 0] })).toBeNull()
    expect(parseViewpoint(null)).toBeNull()
  })
})
