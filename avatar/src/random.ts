// Random creatures: a starting point (and the dice button) for the builder.
// Players change every part from there.
import {
  ARM_COUNT,
  BODY_COUNT,
  BROW_COUNT,
  LEG_COUNT,
  MOUTH_COUNT,
  NOSE_COUNT,
  TOP_COUNT,
  type Creature,
} from './format'
import { defaultEyes, defaultMouthAt, defaultNoseAt } from './layout'

export type Rng = () => number

/** A small seeded generator (mulberry32) over a string. */
export function seededRng(seed: string): Rng {
  let h = 2166136261
  for (let i = 0; i < seed.length; i++) h = Math.imul(h ^ seed.charCodeAt(i), 16777619)
  let a = h >>> 0
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const pick = (rng: Rng, count: number) => Math.min(count - 1, Math.floor(rng() * count))

/** Eye styles that move; the closed and crossed-out ones are left for people to choose. */
const LIVELY_EYES = 6

/** A random creature in the player's colour (no photo). */
export function randomCreature(rng: Rng = Math.random): Creature {
  const body = pick(rng, BODY_COUNT)
  const roll = rng()
  const eyeCount = roll < 0.22 ? 1 : roll < 0.85 ? 2 : 3
  return {
    body,
    eyeStyle: pick(rng, LIVELY_EYES),
    mouth: 1 + pick(rng, MOUTH_COUNT - 1),
    nose: rng() < 0.3 ? 1 + pick(rng, NOSE_COUNT - 1) : 0,
    brow: rng() < 0.35 ? 1 + pick(rng, BROW_COUNT - 1) : 0,
    top: rng() < 0.75 ? 1 + pick(rng, TOP_COUNT - 1) : 0,
    arms: 1 + pick(rng, ARM_COUNT - 1),
    legs: 1 + pick(rng, LEG_COUNT - 1),
    eyes: defaultEyes(body, eyeCount),
    mouthAt: defaultMouthAt(body),
    noseAt: defaultNoseAt(body),
    photo: null,
  }
}
