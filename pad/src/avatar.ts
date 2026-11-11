// Player avatars (contracts §2): a creature the player builds, optionally
// with a selfie that fills its body. The camera code stays thin; cropping,
// encoding and the creature maths are pure so they can be tested without a
// browser.
import {
  bodySize,
  encodeCreature,
  parseAvatar,
  photoSquare,
  randomCreature,
  withoutPhoto,
  type Creature,
  type Rng,
} from '@yui/avatar'
import { avatarImage } from '@yui/protocol'
import type { FaceSpots } from './face/face'

export { avatarImage }

/** Selfie edge in pixels. It fills a creature's body, so it is square, not round. */
export const SELFIE_SIZE = 128
/** Target for the data URL. Rails caps the whole avatar string at 12 KB; the recipe needs under 100 characters. */
export const SELFIE_MAX_CHARS = 11 * 1024
/** JPEG qualities to try, best first, until the data URL fits. */
export const SELFIE_QUALITIES = [0.8, 0.7, 0.6, 0.5, 0.4, 0.3] as const

const STORAGE_KEY = 'yui_pad_avatar'

/** The creature in an avatar string, or null for anything else. */
export function creatureOf(avatar: string | null | undefined): Creature | null {
  const parsed = parseAvatar(avatar)
  return parsed.kind === 'creature' ? parsed.creature : null
}

/**
 * The avatar to start the join form with: the creature this phone made last
 * time, or a fresh random one to build on.
 */
export function startingAvatar(storage: Pick<Storage, 'getItem'> | null, rng: Rng = Math.random): string {
  let saved: string | null = null
  try {
    saved = storage?.getItem(STORAGE_KEY) ?? null
  } catch {
    saved = null
  }
  const creature = creatureOf(saved)
  return encodeCreature(creature ? withoutPhoto(creature) : randomCreature(rng))
}

/** Remembers the creature for next time. The photo never stays on the phone. */
export function rememberAvatar(storage: Pick<Storage, 'setItem' | 'removeItem'> | null, avatar: string | null): void {
  try {
    const creature = creatureOf(avatar)
    if (creature) storage?.setItem(STORAGE_KEY, encodeCreature(withoutPhoto(creature)))
    else storage?.removeItem(STORAGE_KEY)
  } catch {
    /* storage unavailable */
  }
}

/**
 * Puts a selfie on a creature: the photo fills the body and the eyes, nose
 * and mouth move onto the face that was found. `spots` are shares of the
 * photo (0–1); with none the parts stay where they were for the player to drag.
 */
export function withSelfie(c: Creature, photo: string, spots: FaceSpots | null): Creature {
  if (!spots) return { ...c, photo }
  const square = photoSquare(c.body)
  const size = bodySize(c.body)
  const pct = (n: number) => Math.min(100, Math.max(0, Math.round(n)))
  const at = (p: { x: number; y: number }) => ({
    x: pct(((square.x + p.x * square.w) / size.w) * 100),
    y: pct(((square.y + p.y * square.h) / size.h) * 100),
  })
  return {
    ...c,
    photo,
    eyes: spots.eyes.map((e) => ({ ...at(e), r: Math.min(40, Math.max(4, Math.round(((e.r * square.w) / size.w) * 100))) })),
    mouthAt: at(spots.mouth),
    noseAt: at(spots.nose),
  }
}

export interface SquareCrop {
  sx: number
  sy: number
  size: number
}

/** The centred square of a `width`×`height` frame. */
export function squareCrop(width: number, height: number): SquareCrop {
  const size = Math.max(0, Math.min(width, height))
  return { sx: (width - size) / 2, sy: (height - size) / 2, size }
}

/** The 2D context calls `drawSelfie` makes (a subset of CanvasRenderingContext2D). */
export interface SelfieContext {
  save(): void
  restore(): void
  translate(x: number, y: number): void
  scale(x: number, y: number): void
  drawImage(image: CanvasImageSource, sx: number, sy: number, sw: number, sh: number, dx: number, dy: number, dw: number, dh: number): void
}

/**
 * Draws the centred square of `source` into a `size`×`size` square, mirrored
 * like the live preview so the photo matches what the player saw.
 */
export function drawSelfie(ctx: SelfieContext, source: CanvasImageSource, width: number, height: number, size = SELFIE_SIZE): void {
  const crop = squareCrop(width, height)
  ctx.save()
  ctx.translate(size, 0)
  ctx.scale(-1, 1)
  ctx.drawImage(source, crop.sx, crop.sy, crop.size, crop.size, 0, 0, size, size)
  ctx.restore()
}

/**
 * Encodes with falling JPEG quality until the data URL fits `maxChars`.
 * Returns null when the browser can't make a JPEG (it falls back to PNG) or
 * nothing fits.
 */
export function encodeSelfie(
  toDataURL: (type: string, quality: number) => string,
  maxChars = SELFIE_MAX_CHARS,
): string | null {
  for (const quality of SELFIE_QUALITIES) {
    const url = toDataURL('image/jpeg', quality)
    if (!avatarImage(url)?.startsWith('data:image/jpeg;')) return null
    if (url.length <= maxChars) return url
  }
  return null
}

/** Grabs the current video frame as a square selfie data URL (null on failure). */
export function captureSelfie(video: HTMLVideoElement, doc: Document = document): string | null {
  const width = video.videoWidth
  const height = video.videoHeight
  if (!width || !height) return null
  const canvas = doc.createElement('canvas')
  canvas.width = SELFIE_SIZE
  canvas.height = SELFIE_SIZE
  const ctx = canvas.getContext('2d')
  if (!ctx) return null
  drawSelfie(ctx, video, width, height)
  return encodeSelfie((type, quality) => canvas.toDataURL(type, quality))
}

export function cameraSupported(nav: Navigator | undefined = typeof navigator === 'undefined' ? undefined : navigator): boolean {
  return typeof nav?.mediaDevices?.getUserMedia === 'function'
}

/** The front camera at a small size (we only keep 128×128). */
export function openFrontCamera(nav: Navigator = navigator): Promise<MediaStream> {
  return nav.mediaDevices.getUserMedia({
    audio: false,
    video: { facingMode: 'user', width: { ideal: 480 }, height: { ideal: 480 } },
  })
}

export function stopStream(stream: MediaStream | null | undefined): void {
  for (const track of stream?.getTracks() ?? []) track.stop()
}

/** A short message for a failed or refused camera; the builder stays usable without it. */
export function cameraErrorMessage(error: unknown): string {
  const name = (error as { name?: unknown } | null)?.name
  switch (name) {
    case 'NotAllowedError':
    case 'SecurityError':
      return 'Camera access was blocked. You can still build your creature.'
    case 'NotFoundError':
    case 'OverconstrainedError':
      return 'No front camera found. You can still build your creature.'
    case 'NotReadableError':
    case 'AbortError':
      return 'The camera is busy. You can still build your creature.'
    default:
      return "Couldn't use the camera. You can still build your creature."
  }
}
