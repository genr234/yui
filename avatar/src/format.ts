// The avatar string every surface shares (contracts §2 "Avatar").
//
// A player's avatar is null or one string of one of three kinds:
//   - an emoji (older pads)
//   - a JPEG/WebP data URL: a plain round selfie (older pads)
//   - a creature: `yui:c1:<parts>:<eyes>:<mouth>:<nose>[|<photo data URL>]`
//
// A creature is a monster built from parts. With a photo, the selfie fills
// the body shape instead of the player's colour, so monster parts go on a
// selfie and a selfie goes on a monster with the same recipe.
//
//   parts  eight numbers joined by ".": body, eye style, mouth, nose, brow,
//          top, arms, legs. 0 means "none" for everything but body and eye style.
//   eyes   up to three "x-y-r" joined by ",", or empty for no eyes.
//   mouth, nose   "x-y".
//
// Positions are whole percentages of the body's box (0–100); an eye's radius
// is a percentage of the body's width.

export const CREATURE_PREFIX = 'yui:c1:'

/** Longest avatar string Rails accepts. */
export const AVATAR_MAX_CHARS = 12 * 1024

const IMAGE_PREFIXES = ['data:image/jpeg;base64,', 'data:image/webp;base64,'] as const

export const BODY_COUNT = 6
/** Eye styles; the last two (closed, crossed out) don't move. */
export const EYE_STYLE_COUNT = 8
/** Counts below include 0 = none. */
export const MOUTH_COUNT = 15
export const NOSE_COUNT = 5
export const BROW_COUNT = 4
export const TOP_COUNT = 7
export const ARM_COUNT = 6
export const LEG_COUNT = 6

export const MAX_EYES = 3
export const EYE_RADIUS_MIN = 4
export const EYE_RADIUS_MAX = 40

export interface Point {
  x: number
  y: number
}

export interface Eye extends Point {
  r: number
}

export interface Creature {
  body: number
  eyeStyle: number
  mouth: number
  nose: number
  brow: number
  top: number
  arms: number
  legs: number
  eyes: Eye[]
  mouthAt: Point
  noseAt: Point
  /** A square selfie data URL that fills the body, or null to use the player's colour. */
  photo: string | null
}

export type Avatar =
  | { kind: 'none' }
  | { kind: 'emoji'; text: string }
  | { kind: 'image'; url: string }
  | { kind: 'creature'; creature: Creature }

/** The value when it is a JPEG/WebP data URL, otherwise null. */
export function imageUrl(value: string | null | undefined): string | null {
  if (typeof value !== 'string') return null
  return IMAGE_PREFIXES.some((prefix) => value.startsWith(prefix)) ? value : null
}

const PART_LIMITS = [BODY_COUNT, EYE_STYLE_COUNT, MOUTH_COUNT, NOSE_COUNT, BROW_COUNT, TOP_COUNT, ARM_COUNT, LEG_COUNT]
const INT = /^\d{1,3}$/

function int(text: string, min: number, max: number): number | null {
  if (!INT.test(text)) return null
  const n = Number(text)
  return n >= min && n <= max ? n : null
}

function point(text: string): Point | null {
  const parts = text.split('-')
  if (parts.length !== 2) return null
  const x = int(parts[0], 0, 100)
  const y = int(parts[1], 0, 100)
  return x === null || y === null ? null : { x, y }
}

function eye(text: string): Eye | null {
  const parts = text.split('-')
  if (parts.length !== 3) return null
  const x = int(parts[0], 0, 100)
  const y = int(parts[1], 0, 100)
  const r = int(parts[2], EYE_RADIUS_MIN, EYE_RADIUS_MAX)
  return x === null || y === null || r === null ? null : { x, y, r }
}

/** Parses a creature string; null when it is not exactly in the format above. */
export function parseCreature(value: string): Creature | null {
  if (!value.startsWith(CREATURE_PREFIX) || value.length > AVATAR_MAX_CHARS) return null
  const bar = value.indexOf('|')
  const recipe = value.slice(CREATURE_PREFIX.length, bar < 0 ? undefined : bar)
  let photo: string | null = null
  if (bar >= 0) {
    photo = imageUrl(value.slice(bar + 1))
    if (!photo) return null
  }
  const sections = recipe.split(':')
  if (sections.length !== 4) return null
  const ids = sections[0].split('.')
  if (ids.length !== PART_LIMITS.length) return null
  const parts: number[] = []
  for (let i = 0; i < ids.length; i++) {
    const n = int(ids[i], 0, PART_LIMITS[i] - 1)
    if (n === null) return null
    parts.push(n)
  }
  const eyes: Eye[] = []
  if (sections[1] !== '') {
    const list = sections[1].split(',')
    if (list.length > MAX_EYES) return null
    for (const item of list) {
      const e = eye(item)
      if (!e) return null
      eyes.push(e)
    }
  }
  const mouthAt = point(sections[2])
  const noseAt = point(sections[3])
  if (!mouthAt || !noseAt) return null
  const [body, eyeStyle, mouth, nose, brow, top, arms, legs] = parts
  return { body, eyeStyle, mouth, nose, brow, top, arms, legs, eyes, mouthAt, noseAt, photo }
}

const clamp = (n: number, min: number, max: number) => Math.min(max, Math.max(min, Math.round(n)))

/** The creature as an avatar string. Out-of-range numbers are clamped. */
export function encodeCreature(c: Creature): string {
  const ids = [c.body, c.eyeStyle, c.mouth, c.nose, c.brow, c.top, c.arms, c.legs].map((n, i) => clamp(n, 0, PART_LIMITS[i] - 1))
  const eyes = c.eyes
    .slice(0, MAX_EYES)
    .map((e) => `${clamp(e.x, 0, 100)}-${clamp(e.y, 0, 100)}-${clamp(e.r, EYE_RADIUS_MIN, EYE_RADIUS_MAX)}`)
  const at = (p: Point) => `${clamp(p.x, 0, 100)}-${clamp(p.y, 0, 100)}`
  const recipe = `${CREATURE_PREFIX}${ids.join('.')}:${eyes.join(',')}:${at(c.mouthAt)}:${at(c.noseAt)}`
  return c.photo ? `${recipe}|${c.photo}` : recipe
}

/** Emoji avatars are one grapheme; longer text is not shown. */
const EMOJI_MAX_CHARS = 16

/**
 * Sorts any avatar value into its kind. Only a short string that is not a
 * URL of any scheme counts as an emoji, so a broken creature, another data
 * URL or stray markup is `none` and never reaches the page as text.
 */
export function parseAvatar(value: unknown): Avatar {
  if (typeof value !== 'string') return { kind: 'none' }
  const url = imageUrl(value)
  if (url) return { kind: 'image', url }
  if (value.startsWith(CREATURE_PREFIX)) {
    const creature = parseCreature(value)
    return creature ? { kind: 'creature', creature } : { kind: 'none' }
  }
  const text = value.trim()
  if (!text || text.length > EMOJI_MAX_CHARS || /^[a-z][a-z0-9+.-]*:/i.test(text)) return { kind: 'none' }
  return { kind: 'emoji', text }
}

/** True when the value is a creature string. */
export function isCreature(value: string | null | undefined): boolean {
  return typeof value === 'string' && parseCreature(value) !== null
}

/** The creature without its photo: short enough to keep in localStorage. */
export function withoutPhoto(c: Creature): Creature {
  return { ...c, photo: null }
}
