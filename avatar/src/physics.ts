// Googly-eye physics. A pupil is a loose disc inside the eye: gravity pulls
// it, the rim stops it, and friction lets it settle. Nothing here loops or
// follows a script, so the eyes only move when something moves them.
//
// Positions are in "eye units": (0, 0) is the eye's centre and the pupil's
// centre can travel out to a distance of 1.

export interface Pupil {
  x: number
  y: number
  vx: number
  vy: number
  /** Per-eye weight (about 0.85–1.15) so two eyes never move in step. */
  weight: number
}

/** Pull towards the low side, in eye units per second squared at 1 g. */
export const GRAVITY = 26
/** Air drag per second. */
const DRAG = 2.2
/** How much speed survives a bounce off the rim. */
const BOUNCE = 0.42
/** Friction along the rim while the pupil leans on it. */
const RIM_FRICTION = 5
/** Longest physics step, in seconds. Longer frames are split up. */
const MAX_STEP = 1 / 120
const REST_SPEED = 0.02

export function makePupil(index = 0): Pupil {
  // Fixed, different weights: deterministic, but never in sync.
  const weight = [1, 0.86, 1.13][index % 3]
  return { x: 0, y: 1, vx: 0, vy: 0, weight }
}

function stepOnce(p: Pupil, dt: number, gx: number, gy: number): void {
  p.vx += (gx * GRAVITY * dt) / p.weight
  p.vy += (gy * GRAVITY * dt) / p.weight
  const drag = Math.exp(-DRAG * dt)
  p.vx *= drag
  p.vy *= drag
  p.x += p.vx * dt
  p.y += p.vy * dt

  const dist = Math.hypot(p.x, p.y)
  if (dist <= 1) return
  // Back onto the rim; bounce the outward part of the speed, rub off some of
  // the sideways part.
  const nx = p.x / dist
  const ny = p.y / dist
  p.x = nx
  p.y = ny
  const outward = p.vx * nx + p.vy * ny
  if (outward <= 0) return
  let tx = p.vx - outward * nx
  let ty = p.vy - outward * ny
  const rub = Math.exp(-RIM_FRICTION * dt)
  tx *= rub
  ty *= rub
  const back = outward > 0.6 ? -outward * BOUNCE : 0
  p.vx = tx + back * nx
  p.vy = ty + back * ny
}

/** Advances a pupil by `dt` seconds under gravity (`gx`, `gy` in g; +y is down). */
export function stepPupil(p: Pupil, dt: number, gx: number, gy: number): void {
  let left = Math.min(dt, 0.1)
  while (left > 1e-6) {
    const step = Math.min(left, MAX_STEP)
    stepOnce(p, step, gx, gy)
    left -= step
  }
}

/** Gives the pupil a push, in eye units per second. */
export function joltPupil(p: Pupil, vx: number, vy: number): void {
  p.vx += vx / p.weight
  p.vy += vy / p.weight
}

/** True once the pupil has settled where gravity wants it. */
export function atRest(p: Pupil, gx: number, gy: number): boolean {
  if (Math.hypot(p.vx, p.vy) > REST_SPEED) return false
  const g = Math.hypot(gx, gy)
  if (g < 0.05) return true
  // Resting means leaning on the rim at the lowest point.
  return p.x * (gx / g) + p.y * (gy / g) > 0.995
}

/** Where a pupil rests under the given gravity. */
export function restPupil(p: Pupil, gx: number, gy: number): void {
  const g = Math.hypot(gx, gy)
  p.vx = 0
  p.vy = 0
  p.x = g < 0.05 ? 0 : gx / g
  p.y = g < 0.05 ? 0 : gy / g
}
