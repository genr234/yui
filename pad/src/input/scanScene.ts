// Test helper (bun test only): a phone camera looking at a kiosk showing the
// calibration's scan codes, rendered from any spot in the room.
import type { Vec3 } from '@yui/protocol'
import qrcode from 'qrcode-generator'
import type { Pt } from '../platform/viewpoint'
import { findAimCodes, findSquare, regionsToRead, squareRegions, toFrame, type Region, type SeenCode } from './aimCodes'

/** The kiosk: a 1080×1920 portrait screen, 0.6 m wide, its game in the 16:9 band in the middle. */
export const KIOSK = { w: 1080, h: 1920, metres: 0.6 }
export const AREA = { x: 0, y: (1920 - 607.5) / 2, w: 1080, h: 607.5 }
const M_PER_PX = KIOSK.metres / KIOSK.w

/** The kiosk's code and squares, laid out as platform sdk/calibrate-targets.ts aimCode() does. */
export function kioskCodes(area = AREA) {
  const cx = area.x + area.w / 2
  const cy = area.y + area.h / 2
  const short = Math.min(area.w, area.h)
  const r3 = (v: number) => Math.round(v * 1000) / 1000
  const mx = 0.9
  const my = 0.85
  const square = Math.min(0.1 * short, 1.6 * Math.min(((1 - mx) * area.w) / 2, ((1 - my) * area.h) / 2))
  const along = area.w >= area.h ? mx * (area.w / 2) : my * (area.h / 2)
  const size = Math.min((0.96 * short) / 1.28, (along - square / 2 - 12) / 0.64)
  const x1 = r3(size / 2 / (area.w / 2))
  const y1 = r3(size / 2 / (area.h / 2))
  const fmt = (v: number) => v.toFixed(3).replace(/^0/, '')
  const text = `YUIAIM2:${fmt(x1)}:${fmt(y1)}:${fmt(mx)}:${fmt(my)}`
  const qr = qrcode(0, 'H')
  qr.addData(text, 'Alphanumeric')
  qr.make()
  const n = qr.getModuleCount()
  const squares: Pt[] = [
    [-mx, -my],
    [mx, -my],
    [mx, my],
    [-mx, my],
  ]
  return [
    {
      text,
      plane: [
        [-x1, -y1],
        [x1, -y1],
        [x1, y1],
        [-x1, y1],
      ] as Pt[],
      squares,
      squarePx: square,
      x: cx - (x1 * area.w) / 2,
      y: cy - (y1 * area.h) / 2,
      w: x1 * area.w,
      h: y1 * area.h,
      n,
      dark: (r: number, c: number) => qr.isDark(r, c),
    },
  ]
}
const CODES = kioskCodes()

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
    // The dimmed shell: some faint UI blocks.
    const block = (Math.floor(x / 160) + Math.floor(y / 120)) % 3 === 0
    return block ? 70 : 38
  }
  for (const c of CODES) {
    for (const [u, v] of c.squares) {
      const sx = AREA.x + ((u + 1) / 2) * AREA.w
      const sy = AREA.y + ((v + 1) / 2) * AREA.h
      if (Math.abs(x - sx) < c.squarePx / 2 && Math.abs(y - sy) < c.squarePx / 2) return 245
    }
    const m = 0.14 * c.w // the white margin
    if (x < c.x - m || x >= c.x + c.w + m || y < c.y - m || y >= c.y + c.h + m) continue
    const col = Math.floor(((x - c.x) / c.w) * c.n)
    const row = Math.floor(((y - c.y) / c.h) * c.n)
    const inside = col >= 0 && row >= 0 && col < c.n && row < c.n
    // A player aiming at the middle meanwhile: a small dot on the code's centre.
    if (Math.hypot(x - (c.x + c.w / 2), y - (c.y + c.h / 2)) < 9) return 120
    return inside && c.dark(row, col) ? 20 : 245
  }
  // Player labels beside the code.
  const lx = x - (AREA.x + 90)
  const ly = y - (AREA.y + AREA.h / 2)
  if (Math.abs(lx) < 70 && Math.abs(ly) < 18) return Math.abs(lx) % 20 < 5 ? 30 : 200
  return 10
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
  rgba: Uint8ClampedArray
  w: number
  h: number
  /** Every code corner: where it is on the game, and where it really is in the frame. */
  truth: { plane: Pt[]; image: Pt[] }
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
  const w = o.w ?? 540
  const h = o.h ?? 960
  const F = (o.focal ?? 0.71) * Math.max(w, h)
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
  const rgba = new Uint8ClampedArray(w * h * 4)
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
      const g = Math.max(0, Math.min(255, Math.round(v)))
      const k = (y * w + x) * 4
      rgba[k] = rgba[k + 1] = rgba[k + 2] = g
      rgba[k + 3] = 255
    }
  }
  return { rgba, w, h, ...seeCodes(o.at, [r, d, f], w, h, F), at: o.at, focal: F }
}

/** Where a camera at `at` with axes (right, down, forward) sees every code corner, and which way is up for it. */
export function seeCodes(
  at: Vec3,
  [r, d, f]: [Vec3, Vec3, Vec3],
  w: number,
  h: number,
  F: number,
): { truth: { plane: Pt[]; image: Pt[] }; up: Vec3 } {
  const project = (p: Vec3): Pt => {
    const q: Vec3 = [p[0] - at[0], p[1] - at[1], p[2] - at[2]]
    const z = dot(q, f)
    return [w / 2 + (F * dot(q, r)) / z, h / 2 + (F * dot(q, d)) / z]
  }
  const worldUp: Vec3 = [0, -1, 0]
  return {
    // The code's corners, then the squares' centres.
    truth: {
      plane: CODES.flatMap((c) => [...c.plane, ...c.squares]),
      image: CODES.flatMap((c) => [...c.plane, ...c.squares].map((p) => project(planeToWorld(p)))),
    },
    up: [dot(worldUp, r), dot(worldUp, d), dot(worldUp, f)],
  }
}

/** A spot `metres` from the game's centre, `side` degrees to the right of straight ahead and `above` degrees up. */
export function standAt(metres: number, side: number, above = 0): Vec3 {
  const s = (side * Math.PI) / 180
  const a = (above * Math.PI) / 180
  return [metres * Math.cos(a) * Math.sin(s), -metres * Math.sin(a), -metres * Math.cos(a) * Math.cos(s)]
}

/** A region of a rendered shot, box-averaged as drawImage does when shrinking (RGBA). */
function readRegion(shot: Shot, r: Region): Uint8ClampedArray {
  const px = new Uint8ClampedArray(r.dw * r.dh * 4)
  const kx = r.sw / r.dw
  const ky = r.sh / r.dh
  for (let y = 0; y < r.dh; y++)
    for (let x = 0; x < r.dw; x++) {
      const x0 = Math.floor(r.sx + x * kx)
      const y0 = Math.floor(r.sy + y * ky)
      const x1 = Math.max(x0 + 1, Math.floor(r.sx + (x + 1) * kx))
      const y1 = Math.max(y0 + 1, Math.floor(r.sy + (y + 1) * ky))
      let s = 0
      let n = 0
      for (let yy = y0; yy < y1; yy++)
        for (let xx = x0; xx < x1; xx++) {
          s += shot.rgba[(yy * shot.w + xx) * 4]
          n++
        }
      const k = (y * r.dw + x) * 4
      px[k] = px[k + 1] = px[k + 2] = s / n
      px[k + 3] = 255
    }
  return px
}

/**
 * Reads a shot the way the scan screen reads the camera: region by region
 * until the code turns up, then each square where the code says it is.
 * Returns the code and every point found (code corners first, then squares).
 */
export function readShot(shot: Shot, last: Pt[] | null = null, turn = 0): { code: SeenCode; plane: Pt[]; image: Pt[] } | null {
  for (const r of regionsToRead({ w: shot.w, h: shot.h }, last, turn)) {
    const found = findAimCodes(readRegion(shot, r), r.dw, r.dh)
    if (!found.length) continue
    const code = toFrame(found[0], r)
    const plane = [...code.plane]
    const image = [...code.image]
    squareRegions(code, { w: shot.w, h: shot.h }).forEach((sr, i) => {
      if (!sr) return
      const px = readRegion(shot, sr)
      const gray = new Uint8Array(sr.dw * sr.dh)
      for (let k = 0; k < gray.length; k++) gray[k] = px[k * 4]
      const c = findSquare(gray, sr.dw, sr.dh)
      if (!c) return
      plane.push(code.squares[i])
      image.push([sr.sx + (c[0] * sr.sw) / sr.dw, sr.sy + (c[1] * sr.sh) / sr.dh])
    })
    return { code, plane, image }
  }
  return null
}

/** Where the code's corners are for a camera, without rendering a frame (for geometry tests). */
export function see(o: ShotOptions): Pick<Shot, 'truth' | 'up' | 'at' | 'focal' | 'w' | 'h'> {
  const w = o.w ?? 540
  const h = o.h ?? 960
  const F = (o.focal ?? 0.71) * Math.max(w, h)
  return { w, h, ...seeCodes(o.at, cameraAxes(o.at, o.look, o.roll), w, h, F), at: o.at, focal: F }
}
