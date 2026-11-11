// Finds the face in a square camera frame and says where googly eyes and a
// mouth go. Everything is in shares of the frame (0–1), so it works at any
// size and is easy to test.
import cascadeUrl from './facefinder.bin?url'
import { clusterDetections, runCascade, unpackCascade, type Classifier, type GrayImage } from './pico'

export interface Face {
  /** Centre and size of the face box. */
  x: number
  y: number
  size: number
}

export interface FaceSpots {
  eyes: { x: number; y: number; r: number }[]
  mouth: { x: number; y: number }
  nose: { x: number; y: number }
}

/** Frames are shrunk to this edge before searching: plenty for one close face, and fast. */
export const SEARCH_SIZE = 120
/** A cluster needs this much combined score to count as a face. */
const MIN_SCORE = 6

let classifier: Promise<Classifier> | null = null

/** Loads the detector once. Rejects when the cascade can't be fetched. */
export function loadFaceFinder(): Promise<Classifier> {
  classifier ??= fetch(cascadeUrl)
    .then((res) => {
      if (!res.ok) throw new Error(`cascade ${res.status}`)
      return res.arrayBuffer()
    })
    .then((buffer) => unpackCascade(new Uint8Array(buffer)))
    .catch((error) => {
      classifier = null
      throw error
    })
  return classifier
}

/** RGBA pixels to one gray byte per pixel. */
export function toGray(rgba: Uint8ClampedArray, width: number, height: number): GrayImage {
  const pixels = new Uint8Array(width * height)
  for (let i = 0; i < pixels.length; i++) {
    pixels[i] = (2 * rgba[4 * i] + 7 * rgba[4 * i + 1] + rgba[4 * i + 2]) / 10
  }
  return { pixels, nrows: height, ncols: width, ldim: width }
}

/** The biggest confident face in a gray image, or null. */
export function findFace(image: GrayImage, classify: Classifier): Face | null {
  const edge = Math.min(image.nrows, image.ncols)
  const raw = runCascade(image, classify, {
    shiftfactor: 0.1,
    minsize: Math.round(edge * 0.25),
    maxsize: edge,
    scalefactor: 1.1,
  })
  let best: Face | null = null
  let bestSize = 0
  for (const [r, c, s, q] of clusterDetections(raw, 0.2)) {
    if (q < MIN_SCORE || s <= bestSize) continue
    bestSize = s
    best = { x: c / image.ncols, y: r / image.nrows, size: s / edge }
  }
  return best
}

/**
 * Where the eyes, nose and mouth sit in a face box. The detector frames a
 * face from eyebrows to chin, so these are fixed shares of the box; the
 * player can drag anything that lands off.
 */
export function faceSpots(face: Face): FaceSpots {
  const s = face.size
  return {
    eyes: [
      { x: face.x - 0.19 * s, y: face.y - 0.1 * s, r: 0.125 * s },
      { x: face.x + 0.19 * s, y: face.y - 0.1 * s, r: 0.125 * s },
    ],
    nose: { x: face.x, y: face.y + 0.1 * s },
    mouth: { x: face.x, y: face.y + 0.3 * s },
  }
}
