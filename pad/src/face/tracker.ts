// Follows one face through a camera stream, cheaply enough to run on the
// kiosk's main thread and well enough for a dim, noisy, backlit picture.
//
// Finding a face scans densely (small steps in position and size: a coarse
// scan misses faces in poor light), so it is spread over a few frames, one
// slice of sizes per frame. Once found, each frame only scans around the
// face, at sizes near its own, which costs a fraction of a full scan and
// keeps following the same person when others walk past. A frame or two
// without a match keeps the last position; then it searches again.
import { clusterDetections, runCascade, type Classifier, type Detection, type GrayImage } from './pico'
import type { Face } from './face'

export interface TrackerOptions {
  /** Smallest face to find, as a share of the frame's short edge. */
  minShare: number
  /** Frames one full search is spread over. */
  slices: number
  /** Frames without a match the last face is kept for. */
  holdFrames: number
  /** Weight of the new position (0–1): lower is steadier, higher follows faster. */
  follow: number
}

export const DEFAULT_TRACKER: TrackerOptions = { minShare: 0.1, slices: 4, holdFrames: 2, follow: 0.6 }

/** A cluster needs this much combined score to count as a face. */
export const MIN_SCORE = 6
const DENSE = { shiftfactor: 0.05, scalefactor: 1.05 }
/** Around a tracked face: this many face sizes each way, sizes from this low to high. */
const NEAR = 0.7
const SIZE_LOW = 0.72
const SIZE_HIGH = 1.38

export class FaceTracker {
  private face: Face | null = null
  private misses = 0
  private slice = 0
  private pending: Detection[] = []

  constructor(
    private classify: Classifier,
    private opts: TrackerOptions = DEFAULT_TRACKER,
  ) {}

  /** True while following a face (false while searching). */
  get tracking(): boolean {
    return this.face !== null
  }

  reset() {
    this.face = null
    this.misses = 0
    this.slice = 0
    this.pending = []
  }

  /** One frame in; the followed face out (null while searching or lost). */
  update(image: GrayImage): Face | null {
    return this.face ? this.follow(image) : this.search(image)
  }

  private follow(image: GrayImage): Face | null {
    const prev = this.face!
    const edge = Math.min(image.nrows, image.ncols)
    const s = prev.size * edge
    const cx = prev.x * image.ncols
    const cy = prev.y * image.nrows
    const raw = runCascade(image, this.classify, {
      ...DENSE,
      minsize: Math.max(8, s * SIZE_LOW),
      maxsize: Math.min(edge, s * SIZE_HIGH),
      region: { top: cy - NEAR * s, bottom: cy + NEAR * s, left: cx - NEAR * s, right: cx + NEAR * s },
    })
    let best: Face | null = null
    let bestDist = Infinity
    for (const face of toFaces(clusterDetections(raw, 0.2), image)) {
      const d = Math.hypot(face.x - prev.x, face.y - prev.y)
      if (d < bestDist) {
        bestDist = d
        best = face
      }
    }
    if (best) {
      const a = this.opts.follow
      this.face = {
        x: a * best.x + (1 - a) * prev.x,
        y: a * best.y + (1 - a) * prev.y,
        size: a * best.size + (1 - a) * prev.size,
      }
      this.misses = 0
      return this.face
    }
    if (++this.misses <= this.opts.holdFrames) return prev
    this.reset()
    return null
  }

  private search(image: GrayImage): Face | null {
    const edge = Math.min(image.nrows, image.ncols)
    // Interleave sizes across slices so every frame costs about the same.
    const scales: number[] = []
    for (let size = Math.max(8, edge * this.opts.minShare); size <= edge; size *= DENSE.scalefactor) scales.push(size)
    for (let i = this.slice; i < scales.length; i += this.opts.slices) {
      this.pending.push(...runCascade(image, this.classify, { ...DENSE, minsize: scales[i], maxsize: scales[i] }))
    }
    if (++this.slice < this.opts.slices) return null
    const faces = toFaces(clusterDetections(this.pending, 0.2), image)
    this.slice = 0
    this.pending = []
    if (!faces.length) return null
    // Several people: the nearest (largest) one plays.
    this.face = faces.reduce((a, b) => (b.size > a.size ? b : a))
    this.misses = 0
    return this.face
  }
}

function toFaces(clusters: Detection[], image: GrayImage): Face[] {
  const edge = Math.min(image.nrows, image.ncols)
  return clusters.filter((d) => d[3] >= MIN_SCORE).map(([r, c, s]) => ({ x: c / image.ncols, y: r / image.nrows, size: s / edge }))
}
