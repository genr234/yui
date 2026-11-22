import jsQR from 'jsqr'
import { homography, type Pt } from '../platform/viewpoint'

/**
 * Reads what the kiosk shows on its game during the calibration's scan step
 * (platform sdk/calibrate-targets.ts draws it): a QR code in the middle and
 * four white squares near the game's corners. The code's text says where
 * everything is, in game coordinates (−1…1, +y down):
 *
 *   `YUIAIM2:<cx>:<cy>:<mx>:<my>`  the code spans ±cx, ±cy; the squares'
 *                                  centres are at ±mx, ±my
 *   `YUIAIM:<x0>:<y0>:<x1>:<y1>`   (first version) the code's corners, no squares
 *
 * So every point found is a known point of the game, nothing else in the
 * room can pass for one, and the phone needs no idea of the game's shape.
 * The code alone is enough to work out the angle, but its corners are close
 * together; the squares, looked for only where the code says they must be,
 * spread the measurement across the whole game.
 */

export const AIM_CODE_PREFIX = 'YUIAIM2:'
const FIRST_PREFIX = 'YUIAIM:'

/** A code's corners (TL, TR, BR, BL) on the game and in the frame, and where its squares are on the game. */
export interface SeenCode {
  text: string
  plane: Pt[]
  image: Pt[]
  squares: Pt[]
}

export interface AimCodeText {
  x0: number
  y0: number
  x1: number
  y1: number
  /** The corner squares' centres on the game (TL, TR, BR, BL); none for the first version. */
  squares: Pt[]
}

/** What a code's text says, or null when it isn't one of ours. */
export function parseAimCode(text: string): AimCodeText | null {
  const nums = (rest: string, n: number) => {
    const parts = rest.split(':').map((p) => (p.trim() === '' ? NaN : Number(p)))
    return parts.length === n && parts.every((v) => Number.isFinite(v) && Math.abs(v) <= 1.5) ? parts : null
  }
  if (text.startsWith(AIM_CODE_PREFIX)) {
    const p = nums(text.slice(AIM_CODE_PREFIX.length), 4)
    if (!p) return null
    const [cx, cy, mx, my] = p
    if (!(cx > 0 && cy > 0 && mx > 0 && my > 0 && mx <= 1 && my <= 1)) return null
    return {
      x0: -cx,
      y0: -cy,
      x1: cx,
      y1: cy,
      squares: [
        [-mx, -my],
        [mx, -my],
        [mx, my],
        [-mx, my],
      ],
    }
  }
  if (text.startsWith(FIRST_PREFIX)) {
    const p = nums(text.slice(FIRST_PREFIX.length), 4)
    if (!p) return null
    const [x0, y0, x1, y1] = p
    if (!(x1 > x0 && y1 > y0)) return null
    return { x0, y0, x1, y1, squares: [] }
  }
  return null
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
        squares: rect.squares,
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

// ------------------------------------------------------------- the squares

/**
 * Where to look for each of a code's squares (frame pixels): the code's
 * corners say where its plane is, so each square's centre is predicted, and
 * a window a few squares wide around it is read at full resolution. The
 * window is a third of the gap to the next square, so it never reaches
 * another square or the code.
 */
export function squareRegions(code: SeenCode, frame: { w: number; h: number }): (Region | null)[] {
  if (!code.squares.length) return []
  const H = homography(code.plane, code.image)
  if (!H) return code.squares.map(() => null)
  const at = ([x, y]: Pt): Pt => {
    const w = H[6] * x + H[7] * y + H[8]
    return [(H[0] * x + H[1] * y + H[2]) / w, (H[3] * x + H[4] * y + H[5]) / w]
  }
  const centres = code.squares.map(at)
  return centres.map((c, i) => {
    const next = centres[(i + 1) % 4]
    const prev = centres[(i + 3) % 4]
    const gap = Math.min(Math.hypot(next[0] - c[0], next[1] - c[1]), Math.hypot(prev[0] - c[0], prev[1] - c[1]))
    const side = Math.max(16, gap / 3)
    if (c[0] - side / 2 < 0 || c[1] - side / 2 < 0 || c[0] + side / 2 > frame.w || c[1] + side / 2 > frame.h) return null
    const sw = Math.round(side)
    const k = Math.min(1, 192 / sw)
    const sx = Math.round(c[0] - sw / 2)
    const sy = Math.round(c[1] - sw / 2)
    return { sx, sy, sw, sh: sw, dw: Math.round(sw * k), dh: Math.round(sw * k) }
  })
}

/**
 * The centre of the white square in a small grey window, or null. The square
 * is the brightest thing there, on black: threshold halfway between the
 * window's dark and its brightest, then of the bright patches that are whole
 * (not cut by the window's edge: the room behind the kiosk can reach into a
 * window at the game's edge), solid, roughly square under perspective and
 * near the middle, take the biggest.
 */
export function findSquare(gray: Uint8Array, w: number, h: number): Pt | null {
  let hi = 0
  const hist = new Uint32Array(256)
  for (let i = 0; i < w * h; i++) {
    hist[gray[i]]++
    if (gray[i] > hi) hi = gray[i]
  }
  let lo = 0
  for (let acc = 0, v = 0; v < 256; v++) {
    acc += hist[v]
    if (acc >= w * h * 0.2) {
      lo = v
      break
    }
  }
  if (hi - lo < 60) return null // no contrast: nothing white here
  const t = (lo + hi) / 2
  const seen = new Uint8Array(w * h)
  type Blob = { n: number; m: number; sx: number; sy: number; x0: number; y0: number; x1: number; y1: number; edge: boolean }
  let best: Blob | null = null
  const stack: number[] = []
  for (let start = 0; start < w * h; start++) {
    if (seen[start] || gray[start] <= t) continue
    const blob: Blob = { n: 0, m: 0, sx: 0, sy: 0, x0: w, y0: h, x1: -1, y1: -1, edge: false }
    stack.push(start)
    seen[start] = 1
    while (stack.length) {
      const i = stack.pop()!
      const x = i % w
      const y = (i - x) / w
      // Weighted by brightness above the threshold: the soft edges count for less.
      const wt = gray[i] - t
      blob.n++
      blob.sx += (x + 0.5) * wt
      blob.sy += (y + 0.5) * wt
      blob.m += wt
      if (x < blob.x0) blob.x0 = x
      if (x > blob.x1) blob.x1 = x
      if (y < blob.y0) blob.y0 = y
      if (y > blob.y1) blob.y1 = y
      if (x === 0 || y === 0 || x === w - 1 || y === h - 1) blob.edge = true
      for (const j of [i - 1, i + 1, i - w, i + w]) {
        if (j < 0 || j >= w * h || seen[j] || gray[j] <= t) continue
        if ((j === i - 1 && x === 0) || (j === i + 1 && x === w - 1)) continue
        seen[j] = 1
        stack.push(j)
      }
    }
    if (blob.edge || blob.n < 6) continue
    const bw = blob.x1 - blob.x0 + 1
    const bh = blob.y1 - blob.y0 + 1
    if (blob.n / (bw * bh) < 0.5 || bw / bh > 4 || bh / bw > 4) continue
    const cx = blob.sx / (blob.m || 1)
    const cy = blob.sy / (blob.m || 1)
    if (Math.hypot(cx - w / 2, cy - h / 2) > 0.3 * Math.max(w, h)) continue
    if (!best || blob.n > best.n) best = blob
  }
  return best ? [best.sx / (best.m || 1), best.sy / (best.m || 1)] : null
}
