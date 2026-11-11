// A TypeScript port of pico.js, a small face detector that runs in plain
// JavaScript (no WebAssembly, so the pad's CSP stays as it is).
//
// pico.js is released under the MIT license, Copyright (c) Nenad Markus,
// https://github.com/nenadmarkus/picojs. The cascade in facefinder.bin is
// from https://github.com/nenadmarkus/pico (MIT).

/** [row, column, size, score] */
export type Detection = [number, number, number, number]

export interface GrayImage {
  pixels: Uint8Array
  nrows: number
  ncols: number
  /** Row stride. */
  ldim: number
}

export interface CascadeParams {
  /** Move the window by this share of its size. */
  shiftfactor: number
  minsize: number
  maxsize: number
  /** Grow the window by this factor between passes. */
  scalefactor: number
}

export type Classifier = (r: number, c: number, s: number, pixels: Uint8Array, ldim: number) => number

/** Builds the classifier from a cascade file's bytes. */
export function unpackCascade(bytes: Uint8Array): Classifier {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  // The first 8 bytes are a version number and training data we don't need.
  let p = 8
  const tdepth = view.getInt32(p, true)
  p += 4
  const ntrees = view.getInt32(p, true)
  p += 4
  const leaves = 1 << tdepth
  const tcodes = new Int8Array(ntrees * 4 * leaves)
  const tpreds = new Float32Array(ntrees * leaves)
  const thresh = new Float32Array(ntrees)
  for (let t = 0; t < ntrees; t++) {
    // Each tree's codes start with four unused zeros.
    const codes = 4 * leaves - 4
    for (let i = 0; i < codes; i++) tcodes[t * 4 * leaves + 4 + i] = view.getInt8(p + i)
    p += codes
    for (let i = 0; i < leaves; i++) {
      tpreds[t * leaves + i] = view.getFloat32(p, true)
      p += 4
    }
    thresh[t] = view.getFloat32(p, true)
    p += 4
  }

  return (r, c, s, pixels, ldim) => {
    r = 256 * r
    c = 256 * c
    let root = 0
    let o = 0
    for (let i = 0; i < ntrees; i++) {
      let idx = 1
      for (let j = 0; j < tdepth; j++) {
        const a = pixels[(((r + tcodes[root + 4 * idx] * s) >> 8) * ldim + ((c + tcodes[root + 4 * idx + 1] * s) >> 8)) | 0]
        const b = pixels[(((r + tcodes[root + 4 * idx + 2] * s) >> 8) * ldim + ((c + tcodes[root + 4 * idx + 3] * s) >> 8)) | 0]
        idx = 2 * idx + (a <= b ? 1 : 0)
      }
      o += tpreds[leaves * i + idx - leaves]
      if (o <= thresh[i]) return -1
      root += 4 * leaves
    }
    return o - thresh[ntrees - 1]
  }
}

/** Slides the classifier over the image at every size. */
export function runCascade(image: GrayImage, classify: Classifier, params: CascadeParams): Detection[] {
  const detections: Detection[] = []
  let scale = params.minsize
  while (scale <= params.maxsize) {
    const step = Math.max(params.shiftfactor * scale, 1) | 0
    const offset = (scale / 2 + 1) | 0
    for (let r = offset; r <= image.nrows - offset; r += step) {
      for (let c = offset; c <= image.ncols - offset; c += step) {
        const q = classify(r, c, scale, image.pixels, image.ldim)
        if (q > 0) detections.push([r, c, scale, q])
      }
    }
    scale *= params.scalefactor
  }
  return detections
}

function iou(a: Detection, b: Detection): number {
  const overR = Math.max(0, Math.min(a[0] + a[2] / 2, b[0] + b[2] / 2) - Math.max(a[0] - a[2] / 2, b[0] - b[2] / 2))
  const overC = Math.max(0, Math.min(a[1] + a[2] / 2, b[1] + b[2] / 2) - Math.max(a[1] - a[2] / 2, b[1] - b[2] / 2))
  return (overR * overC) / (a[2] * a[2] + b[2] * b[2] - overR * overC)
}

/** Merges overlapping detections; each cluster's score is the sum of its members'. */
export function clusterDetections(dets: Detection[], threshold: number): Detection[] {
  const sorted = [...dets].sort((a, b) => b[3] - a[3])
  const taken = new Array<boolean>(sorted.length).fill(false)
  const clusters: Detection[] = []
  for (let i = 0; i < sorted.length; i++) {
    if (taken[i]) continue
    let r = 0
    let c = 0
    let s = 0
    let q = 0
    let n = 0
    for (let j = i; j < sorted.length; j++) {
      if (!taken[j] && iou(sorted[i], sorted[j]) > threshold) {
        taken[j] = true
        r += sorted[j][0]
        c += sorted[j][1]
        s += sorted[j][2]
        q += sorted[j][3]
        n++
      }
    }
    clusters.push([r / n, c / n, s / n, q])
  }
  return clusters
}
