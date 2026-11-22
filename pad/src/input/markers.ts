import type { Pt } from '../platform/viewpoint'

/**
 * Finds the kiosk's four scan markers in a camera frame.
 *
 * While a phone is on the calibration's scan step the kiosk blacks out the
 * game's area and draws a marker near each corner: a white square with a
 * black square hole, half its size (platform styles/calibrate.css). A
 * marker is found by its shape, not its colour or size: a bright ring that
 * fully encloses exactly one dark hole, and that hole touches nothing else.
 * Ceiling lights, windows, text and game art rarely look like that; the four
 * that form the biggest convex quad of similar markers are taken.
 *
 * Pure and synchronous: feed it a grey frame (about 480 px on its long side
 * is plenty and takes a few milliseconds).
 */

export interface Marker {
  /** Centre (the hole's centroid), frame pixels. */
  x: number
  y: number
  /** About the marker's side, pixels. */
  size: number
}

/** Rec. 601 luma of an RGBA frame. */
export function toGray(rgba: ArrayLike<number>, w: number, h: number, out: Uint8Array = new Uint8Array(w * h)): Uint8Array {
  for (let i = 0, j = 0; i < w * h; i++, j += 4) out[i] = (rgba[j] * 77 + rgba[j + 1] * 150 + rgba[j + 2] * 29) >> 8
  return out
}

/**
 * Bright if clearly above the local mean. The window is a fifth of the long
 * side: wider than any marker's ring, so a ring stays bright all through and
 * its hole stays dark even when the camera blooms the white into it.
 */
function binarize(gray: Uint8Array, w: number, h: number): Uint8Array {
  const W = w + 1
  const ii = new Uint32Array(W * (h + 1))
  for (let y = 0; y < h; y++) {
    let row = 0
    for (let x = 0; x < w; x++) {
      row += gray[y * w + x]
      ii[(y + 1) * W + x + 1] = ii[y * W + x + 1] + row
    }
  }
  const r = Math.max(4, Math.round(Math.max(w, h) / 10))
  const bin = new Uint8Array(w * h)
  for (let y = 0; y < h; y++) {
    const y0 = Math.max(0, y - r)
    const y1 = Math.min(h, y + r + 1)
    for (let x = 0; x < w; x++) {
      const x0 = Math.max(0, x - r)
      const x1 = Math.min(w, x + r + 1)
      const sum = ii[y1 * W + x1] - ii[y0 * W + x1] - ii[y1 * W + x0] + ii[y0 * W + x0]
      const mean = sum / ((x1 - x0) * (y1 - y0))
      bin[y * w + x] = gray[y * w + x] > mean * 1.04 + 4 ? 1 : 0
    }
  }
  return bin
}

function find(parent: Int32Array, i: number): number {
  while (parent[i] !== i) {
    parent[i] = parent[parent[i]]
    i = parent[i]
  }
  return i
}

function union(parent: Int32Array, a: number, b: number) {
  const ra = find(parent, a)
  const rb = find(parent, b)
  if (ra !== rb) parent[Math.max(ra, rb)] = Math.min(ra, rb)
}

/** Every marker-shaped thing in the frame. */
export function findMarkers(gray: Uint8Array, w: number, h: number): Marker[] {
  const n = w * h
  const bin = binarize(gray, w, h)
  // Bright regions join 8-connected and dark ones 4-connected, so a bright
  // ring always separates its hole from the outside.
  const parent = new Int32Array(n)
  for (let i = 0; i < n; i++) parent[i] = i
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = y * w + x
      const b = bin[i]
      if (x > 0 && bin[i - 1] === b) union(parent, i, i - 1)
      if (y > 0) {
        if (bin[i - w] === b) union(parent, i, i - w)
        if (b) {
          if (x > 0 && bin[i - w - 1]) union(parent, i, i - w - 1)
          if (x < w - 1 && bin[i - w + 1]) union(parent, i, i - w + 1)
        }
      }
    }
  }

  const area = new Int32Array(n)
  const sx = new Float64Array(n)
  const sy = new Float64Array(n)
  const edge = new Uint8Array(n)
  // A dark region's bright neighbour: −1 none yet, −2 more than one.
  const around = new Int32Array(n).fill(-1)
  const meet = (dark: number, bright: number) => {
    const a = around[dark]
    if (a === -1) around[dark] = bright
    else if (a !== bright) around[dark] = -2
  }
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = y * w + x
      const root = find(parent, i)
      parent[i] = root
      area[root]++
      sx[root] += x
      sy[root] += y
      if (x === 0 || y === 0 || x === w - 1 || y === h - 1) edge[root] = 1
    }
  }
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = y * w + x
      if (x < w - 1 && bin[i] !== bin[i + 1]) {
        if (bin[i]) meet(parent[i + 1], parent[i])
        else meet(parent[i], parent[i + 1])
      }
      if (y < h - 1 && bin[i] !== bin[i + w]) {
        if (bin[i]) meet(parent[i + w], parent[i])
        else meet(parent[i], parent[i + w])
      }
    }
  }

  // Holes: dark regions inside exactly one bright region.
  const holes = new Map<number, number[]>()
  for (let i = 0; i < n; i++) {
    if (parent[i] !== i || bin[i] || edge[i] || around[i] < 0 || edge[around[i]]) continue
    const ring = around[i]
    const list = holes.get(ring)
    if (list) list.push(i)
    else holes.set(ring, [i])
  }

  const out: Marker[] = []
  for (const [ring, list] of holes) {
    if (list.length !== 1) continue // a marker has one hole: text and clutter have several
    const hole = list[0]
    const ah = area[hole]
    const ar = area[ring]
    if (ah < 4) continue
    // The hole is a quarter of the marker's area; bloom shrinks it, blur grows it.
    const q = ah / (ah + ar)
    if (q < 0.06 || q > 0.55) continue
    const hx = sx[hole] / ah
    const hy = sy[hole] / ah
    const rx = sx[ring] / ar
    const ry = sy[ring] / ar
    const side = Math.sqrt(ah + ar)
    // Concentric, as a square ring around a square hole is.
    if (Math.hypot(hx - rx, hy - ry) > 0.2 * side) continue
    // +0.5: pixel i covers [i, i + 1).
    out.push({ x: hx + 0.5, y: hy + 0.5, size: 2 * Math.sqrt(ah) })
  }
  return out
}

const EXPECT: Pt[] = [
  [-1, -1],
  [1, -1],
  [1, 1],
  [-1, 1],
]

/**
 * The four markers' centres in order TL, TR, BR, BL, or null. Picks, from
 * the biggest dozen candidates, the four making the largest convex quad of
 * markers of similar size and spaced like the kiosk's (a marker is a sixth
 * to a third of the gap to its neighbours). The camera picture is upright,
 * so a corner is named by where it sits.
 */
export function pickQuad(markers: readonly Marker[]): Pt[] | null {
  const c = [...markers].sort((a, b) => b.size - a.size).slice(0, 12)
  let best: Pt[] | null = null
  let bestScore = 0
  const k = c.length
  for (let a = 0; a < k; a++)
    for (let b = a + 1; b < k; b++)
      for (let d = b + 1; d < k; d++)
        for (let e = d + 1; e < k; e++) {
          const four = [c[a], c[b], c[d], c[e]]
          const sizes = four.map((m) => m.size)
          const similar = Math.min(...sizes) / Math.max(...sizes)
          if (similar < 0.3) continue
          const q = orderQuad(four)
          if (!q) continue
          const score = q.area * similar
          if (score > bestScore) {
            bestScore = score
            best = q.pts
          }
        }
  return best
}

function orderQuad(four: Marker[]): { pts: Pt[]; area: number } | null {
  const cx = four.reduce((s, m) => s + m.x, 0) / 4
  const cy = four.reduce((s, m) => s + m.y, 0) / 4
  // Clockwise on screen (y down): by angle from +x towards +y.
  const ring = [...four].sort((p, q) => Math.atan2(p.y - cy, p.x - cx) - Math.atan2(q.y - cy, q.x - cx))
  let area = 0
  for (let i = 0; i < 4; i++) {
    const p = ring[i]
    const q = ring[(i + 1) % 4]
    const r = ring[(i + 2) % 4]
    const turn = (q.x - p.x) * (r.y - q.y) - (q.y - p.y) * (r.x - q.x)
    if (turn <= 0) return null // not convex
    area += p.x * q.y - q.x * p.y
  }
  area /= 2
  // Which of the four is the top-left.
  let shift = 0
  let fit = -Infinity
  for (let s = 0; s < 4; s++) {
    let f = 0
    for (let i = 0; i < 4; i++) {
      const m = ring[(i + s) % 4]
      f += (m.x - cx) * EXPECT[i][0] + (m.y - cy) * EXPECT[i][1]
    }
    if (f > fit) {
      fit = f
      shift = s
    }
  }
  const ordered = [0, 1, 2, 3].map((i) => ring[(i + shift) % 4])
  for (let i = 0; i < 4; i++) {
    const m = ordered[i]
    const prev = ordered[(i + 3) % 4]
    const next = ordered[(i + 1) % 4]
    const gap = Math.min(Math.hypot(m.x - prev.x, m.y - prev.y), Math.hypot(m.x - next.x, m.y - next.y))
    const r = m.size / gap
    if (r < 0.05 || r > 0.7) return null
  }
  return { pts: ordered.map((m) => [m.x, m.y] as Pt), area }
}

/** The scan markers in a grey frame, TL, TR, BR, BL, or null. */
export function detectScanMarkers(gray: Uint8Array, w: number, h: number): Pt[] | null {
  return pickQuad(findMarkers(gray, w, h))
}
