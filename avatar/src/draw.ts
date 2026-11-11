// Draws a creature on a 2D canvas.
import type { Creature } from './format'
import { FRAMES, layoutCreature, photoSquare, type Box, type EyeSpot, type Sprite, type View } from './layout'

export interface PupilAt {
  x: number
  y: number
}

export interface DrawOptions {
  /** The sprite atlas image (src/atlas.png). */
  atlas: CanvasImageSource
  /** The player's colour as a CSS colour. */
  color: string
  view: View
  /** The square to draw into, in canvas pixels. */
  size: number
  /** One per eye, in eye units (see physics.ts). Missing ones rest at the bottom. */
  pupils?: PupilAt[]
  /** The decoded photo for a creature that has one. */
  photo?: CanvasImageSource | null
}

type Ctx = CanvasRenderingContext2D

const tinted = new Map<string, HTMLCanvasElement>()

function scratch(w: number, h: number): [HTMLCanvasElement, Ctx] {
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.ceil(w))
  canvas.height = Math.max(1, Math.ceil(h))
  return [canvas, canvas.getContext('2d') as Ctx]
}

/** A part multiplied by the player's colour. The white parts carry the shading. */
function tintedPart(atlas: CanvasImageSource, name: string, color: string): HTMLCanvasElement {
  const key = `${color}|${name}`
  let canvas = tinted.get(key)
  if (canvas) return canvas
  const [x, y, w, h] = FRAMES[name]
  let ctx: Ctx
  ;[canvas, ctx] = scratch(w, h)
  ctx.drawImage(atlas, x, y, w, h, 0, 0, w, h)
  ctx.globalCompositeOperation = 'multiply'
  ctx.fillStyle = color
  ctx.fillRect(0, 0, w, h)
  ctx.globalCompositeOperation = 'destination-in'
  ctx.drawImage(atlas, x, y, w, h, 0, 0, w, h)
  if (tinted.size > 400) tinted.clear()
  tinted.set(key, canvas)
  return canvas
}

function drawSprite(ctx: Ctx, atlas: CanvasImageSource, s: Sprite, color: string): void {
  const [x, y, w, h] = FRAMES[s.name]
  ctx.save()
  ctx.translate(s.x, s.y)
  ctx.scale(s.flip ? -s.scale : s.scale, s.scale)
  if (s.tint) ctx.drawImage(tintedPart(atlas, s.name, color), -s.ax, -s.ay)
  else ctx.drawImage(atlas, x, y, w, h, -s.ax, -s.ay, w, h)
  ctx.restore()
}

/** The body's shape filled with the photo. */
function photoBody(atlas: CanvasImageSource, name: string, photo: CanvasImageSource, square: Box): HTMLCanvasElement {
  const [x, y, w, h] = FRAMES[name]
  const [canvas, ctx] = scratch(w, h)
  ctx.drawImage(atlas, x, y, w, h, 0, 0, w, h)
  ctx.globalCompositeOperation = 'source-in'
  ctx.imageSmoothingQuality = 'high'
  ctx.drawImage(photo, square.x, square.y, square.w, square.h)
  return canvas
}

const DARK = '#2b2118'

function disc(ctx: Ctx, x: number, y: number, r: number, fill: string): void {
  ctx.beginPath()
  ctx.arc(x, y, Math.max(0, r), 0, Math.PI * 2)
  ctx.fillStyle = fill
  ctx.fill()
}

interface EyeLook {
  white: string
  /** Outer disc of the moving part, as a share of the eye's radius. */
  iris: number
  irisColor: string
  pupil: number
  pupilColor: string
  slit?: boolean
}

const LOOKS: EyeLook[] = [
  { white: '#ffffff', iris: 0.5, irisColor: DARK, pupil: 0, pupilColor: DARK },
  { white: '#ffffff', iris: 0.6, irisColor: '#35c3e8', pupil: 0.34, pupilColor: DARK },
  { white: '#ffffff', iris: 0.6, irisColor: '#2ecc71', pupil: 0.34, pupilColor: DARK },
  { white: '#ffffff', iris: 0.6, irisColor: '#ff4d6d', pupil: 0.34, pupilColor: DARK },
  { white: '#ffb703', iris: 0.62, irisColor: '#ffb703', pupil: 0.62, pupilColor: DARK, slit: true },
  { white: '#3a2e25', iris: 0.5, irisColor: '#ffffff', pupil: 0.26, pupilColor: '#3a2e25' },
]

/** Eye styles drawn from a sprite; they don't move. */
const STILL_EYES: Record<number, { name: string; width: number }> = {
  6: { name: 'eye_closed_happy', width: 2.1 },
  7: { name: 'eye_dead', width: 1.5 },
}

/** True when this eye style has a pupil that moves. */
export function eyeMoves(style: number): boolean {
  return !(style in STILL_EYES)
}

/**
 * Draws one eye. `pupil` is in eye units (see physics.ts). The two styles
 * that come from a sprite need the atlas; without it they are skipped.
 */
export function drawEye(ctx: Ctx, atlas: CanvasImageSource | null, e: EyeSpot, style: number, pupil: PupilAt): void {
  const still = STILL_EYES[style]
  if (still) {
    if (!atlas) return
    const [x, y, w, h] = FRAMES[still.name]
    const dw = e.r * still.width
    const dh = (dw * h) / w
    ctx.drawImage(atlas, x, y, w, h, e.cx - dw / 2, e.cy - dh / 2, dw, dh)
    return
  }
  const look = LOOKS[style] ?? LOOKS[0]
  disc(ctx, e.cx, e.cy + e.r * 0.07, e.r, 'rgba(0, 0, 0, 0.2)')
  disc(ctx, e.cx, e.cy, e.r, look.white)
  if (look.slit) disc(ctx, e.cx, e.cy, e.r * 0.86, '#ffd166')
  const travel = e.r * (1 - look.iris - 0.06)
  const px = e.cx + pupil.x * travel
  const py = e.cy + pupil.y * travel
  const ir = e.r * look.iris
  if (look.slit) {
    ctx.beginPath()
    ctx.ellipse(px, py, ir * 0.3, ir, 0, 0, Math.PI * 2)
    ctx.fillStyle = look.pupilColor
    ctx.fill()
    return
  }
  disc(ctx, px, py, ir, look.irisColor)
  if (look.pupil > 0) disc(ctx, px, py, e.r * look.pupil, look.pupilColor)
  // A small glint that stays put on the pupil.
  disc(ctx, px - ir * 0.32, py - ir * 0.34, ir * 0.2, 'rgba(255, 255, 255, 0.85)')
}

const REST: PupilAt = { x: 0, y: 1 }

/**
 * Draws the creature centred in a `size`×`size` square at the canvas origin.
 * The caller clears the canvas first.
 */
export function drawCreature(ctx: Ctx, c: Creature, opts: DrawOptions): void {
  const layout = layoutCreature(c, opts.view)
  const b = layout.bounds
  const scale = opts.size / Math.max(b.w, b.h)
  ctx.save()
  ctx.translate((opts.size - b.w * scale) / 2, (opts.size - b.h * scale) / 2)
  ctx.scale(scale, scale)
  ctx.translate(-b.x, -b.y)
  ctx.imageSmoothingQuality = 'high'

  for (const s of layout.back) drawSprite(ctx, opts.atlas, s, opts.color)
  if (c.photo && opts.photo) ctx.drawImage(photoBody(opts.atlas, layout.body, opts.photo, photoSquare(c.body)), 0, 0)
  else ctx.drawImage(tintedPart(opts.atlas, layout.body, opts.color), 0, 0)
  for (const s of layout.face) drawSprite(ctx, opts.atlas, s, opts.color)
  layout.eyes.forEach((e, i) => drawEye(ctx, opts.atlas, e, c.eyeStyle, opts.pupils?.[i] ?? REST))
  for (const s of layout.brows) drawSprite(ctx, opts.atlas, s, opts.color)
  ctx.restore()
}

/** Maps a point in the drawn square back to the body's box, as percentages. */
export function pointToBody(c: Creature, view: View, size: number, x: number, y: number): { x: number; y: number } {
  const layout = layoutCreature(c, view)
  const b = layout.bounds
  const scale = size / Math.max(b.w, b.h)
  const ox = (size - b.w * scale) / 2
  const oy = (size - b.h * scale) / 2
  return {
    x: (((x - ox) / scale + b.x) / layout.bodyW) * 100,
    y: (((y - oy) / scale + b.y) / layout.bodyH) * 100,
  }
}
