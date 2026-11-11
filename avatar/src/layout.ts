// Where each part of a creature goes. Pure geometry, no canvas: the body's
// box starts at (0, 0) and everything is in atlas pixels.
import atlasData from './atlas.json'
import type { Creature, Eye, Point } from './format'

export type Frame = [x: number, y: number, w: number, h: number]
interface Outline {
  top: number[]
  bottom: number[]
  left: number[]
  right: number[]
}

export const FRAMES = atlasData.frames as unknown as Record<string, Frame>
const OUTLINES = atlasData.outlines as Record<string, Outline>

const LETTERS = 'ABCDEF'
export const BODY_NAMES = [...LETTERS].map((c) => `body${c}`)
export const ARM_NAMES = [...'ABCDE'].map((c) => `arm${c}`)
export const LEG_NAMES = [...'ABCDE'].map((c) => `leg${c}`)
export const TOP_NAMES = ['top_horn_large', 'top_horn_small', 'top_ear', 'top_ear_round', 'top_antenna_large', 'top_antenna_small']
export const MOUTH_NAMES = [
  'mouthA',
  'mouth_closed_happy',
  'mouthB',
  'mouthC',
  'mouth_closed_teeth',
  'mouthD',
  'mouthE',
  'mouthF',
  'mouth_closed_fangs',
  'mouthG',
  'mouthH',
  'mouthI',
  'mouthJ',
  'mouth_closed_sad',
]
export const NOSE_NAMES = ['nose_red', 'nose_brown', 'nose_green', 'nose_yellow']
export const BROW_NAMES = ['eyebrowA', 'eyebrowB', 'eyebrowC']

/** Where a limb joins the body, in the sprite's own pixels. */
const ANCHORS: Record<string, Point> = {
  armA: { x: 48, y: 48 },
  armB: { x: 34, y: 34 },
  armC: { x: 58, y: 58 },
  armD: { x: 60, y: 60 },
  armE: { x: 60, y: 26 },
  legA: { x: 51, y: 51 },
  legB: { x: 32, y: 32 },
  legC: { x: 42, y: 42 },
  legD: { x: 42, y: 42 },
  legE: { x: 50, y: 50 },
  top_horn_large: { x: 26, y: 66 },
  top_horn_small: { x: 20, y: 42 },
  top_ear: { x: 40, y: 74 },
  top_ear_round: { x: 54, y: 84 },
  top_antenna_large: { x: 16, y: 104 },
  top_antenna_small: { x: 10, y: 76 },
}

/** Pixel radius the pack's eyebrows were drawn for. */
const BROW_EYE_RADIUS = 64

export interface Sprite {
  name: string
  /** Where the sprite's anchor lands. */
  x: number
  y: number
  /** The anchor inside the sprite. */
  ax: number
  ay: number
  scale: number
  flip: boolean
  /** Take the player's colour. */
  tint: boolean
}

export interface EyeSpot {
  cx: number
  cy: number
  r: number
}

export interface Box {
  x: number
  y: number
  w: number
  h: number
}

export interface Layout {
  body: string
  bodyW: number
  bodyH: number
  /** Drawn before the body: legs, arms, tops. */
  back: Sprite[]
  /** Drawn over the body: mouth, nose. */
  face: Sprite[]
  eyes: EyeSpot[]
  /** One per eye, drawn last. */
  brows: Sprite[]
  bounds: Box
}

export type View = 'face' | 'full'

export function bodySize(body: number): { w: number; h: number } {
  const [, , w, h] = FRAMES[BODY_NAMES[body] ?? BODY_NAMES[0]]
  return { w, h }
}

function sample(values: number[], t: number): number {
  const pos = Math.min(1, Math.max(0, t)) * (values.length - 1)
  const i = Math.floor(pos)
  const next = values[Math.min(values.length - 1, i + 1)]
  return values[i] + (next - values[i]) * (pos - i)
}

function limb(name: string, x: number, y: number, flip: boolean): Sprite {
  const a = ANCHORS[name]
  return { name, x, y, ax: a.x, ay: a.y, scale: 1, flip, tint: true }
}

function centred(name: string, x: number, y: number, scale = 1, flip = false): Sprite {
  const [, , w, h] = FRAMES[name]
  return { name, x, y, ax: w / 2, ay: h / 2, scale, flip, tint: false }
}

function spriteBox(s: Sprite): Box {
  const [, , w, h] = FRAMES[s.name]
  const left = s.flip ? s.x - (w - s.ax) * s.scale : s.x - s.ax * s.scale
  return { x: left, y: s.y - s.ay * s.scale, w: w * s.scale, h: h * s.scale }
}

function union(a: Box, b: Box): Box {
  const x = Math.min(a.x, b.x)
  const y = Math.min(a.y, b.y)
  return { x, y, w: Math.max(a.x + a.w, b.x + b.w) - x, h: Math.max(a.y + a.h, b.y + b.h) - y }
}

/**
 * Lays a creature out. The `face` view leaves out arms and legs so the head
 * stays readable in a small chip; `full` is the whole creature.
 */
export function layoutCreature(c: Creature, view: View): Layout {
  const body = BODY_NAMES[c.body] ?? BODY_NAMES[0]
  const { w, h } = bodySize(c.body)
  const outline = OUTLINES[body]
  const back: Sprite[] = []

  if (view === 'full') {
    const leg = LEG_NAMES[c.legs - 1]
    if (leg) {
      for (const side of [0.3, 0.7]) {
        back.push(limb(leg, side * w, sample(outline.bottom, side) - 24, side < 0.5))
      }
    }
    const arm = ARM_NAMES[c.arms - 1]
    if (arm) {
      const t = h > w * 1.2 ? 0.42 : 0.52
      back.push(limb(arm, sample(outline.left, t) + 14, t * h, true))
      back.push(limb(arm, sample(outline.right, t) - 14, t * h, false))
    }
  }
  const top = TOP_NAMES[c.top - 1]
  if (top) {
    for (const side of [0.22, 0.78]) {
      back.push(limb(top, side * w, sample(outline.top, side) + 12, side < 0.5))
    }
  }

  const face: Sprite[] = []
  const mouth = MOUTH_NAMES[c.mouth - 1]
  if (mouth) face.push(centred(mouth, (c.mouthAt.x / 100) * w, (c.mouthAt.y / 100) * h))
  const nose = NOSE_NAMES[c.nose - 1]
  if (nose) face.push(centred(nose, (c.noseAt.x / 100) * w, (c.noseAt.y / 100) * h))

  const eyes = c.eyes.map((e) => ({ cx: (e.x / 100) * w, cy: (e.y / 100) * h, r: (e.r / 100) * w }))
  const brow = BROW_NAMES[c.brow - 1]
  const brows = brow
    ? eyes.map((e) => centred(brow, e.cx, e.cy - e.r * 1.2, e.r / BROW_EYE_RADIUS, e.cx > w / 2 + 1))
    : []

  let bounds: Box = { x: 0, y: 0, w, h }
  for (const s of [...back, ...brows]) bounds = union(bounds, spriteBox(s))
  for (const e of eyes) bounds = union(bounds, { x: e.cx - e.r, y: e.cy - e.r, w: e.r * 2, h: e.r * 2 })
  return { body, bodyW: w, bodyH: h, back, face, eyes, brows, bounds }
}

/** The square of the body's box that a photo covers (it is centred and fills the longer side). */
export function photoSquare(body: number): Box {
  const { w, h } = bodySize(body)
  const side = Math.max(w, h)
  return { x: (w - side) / 2, y: (h - side) / 2, w: side, h: side }
}

const tall = (body: number) => {
  const { w, h } = bodySize(body)
  return h > w * 1.2
}

/** Evenly spaced eyes for a body: one, two or three. */
export function defaultEyes(body: number, count: number): Eye[] {
  const y = tall(body) ? 27 : 38
  const big = tall(body) ? 1.3 : 1
  if (count <= 0) return []
  if (count === 1) return [{ x: 50, y, r: Math.round(19 * big) }]
  if (count === 2)
    return [
      { x: 33, y, r: Math.round(13 * big) },
      { x: 67, y, r: Math.round(13 * big) },
    ]
  return [
    { x: 24, y: y + 3, r: Math.round(10 * big) },
    { x: 50, y: y - 4, r: Math.round(11 * big) },
    { x: 76, y: y + 3, r: Math.round(10 * big) },
  ]
}

export function defaultMouthAt(body: number): Point {
  return { x: 50, y: tall(body) ? 50 : 70 }
}

export function defaultNoseAt(body: number): Point {
  return { x: 50, y: tall(body) ? 39 : 55 }
}

/**
 * Moves a creature onto another body. With a photo the eyes, mouth and nose
 * stay on the same spots of the face; without one they keep their relative
 * positions.
 */
export function withBody(c: Creature, body: number): Creature {
  if (!c.photo || body === c.body) return { ...c, body }
  const from = photoSquare(c.body)
  const to = photoSquare(body)
  const a = bodySize(c.body)
  const b = bodySize(body)
  const clamp = (n: number) => Math.min(100, Math.max(0, Math.round(n)))
  const move = <T extends Point>(p: T): T => {
    const u = ((p.x / 100) * a.w - from.x) / from.w
    const v = ((p.y / 100) * a.h - from.y) / from.h
    return { ...p, x: clamp(((to.x + u * to.w) / b.w) * 100), y: clamp(((to.y + v * to.h) / b.h) * 100) }
  }
  const eyes = c.eyes.map((e) => {
    const moved = move(e)
    // The photo's scale changes with the body, so the eye's size does too.
    const px = (e.r / 100) * a.w * (to.w / from.w)
    return { ...moved, r: Math.min(40, Math.max(4, Math.round((px / b.w) * 100))) }
  })
  return { ...c, body, eyes, mouthAt: move(c.mouthAt), noseAt: move(c.noseAt) }
}
