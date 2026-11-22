// Test helper (bun test only): a phone camera looking at a kiosk showing the
// calibration's scan markers, rendered from any spot in the room.
import type { Vec3 } from '@yui/protocol'
import { SCAN_MARKERS, type Pt } from '../platform/viewpoint'

/** The kiosk: a 1080×1920 portrait screen, 0.6 m wide, its game in the 16:9 band in the middle. */
export const KIOSK = { w: 1080, h: 1920, metres: 0.6 }
export const AREA = { x: 0, y: (1920 - 607.5) / 2, w: 1080, h: 607.5 }
const M_PER_PX = KIOSK.metres / KIOSK.w

/** Marker side and hole, kiosk px (platform sdk/calibrate-targets.ts: 1.6 × the smaller inset). */
const MARKER = 1.6 * Math.min(AREA.w * 0.1, AREA.h * 0.15)

/** World: metres, origin at the game's centre, +x right, +y down, +z into the screen. */
export function planeToWorld([u, v]: Pt): Vec3 {
  return [((u * AREA.w) / 2) * M_PER_PX, ((v * AREA.h) / 2) * M_PER_PX, 0]
}

function worldToKioskPx(p: Vec3): Pt {
  return [p[0] / M_PER_PX + AREA.x + AREA.w / 2, p[1] / M_PER_PX + AREA.y + AREA.h / 2]
}

/** What the kiosk shows during the scan step, 0–255, at kiosk px. */
function kioskPixel(x: number, y: number): number {
  if (x < 0 || y < 0 || x >= KIOSK.w || y >= KIOSK.h) return -1
  const inArea = y >= AREA.y && y < AREA.y + AREA.h
  if (!inArea) {
    // The dimmed shell: some faint UI blocks and text-like stripes.
    const block = (Math.floor(x / 160) + Math.floor(y / 120)) % 3 === 0
    return block ? 70 : 38
  }
  for (const [u, v] of SCAN_MARKERS) {
    const mx = AREA.x + ((u + 1) / 2) * AREA.w
    const my = AREA.y + ((v + 1) / 2) * AREA.h
    const dx = Math.abs(x - mx)
    const dy = Math.abs(y - my)
    if (dx < MARKER / 2 && dy < MARKER / 2) return dx < MARKER / 4 && dy < MARKER / 4 ? 12 : 250
  }
  // The player's label pill in the middle: a bright pill with dark letters (one with a hole).
  const lx = x - (AREA.x + AREA.w / 2)
  const ly = y - (AREA.y + AREA.h / 2)
  if (Math.abs(lx) < 150 && Math.abs(ly) < 22) {
    const ring = Math.hypot(lx + 100, ly) // an "o"
    if (ring > 7 && ring < 12) return 30
    return Math.abs(lx) % 24 < 5 ? 30 : 200
  }
  return 12
}

/** The room behind and around the kiosk: a wall, a bright ceiling light, a window. */
function roomPixel(dir: Vec3): number {
  const el = -dir[1] / Math.hypot(dir[0], dir[1], dir[2])
  const az = Math.atan2(dir[0], dir[2])
  if (el > 0.45 && Math.abs(az - 0.3) < 0.15) return 255 // light
  if (Math.abs(el - 0.1) < 0.12 && az < -0.6 && az > -1.1) return 230 // window
  return 120 + 40 * Math.sin(az * 3) + 30 * el
}

export interface Shot {
  gray: Uint8Array
  w: number
  h: number
  /** Where the markers' centres really are in the frame, TL TR BR BL. */
  truth: Pt[]
  /** Up, in the camera frame. */
  up: Vec3
  /** Camera position in metres (world above). */
  at: Vec3
  focal: number
}

export interface ShotOptions {
  /** Where the camera stands, metres from the game's centre (+x right, −y up, −z out in front). */
  at: Vec3
  /** Degrees the phone is rolled about its lens. */
  roll?: number
  /** Where in the game the camera points (plane coordinates), default its centre. */
  look?: Pt
  w?: number
  h?: number
  /** Focal length as a fraction of the long side. */
  focal?: number
  noise?: number
  seed?: number
}

const norm = (v: Vec3): Vec3 => {
  const n = Math.hypot(v[0], v[1], v[2])
  return [v[0] / n, v[1] / n, v[2] / n]
}
const cross = (a: Vec3, b: Vec3): Vec3 => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]]
const dot = (a: Vec3, b: Vec3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2]

/** The camera's axes (right, down, forward) in world coordinates. */
export function cameraAxes(at: Vec3, look: Pt = [0, 0], rollDeg = 0): [Vec3, Vec3, Vec3] {
  const t = planeToWorld(look)
  const f = norm([t[0] - at[0], t[1] - at[1], t[2] - at[2]])
  let r = norm(cross([0, 1, 0], f))
  let d = cross(f, r)
  const a = (rollDeg * Math.PI) / 180
  ;[r, d] = [
    [0, 1, 2].map((i) => Math.cos(a) * r[i] + Math.sin(a) * d[i]) as Vec3,
    [0, 1, 2].map((i) => -Math.sin(a) * r[i] + Math.cos(a) * d[i]) as Vec3,
  ]
  return [r, d, f]
}

/** Renders the frame the phone would see (2×2 supersampled, a little blur and sensor noise). */
export function shoot(o: ShotOptions): Shot {
  const w = o.w ?? 360
  const h = o.h ?? 480
  const F = (o.focal ?? 0.75) * Math.max(w, h)
  const [r, d, f] = cameraAxes(o.at, o.look, o.roll)
  const cx = w / 2
  const cy = h / 2
  const img = new Float32Array(w * h)
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      let acc = 0
      for (const [ox, oy] of [
        [0.25, 0.25],
        [0.75, 0.25],
        [0.25, 0.75],
        [0.75, 0.75],
      ]) {
        const a = (x + ox - cx) / F
        const b = (y + oy - cy) / F
        const dir: Vec3 = [r[0] * a + d[0] * b + f[0], r[1] * a + d[1] * b + f[1], r[2] * a + d[2] * b + f[2]]
        let v = -1
        if (dir[2] > 1e-6) {
          const t = -o.at[2] / dir[2]
          const p: Vec3 = [o.at[0] + t * dir[0], o.at[1] + t * dir[1], 0]
          const [kx, ky] = worldToKioskPx(p)
          v = kioskPixel(kx, ky)
        }
        acc += v < 0 ? roomPixel(dir) : v
      }
      img[y * w + x] = acc / 4
    }
  }
  // A slight blur (the lens, and the screen's own glow), then noise.
  const gray = new Uint8Array(w * h)
  let seed = o.seed ?? 1
  const rand = () => {
    seed = (seed * 1664525 + 1013904223) >>> 0
    return seed / 2 ** 32
  }
  const noise = o.noise ?? 6
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      let s = 0
      let n = 0
      for (let j = -1; j <= 1; j++)
        for (let i = -1; i <= 1; i++) {
          const xx = x + i
          const yy = y + j
          if (xx < 0 || yy < 0 || xx >= w || yy >= h) continue
          const k = i === 0 && j === 0 ? 4 : i === 0 || j === 0 ? 2 : 1
          s += img[yy * w + xx] * k
          n += k
        }
      const v = s / n + (rand() + rand() + rand() - 1.5) * noise
      gray[y * w + x] = Math.max(0, Math.min(255, Math.round(v)))
    }
  }
  return { gray, w, h, ...seeMarkers(o.at, [r, d, f], w, h, F), at: o.at, focal: F }
}

/** Where a camera at `at` with axes (right, down, forward) sees the markers' centres, and which way is up for it. */
export function seeMarkers(at: Vec3, [r, d, f]: [Vec3, Vec3, Vec3], w: number, h: number, F: number): { truth: Pt[]; up: Vec3 } {
  const project = (p: Vec3): Pt => {
    const q: Vec3 = [p[0] - at[0], p[1] - at[1], p[2] - at[2]]
    const z = dot(q, f)
    return [w / 2 + (F * dot(q, r)) / z, h / 2 + (F * dot(q, d)) / z]
  }
  const worldUp: Vec3 = [0, -1, 0]
  return {
    truth: SCAN_MARKERS.map((m) => project(planeToWorld(m))),
    up: [dot(worldUp, r), dot(worldUp, d), dot(worldUp, f)],
  }
}

/** A spot `metres` from the game's centre, `side` degrees to the right of straight ahead and `above` degrees up. */
export function standAt(metres: number, side: number, above = 0): Vec3 {
  const s = (side * Math.PI) / 180
  const a = (above * Math.PI) / 180
  return [metres * Math.cos(a) * Math.sin(s), -metres * Math.sin(a), -metres * Math.cos(a) * Math.cos(s)]
}
