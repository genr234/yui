import type { Vec3 } from '@yui/protocol'

/**
 * Where a player sees the kiosk's game from, measured by scanning it with
 * the phone's camera (input/aimCodes.ts reads the QR codes the kiosk draws
 * on the game; each one says where on the game it sits), and the pointer
 * that follows from it.
 *
 * The kiosk is small and players stand around it, often well to one side.
 * Seen at an angle the game is a trapezoid: its near edge is a bigger turn
 * of the wrist away than its far edge, and its top and bottom slope. A
 * pointer that maps turn angles straight onto the screen (PointerIntegrator)
 * is only right for a player standing square in front. This one casts the
 * phone's pointing direction onto the game's plane, as seen from where the
 * player stood when they scanned it, so the dot stays under where they aim
 * from any angle.
 *
 * Plane coordinates are the game's area normalised to −1…1, +x right, +y
 * down: exactly the pointer's coordinates.
 */

/**
 * The camera's focal length as a fraction of the frame's long side. Phones
 * don't tell the browser; their main cameras are 23–26 mm equivalent (a
 * 67–74° view across the long side), so this is within about 6%. An error
 * here scales how far a turn moves the dot by the same amount; the shape
 * (which edge is nearer, how the game slopes) does not depend on it.
 */
export const FOCAL_PER_LONG_SIDE = 0.71

export type Pt = [number, number]
/** Row-major 3×3. */
export type Mat3 = number[]

/**
 * `m` maps a plane point (x, y, 1) to the direction it is seen in from the
 * scan's camera (camera frame: +x right, +y down, +z out of the lens),
 * scaled so its first column (the game's half-width) has length 1: the third
 * column is then the camera-to-centre vector in half-widths. `up` is the way
 * up, in the same camera frame: the game's own up, since the kiosk stands upright.
 */
export interface Viewpoint {
  m: Mat3
  up: Vec3
}

// ------------------------------------------------------------------ algebra

const dot = (a: Vec3, b: Vec3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
const cross = (a: Vec3, b: Vec3): Vec3 => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]]
const len = (a: Vec3) => Math.hypot(a[0], a[1], a[2])
const scale = (a: Vec3, k: number): Vec3 => [a[0] * k, a[1] * k, a[2] * k]
const sub = (a: Vec3, b: Vec3): Vec3 => [a[0] - b[0], a[1] - b[1], a[2] - b[2]]
const unit = (a: Vec3): Vec3 => scale(a, 1 / (len(a) || 1))
const col = (m: Mat3, j: number): Vec3 => [m[j], m[3 + j], m[6 + j]]
const apply = (m: Mat3, v: Vec3): Vec3 => [
  m[0] * v[0] + m[1] * v[1] + m[2] * v[2],
  m[3] * v[0] + m[4] * v[1] + m[5] * v[2],
  m[6] * v[0] + m[7] * v[1] + m[8] * v[2],
]
const DEG = 180 / Math.PI

export function invert3(m: Mat3): Mat3 | null {
  const [a, b, c, d, e, f, g, h, i] = m
  const A = e * i - f * h
  const B = -(d * i - f * g)
  const C = d * h - e * g
  const det = a * A + b * B + c * C
  if (!Number.isFinite(det) || Math.abs(det) < 1e-12) return null
  const k = 1 / det
  return [
    A * k,
    -(b * i - c * h) * k,
    (b * f - c * e) * k,
    B * k,
    (a * i - c * g) * k,
    -(a * f - c * d) * k,
    C * k,
    -(a * h - b * g) * k,
    (a * e - b * d) * k,
  ]
}

/**
 * The homography taking each `src` point to its `dst` point: exact for four
 * pairs, least squares for more. Both sets are normalised first (centred,
 * mean distance √2) so pixels and plane units weigh alike. Null when the
 * points are degenerate.
 */
export function homography(src: readonly Pt[], dst: readonly Pt[]): Mat3 | null {
  if (src.length < 4 || src.length !== dst.length) return null
  const ns = normaliser(src)
  const nd = normaliser(dst)
  // h33 = 1; two equations per pair, accumulated as normal equations.
  const AtA = Array.from({ length: 8 }, () => new Array<number>(9).fill(0))
  const row = (r: number[]) => {
    for (let i = 0; i < 8; i++) {
      for (let j = 0; j < 8; j++) AtA[i][j] += r[i] * r[j]
      AtA[i][8] += r[i] * r[8]
    }
  }
  for (let k = 0; k < src.length; k++) {
    const [x, y] = ns.apply(src[k])
    const [u, v] = nd.apply(dst[k])
    row([x, y, 1, 0, 0, 0, -u * x, -u * y, u])
    row([0, 0, 0, x, y, 1, -v * x, -v * y, v])
  }
  const h = solve8(AtA)
  if (!h) return null
  // Undo the normalisation: H = Nd⁻¹ · Hn · Ns.
  const Hn: Mat3 = [...h, 1]
  const Ndi = invert3(nd.m)
  if (!Ndi) return null
  const H = mul3(mul3(Ndi, Hn), ns.m)
  const k = H[8] || 1
  return H.map((v) => v / k)
}

function normaliser(pts: readonly Pt[]): { m: Mat3; apply(p: Pt): Pt } {
  const n = pts.length
  const cx = pts.reduce((a, p) => a + p[0], 0) / n
  const cy = pts.reduce((a, p) => a + p[1], 0) / n
  const d = pts.reduce((a, p) => a + Math.hypot(p[0] - cx, p[1] - cy), 0) / n || 1
  const k = Math.SQRT2 / d
  return { m: [k, 0, -k * cx, 0, k, -k * cy, 0, 0, 1], apply: (p) => [(p[0] - cx) * k, (p[1] - cy) * k] }
}

function mul3(a: Mat3, b: Mat3): Mat3 {
  const out: number[] = []
  for (let i = 0; i < 3; i++) for (let j = 0; j < 3; j++) out.push(a[i * 3] * b[j] + a[i * 3 + 1] * b[3 + j] + a[i * 3 + 2] * b[6 + j])
  return out
}

/** Gaussian elimination with partial pivoting on an 8×9 augmented matrix. */
function solve8(A: number[][]): number[] | null {
  const n = 8
  for (let c = 0; c < n; c++) {
    let p = c
    for (let r = c + 1; r < n; r++) if (Math.abs(A[r][c]) > Math.abs(A[p][c])) p = r
    if (Math.abs(A[p][c]) < 1e-12) return null
    ;[A[c], A[p]] = [A[p], A[c]]
    for (let r = c + 1; r < n; r++) {
      const k = A[r][c] / A[c][c]
      for (let j = c; j <= n; j++) A[r][j] -= k * A[c][j]
    }
  }
  const x = new Array<number>(n).fill(0)
  for (let r = n - 1; r >= 0; r--) {
    let s = A[r][n]
    for (let j = r + 1; j < n; j++) s -= A[r][j] * x[j]
    x[r] = s / A[r][r]
  }
  return x.every(Number.isFinite) ? x : null
}

// ------------------------------------------------------------ the viewpoint

export type ViewProblem = 'scan-angle' | 'scan-odd'

/**
 * The viewpoint from points of the game seen in a camera frame: `plane`
 * points (game coordinates, from the codes' own contents) and where each was
 * seen (`image`, frame pixels), at least four, and the frame's size. Up is
 * the game's own up: the kiosk's screen stands upright, so this holds
 * however the phone or its camera picture is turned. Refuses what can't be
 * a phone looking at the kiosk: the game seen nearly edge-on, a camera
 * model that doesn't fit, or impossibly near or far.
 */
export function viewpointFromScan(
  plane: readonly Pt[],
  image: readonly Pt[],
  frame: { w: number; h: number },
  focal = FOCAL_PER_LONG_SIDE * Math.max(frame.w, frame.h),
): Viewpoint | ViewProblem {
  const H = homography(plane, image)
  if (!H) return 'scan-odd'
  const cx = frame.w / 2
  const cy = frame.h / 2
  // K⁻¹·H: image pixels → camera directions.
  let m: Mat3 = [
    (H[0] - cx * H[6]) / focal,
    (H[1] - cx * H[7]) / focal,
    (H[2] - cx * H[8]) / focal,
    (H[3] - cy * H[6]) / focal,
    (H[4] - cy * H[7]) / focal,
    (H[5] - cy * H[8]) / focal,
    H[6],
    H[7],
    H[8],
  ]
  // The game is in front of the lens.
  if (m[8] < 0) m = m.map((v) => -v)
  const k = 1 / (len(col(m, 0)) || 1)
  m = m.map((v) => v * k + 0)
  if (!m.every(Number.isFinite)) return 'scan-odd'
  const vp: Viewpoint = { m, up: scale(unit(col(m, 1)), -1) }
  const g = geometry(vp)
  // Seen from more than 75° off square, the scan is too squashed to trust.
  if (g.facing < Math.cos(75 / DEG)) return 'scan-angle'
  // The game's axes come out square when the camera model fits; a bad match does not.
  if (Math.abs(dot(g.right, g.down)) > 0.35 || g.distance < 0.3 || g.distance > 60) return 'scan-odd'
  return vp
}

interface Geometry {
  /** The game's right and down axes, and the way into the screen, in the camera frame. */
  right: Vec3
  down: Vec3
  into: Vec3
  /** Camera to the game's centre, unit. */
  center: Vec3
  /** cos of the angle between the line of sight and the screen's normal. */
  facing: number
  /** Camera to centre, in half-widths of the game. */
  distance: number
}

function geometry(vp: Viewpoint): Geometry {
  const right = unit(col(vp.m, 0))
  const down = unit(col(vp.m, 1))
  const t = col(vp.m, 2)
  const into = unit(cross(right, down))
  const center = unit(t)
  return { right, down, into, center, facing: dot(center, into), distance: len(t) / (len(col(vp.m, 0)) || 1) }
}

/**
 * Where the player stood, for telling them: degrees to the right of the
 * screen's straight-ahead (negative: to the left), degrees above its middle,
 * and how far, in widths of the game.
 */
export function describeViewpoint(vp: Viewpoint): { side: number; above: number; widths: number } {
  const g = geometry(vp)
  const out = scale(g.center, -1) // centre → player
  const ahead = -dot(out, g.into) // along the screen's outward normal
  return {
    side: Math.atan2(dot(out, g.right), ahead) * DEG,
    above: Math.asin(Math.max(-1, Math.min(1, -dot(out, g.down)))) * DEG,
    widths: g.distance / 2,
  }
}

/** Checked field by field (null for anything else). */
export function parseViewpoint(v: unknown): Viewpoint | null {
  if (!v || typeof v !== 'object') return null
  const o = v as Record<string, unknown>
  const fin = (x: unknown): x is number => typeof x === 'number' && Number.isFinite(x)
  if (!Array.isArray(o.m) || o.m.length !== 9 || !o.m.every(fin)) return null
  if (!Array.isArray(o.up) || o.up.length !== 3 || !o.up.every(fin)) return null
  const up = o.up as Vec3
  if (len(up) < 0.5 || !invert3(o.m as Mat3)) return null
  return { m: [...(o.m as number[])], up: unit(up) }
}

// ------------------------------------------------------------------ pointer

/** What MotionController needs from a pointer. */
export interface Pointer {
  update(gyro: Vec3, dtMs: number): [number, number]
  value(): [number, number]
  recenter(): void
}

const RAD = Math.PI / 180
const EL_LIMIT = 85 * RAD
const SEARCH_STEPS = 16

/**
 * The pointer seen from a viewpoint. The gyro turns the aim: yaw about the
 * vertical (azimuth), pitch up and down (elevation), starting from the
 * direction of the game's centre at recenter (the aim-centre step, or the
 * Recenter button). Each aim is cast onto the game's plane from where the
 * scan was taken. Aim stops at the game's edges, as PointerIntegrator's does,
 * so swinging past one does not have to be undone.
 */
export class ViewPointer implements Pointer {
  private g: Mat3
  private ahead: Vec3
  private right: Vec3
  private up: Vec3
  private el0: number
  private az = 0
  private el: number
  private last: [number, number] = [0, 0]

  constructor(vp: Viewpoint) {
    this.g = invert3(vp.m) ?? [1, 0, 0, 0, 1, 0, 0, 0, 1]
    this.up = unit(vp.up)
    const c = unit(col(vp.m, 2))
    this.ahead = unit(sub(c, scale(this.up, dot(c, this.up))))
    this.right = cross(this.ahead, this.up)
    this.el0 = Math.asin(Math.max(-1, Math.min(1, dot(c, this.up))))
    this.el = this.el0
  }

  /** Plane point the aim (az, el) lands on, or null when it misses the plane (looking away from it). */
  project(az: number, el: number): Pt | null {
    const ce = Math.cos(el)
    const d: Vec3 = [0, 1, 2].map(
      (i) => ce * (Math.cos(az) * this.ahead[i] + Math.sin(az) * this.right[i]) + Math.sin(el) * this.up[i],
    ) as Vec3
    const q = apply(this.g, d)
    if (!(q[2] > 1e-9)) return null
    return [q[0] / q[2], q[1] / q[2]]
  }

  update(gyro: Vec3, dtMs: number): [number, number] {
    const dt = Math.max(0, Math.min(dtMs, 100)) / 1000
    // Turning left is +yaw and moves the aim left; tipping the top up is +pitch and moves it up.
    const az = this.az - gyro[1] * dt * RAD
    const el = Math.max(-EL_LIMIT, Math.min(EL_LIMIT, this.el + gyro[0] * dt * RAD))
    this.az = this.limit(this.az, az, (a) => this.project(a, this.el), 0)
    this.el = this.limit(this.el, el, (e) => this.project(this.az, e), 1)
    return this.value()
  }

  /** The furthest of `from`→`to` that stays on the game along `axis` (all of it when `from` is off already). */
  private limit(from: number, to: number, at: (v: number) => Pt | null, axis: 0 | 1): number {
    const on = (v: number) => {
      const p = at(v)
      return !!p && Math.abs(p[axis]) <= 1
    }
    if (on(to) || !on(from)) return to
    let a = from
    let b = to
    for (let i = 0; i < SEARCH_STEPS; i++) {
      const mid = (a + b) / 2
      if (on(mid)) a = mid
      else b = mid
    }
    return a
  }

  value(): [number, number] {
    const p = this.project(this.az, this.el)
    if (p) {
      const c = (v: number) => (Math.abs(v) < 1e-9 ? 0 : Math.max(-1, Math.min(1, v)))
      this.last = [c(p[0]), c(p[1])]
    }
    return this.last
  }

  recenter() {
    this.az = 0
    this.el = this.el0
    this.last = [0, 0]
  }

  /**
   * Degrees from the centre to the game's right edge and to its top edge
   * (the same measure as the edge-aiming calibration's `pointer`).
   */
  ranges(): { yaw: number; pitch: number } {
    const edge = (at: (v: number) => Pt | null, axis: 0 | 1, target: number, start: number) => {
      // Walk out from the centre until past the edge, then bisect.
      let a = start
      let step = 0.5 * RAD
      let b = start
      for (let i = 0; i < 400; i++) {
        b = a + step
        const p = at(b)
        if (!p || Math.sign(target) * p[axis] >= Math.abs(target)) break
        a = b
      }
      for (let i = 0; i < SEARCH_STEPS * 2; i++) {
        const mid = (a + b) / 2
        const p = at(mid)
        if (p && Math.sign(target) * p[axis] < Math.abs(target)) a = mid
        else b = mid
      }
      return Math.abs(a - start) / RAD
    }
    return {
      yaw: edge((a) => this.project(a, this.el0), 0, 1, 0),
      pitch: edge((e) => this.project(0, e), 1, -1, this.el0),
    }
  }
}
