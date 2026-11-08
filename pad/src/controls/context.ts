import { getContext, setContext } from 'svelte'
import type { Readable } from 'svelte/store'
import type { CalibrationResult } from '../platform/motion'
import type { ControlSink } from './sink'

export type MotionStatus = 'off' | 'needs-permission' | 'denied' | 'unsupported' | 'on'

export interface PadContext {
  sink: ControlSink
  motion: Readable<{ status: MotionStatus; calibrated: boolean; gyro: boolean }>
  /** Recenter pressed (true) / released (false): holds bit 17 and re-zeroes the pointer. */
  recenter(down: boolean): void
  calibrate(): Promise<CalibrationResult>
}

const KEY = Symbol('yui-pad')

export const providePad = (ctx: PadContext) => setContext(KEY, ctx)
export const usePad = () => getContext<PadContext>(KEY)

/** Pointer position relative to an element: px from its top-left, and −1…1 of its size. */
export function localPoint(el: Element, e: { clientX: number; clientY: number }) {
  const r = el.getBoundingClientRect()
  const x = e.clientX - r.left
  const y = e.clientY - r.top
  const nx = r.width ? Math.max(-1, Math.min(1, (x / r.width) * 2 - 1)) : 0
  const ny = r.height ? Math.max(-1, Math.min(1, (y / r.height) * 2 - 1)) : 0
  return { x, y, nx, ny, w: r.width, h: r.height }
}

/** Capture a pointer so its moves and release reach this element even off its bounds. */
export function capture(e: PointerEvent) {
  try {
    ;(e.currentTarget as Element).setPointerCapture(e.pointerId)
  } catch {
    /* pointer already gone */
  }
}

/** Only use an image that is a data: URI or on this server (contracts §3 Control props). */
export function safeImage(src: unknown, origin = typeof location === 'undefined' ? '' : location.origin): string | null {
  if (typeof src !== 'string' || !src) return null
  if (/^data:image\//i.test(src)) return src.length <= 90_000 ? src : null // ~64 KB of base64
  try {
    const u = new URL(src, origin)
    return u.origin === origin && (u.protocol === 'https:' || u.protocol === 'http:') ? u.href : null
  } catch {
    return null
  }
}
