import jsQR from 'jsqr'
import type { Pt } from '../platform/viewpoint'

/**
 * Reads the QR code the kiosk shows on its game during the calibration's
 * scan step (platform sdk/calibrate-targets.ts draws it). The code says
 * where on the game it sits: `YUIAIM:<x0>:<y0>:<x1>:<y1>` is its top-left
 * and bottom-right corner in game coordinates (−1…1, +y down). So every
 * corner jsQR finds is a known point of the game, nothing else in the room
 * can pass for one, and the phone needs no idea of the game's shape.
 */

export const AIM_CODE_PREFIX = 'YUIAIM:'

/** A code's corners (TL, TR, BR, BL) on the game and in the frame. */
export interface SeenCode {
  text: string
  plane: Pt[]
  image: Pt[]
}

/** The game rectangle a code's text names, or null when it isn't one of ours. */
export function parseAimCode(text: string): { x0: number; y0: number; x1: number; y1: number } | null {
  if (!text.startsWith(AIM_CODE_PREFIX)) return null
  const parts = text.slice(AIM_CODE_PREFIX.length).split(':').map(Number)
  if (parts.length !== 4 || !parts.every((n) => Number.isFinite(n) && Math.abs(n) <= 1.5)) return null
  const [x0, y0, x1, y1] = parts
  if (!(x1 > x0 && y1 > y0)) return null
  return { x0, y0, x1, y1 }
}

/**
 * The aim codes in an RGBA frame, at most `max` (the kiosk shows one). jsQR
 * reads one code per pass, so each code found is painted over before
 * looking again.
 */
export function findAimCodes(rgba: Uint8ClampedArray, w: number, h: number, max = 1): SeenCode[] {
  const out: SeenCode[] = []
  for (let i = 0; i < max; i++) {
    const code = jsQR(rgba, w, h, { inversionAttempts: 'dontInvert' })
    if (!code) break
    const l = code.location
    const image: Pt[] = [l.topLeftCorner, l.topRightCorner, l.bottomRightCorner, l.bottomLeftCorner].map((p) => [p.x, p.y])
    const rect = parseAimCode(code.data)
    if (rect && !out.some((c) => c.text === code.data)) {
      out.push({
        text: code.data,
        plane: [
          [rect.x0, rect.y0],
          [rect.x1, rect.y0],
          [rect.x1, rect.y1],
          [rect.x0, rect.y1],
        ],
        image,
      })
    }
    if (i + 1 < max) blank(rgba, w, h, image)
  }
  return out
}

/** Paints a found code (and a margin around it) white, so the next pass finds another. */
function blank(rgba: Uint8ClampedArray, w: number, h: number, quad: Pt[]) {
  const cx = quad.reduce((a, p) => a + p[0], 0) / 4
  const cy = quad.reduce((a, p) => a + p[1], 0) / 4
  const grown = quad.map(([x, y]) => [cx + (x - cx) * 1.25, cy + (y - cy) * 1.25] as Pt)
  const xs = grown.map((p) => p[0])
  const ys = grown.map((p) => p[1])
  const x0 = Math.max(0, Math.floor(Math.min(...xs)))
  const x1 = Math.min(w - 1, Math.ceil(Math.max(...xs)))
  const y0 = Math.max(0, Math.floor(Math.min(...ys)))
  const y1 = Math.min(h - 1, Math.ceil(Math.max(...ys)))
  // Inside the convex quad: on the same side of all four edges.
  const side = (a: Pt, b: Pt, x: number, y: number) => (b[0] - a[0]) * (y - a[1]) - (b[1] - a[1]) * (x - a[0])
  for (let y = y0; y <= y1; y++) {
    for (let x = x0; x <= x1; x++) {
      let pos = 0
      let neg = 0
      for (let k = 0; k < 4; k++) {
        const s = side(grown[k], grown[(k + 1) % 4], x + 0.5, y + 0.5)
        if (s > 0) pos++
        else if (s < 0) neg++
      }
      if (pos && neg) continue
      const j = (y * w + x) * 4
      rgba[j] = rgba[j + 1] = rgba[j + 2] = 255
    }
  }
}

/** All the codes' points, for viewpointFromScan. */
export function scanPoints(codes: readonly SeenCode[]): { plane: Pt[]; image: Pt[] } {
  return { plane: codes.flatMap((c) => c.plane), image: codes.flatMap((c) => c.image) }
}

/** A part of the camera frame to read, and the size to read it at. */
export interface Region {
  sx: number
  sy: number
  sw: number
  sh: number
  /** Pixels to read it at (≤ the region: never upscaled). */
  dw: number
  dh: number
}

/** Regions are read at most this big: jsQR takes ~50 ms for 1024² on a phone. */
export const READ_MAX = 1024

function region(frame: { w: number; h: number }, cx: number, cy: number, sw: number, sh: number): Region {
  sw = Math.min(frame.w, Math.max(64, sw))
  sh = Math.min(frame.h, Math.max(64, sh))
  const sx = Math.round(Math.max(0, Math.min(frame.w - sw, cx - sw / 2)))
  const sy = Math.round(Math.max(0, Math.min(frame.h - sh, cy - sh / 2)))
  const k = Math.min(1, READ_MAX / Math.max(sw, sh))
  return { sx, sy, sw: Math.round(sw), sh: Math.round(sh), dw: Math.round(sw * k), dh: Math.round(sh * k) }
}

/**
 * Where to look in a frame, best first. Codes are small in the picture
 * (a kiosk a couple of metres away fills a fraction of it), so reading the
 * whole frame shrunk loses them: around where the code was last seen, and
 * the middle (where the player aims), are read at the camera's own
 * resolution. `turn` alternates the fallbacks between frames.
 */
export function regionsToRead(frame: { w: number; h: number }, last: Pt[] | null, turn: number): Region[] {
  const out: Region[] = []
  if (last) {
    const xs = last.map((p) => p[0])
    const ys = last.map((p) => p[1])
    const w = Math.max(...xs) - Math.min(...xs)
    const h = Math.max(...ys) - Math.min(...ys)
    const s = Math.max(w, h) * 1.8
    out.push(region(frame, (Math.max(...xs) + Math.min(...xs)) / 2, (Math.max(...ys) + Math.min(...ys)) / 2, s, s))
  }
  const middle = region(frame, frame.w / 2, frame.h / 2, frame.w * 0.55, frame.h * 0.55)
  const whole = region(frame, frame.w / 2, frame.h / 2, frame.w, frame.h)
  out.push(...(turn % 2 ? [whole, middle] : [middle, whole]))
  return out
}

/** A code found in a region, in frame pixels. */
export function toFrame(code: SeenCode, r: Region): SeenCode {
  const kx = r.sw / r.dw
  const ky = r.sh / r.dh
  return { ...code, image: code.image.map(([x, y]) => [r.sx + x * kx, r.sy + y * ky] as Pt) }
}
