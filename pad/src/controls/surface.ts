import type { TouchPoint } from '@yui/protocol'

/** A rectangle inside the surface element, in element pixels. */
export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

/**
 * Where the video image sits inside its element when it is scaled to fit
 * (`object-fit: contain`): the largest rectangle of the video's aspect ratio,
 * centred. Without a known video size the image fills the element.
 */
export function contentRect(boxW: number, boxH: number, videoW: number, videoH: number): Rect {
  if (!(boxW > 0 && boxH > 0)) return { x: 0, y: 0, w: 0, h: 0 }
  if (!(videoW > 0 && videoH > 0)) return { x: 0, y: 0, w: boxW, h: boxH }
  const scale = Math.min(boxW / videoW, boxH / videoH)
  const w = videoW * scale
  const h = videoH * scale
  return { x: (boxW - w) / 2, y: (boxH - h) / 2, w, h }
}

const clamp16 = (v: number) => Math.max(0, Math.min(65535, Math.round(v)))

/** Element-relative pixels → 0…65535 across the content rectangle (clamped). */
export function toU16(rect: Rect, px: number, py: number): { x: number; y: number } {
  if (!(rect.w > 0 && rect.h > 0)) return { x: 0, y: 0 }
  return { x: clamp16(((px - rect.x) / rect.w) * 65535), y: clamp16(((py - rect.y) / rect.h) * 65535) }
}

export const insideRect = (rect: Rect, px: number, py: number) =>
  px >= rect.x && px <= rect.x + rect.w && py >= rect.y && py <= rect.y + rect.h

const UP: TouchPoint = { down: false, x: 0, y: 0 }

/** Tracks up to two fingers on a surface and reports them as touch points. */
export class SurfaceTouches {
  private slots: [number | null, number | null] = [null, null]
  private pts: [TouchPoint, TouchPoint] = [UP, UP]

  /** A pointer went down at element pixels (px, py). Ignored in the letterbox or with two fingers already down. */
  down(id: number, rect: Rect, px: number, py: number): boolean {
    if (this.slots.includes(id)) return false
    if (!insideRect(rect, px, py)) return false
    const i: 0 | 1 | null = this.slots[0] === null ? 0 : this.slots[1] === null ? 1 : null
    if (i === null) return false
    this.slots[i] = id
    this.pts[i] = { down: true, ...toU16(rect, px, py) }
    return true
  }

  /** A tracked pointer moved; the position is clamped to the image. */
  move(id: number, rect: Rect, px: number, py: number): boolean {
    const i = this.slots.indexOf(id)
    if (i < 0) return false
    this.pts[i] = { down: true, ...toU16(rect, px, py) }
    return true
  }

  up(id: number): boolean {
    const i = this.slots.indexOf(id)
    if (i < 0) return false
    this.slots[i] = null
    this.pts[i] = UP
    return true
  }

  clear() {
    this.slots = [null, null]
    this.pts = [UP, UP]
  }

  get count() {
    return this.slots.filter((s) => s !== null).length
  }

  points(): [TouchPoint, TouchPoint] {
    return [this.pts[0], this.pts[1]]
  }
}
