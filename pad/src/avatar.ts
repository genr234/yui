// Player avatars (contracts §2): either a creature the player builds
// (optionally with a selfie filling its body) or just a round photo. The
// camera code stays thin; cropping, encoding and the creature maths are pure
// so they can be tested without a browser.
import {
  bodySize,
  encodeCreature,
  imageUrl,
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

/** The photo when the avatar is a plain photo (not a creature), or null. */
export function photoOf(avatar: string | null | undefined): string | null {
  return imageUrl(avatar)
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

/**
 * Remembers the creature for next time. Photos never stay on the phone, so a
 * plain photo avatar leaves the last creature in place for next time.
 */
export function rememberAvatar(storage: Pick<Storage, 'setItem' | 'removeItem'> | null, avatar: string | null): void {
  try {
    const creature = creatureOf(avatar)
    if (creature) storage?.setItem(STORAGE_KEY, encodeCreature(withoutPhoto(creature)))
    else if (!photoOf(avatar)) storage?.removeItem(STORAGE_KEY)
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

/** Most a picked photo can be zoomed in, relative to filling the square. */
export const CROP_ZOOM_MAX = 4

/**
 * The square of a `width`×`height` picture shown when it is zoomed by `zoom`
 * (1 = the short side fills the square) around the centre (`cx`, `cy`), in
 * the picture's pixels. The centre is pulled back so the square never leaves
 * the picture.
 */
export function cropSquare(width: number, height: number, zoom: number, cx: number, cy: number): SquareCrop {
  const size = Math.max(0, Math.min(width, height)) / Math.min(CROP_ZOOM_MAX, Math.max(1, zoom))
  const half = size / 2
  const x = Math.min(width - half, Math.max(half, cx))
  const y = Math.min(height - half, Math.max(half, cy))
  return { sx: x - half, sy: y - half, size }
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
 * Draws the `crop` square of `source` into a `size`×`size` square. Camera
 * frames are mirrored like the live preview so the photo matches what the
 * player saw; a picked photo is not.
 */
export function drawSquare(ctx: SelfieContext, source: CanvasImageSource, crop: SquareCrop, size: number, mirror: boolean): void {
  ctx.save()
  if (mirror) {
    ctx.translate(size, 0)
    ctx.scale(-1, 1)
  }
  ctx.drawImage(source, crop.sx, crop.sy, crop.size, crop.size, 0, 0, size, size)
  ctx.restore()
}

/** Draws the centred square of a camera frame, mirrored, into a `size`×`size` square. */
export function drawSelfie(ctx: SelfieContext, source: CanvasImageSource, width: number, height: number, size = SELFIE_SIZE): void {
  drawSquare(ctx, source, squareCrop(width, height), size, true)
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

/** The `crop` square of `source` as a small square JPEG data URL (null on failure). */
export function capturePhoto(source: CanvasImageSource, crop: SquareCrop, mirror: boolean, doc: Document = document): string | null {
  if (!(crop.size > 0)) return null
  const canvas = doc.createElement('canvas')
  canvas.width = SELFIE_SIZE
  canvas.height = SELFIE_SIZE
  const ctx = canvas.getContext('2d')
  if (!ctx) return null
  // JPEG has no transparency: a see-through PNG goes on white, not black.
  ctx.fillStyle = '#fff'
  ctx.fillRect(0, 0, SELFIE_SIZE, SELFIE_SIZE)
  drawSquare(ctx, source, crop, SELFIE_SIZE, mirror)
  return encodeSelfie((type, quality) => canvas.toDataURL(type, quality))
}

/** Grabs the current video frame as a square selfie data URL (null on failure). */
export function captureSelfie(video: HTMLVideoElement, doc: Document = document): string | null {
  const width = video.videoWidth
  const height = video.videoHeight
  if (!width || !height) return null
  return capturePhoto(video, squareCrop(width, height), true, doc)
}

/** Opens a picked image file. Browsers turn phone photos the right way up (EXIF) for us. */
export function loadImageFile(file: Blob): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const img = new Image()
    img.onload = () => {
      URL.revokeObjectURL(url)
      if (img.naturalWidth && img.naturalHeight) resolve(img)
      else reject(new Error('empty image'))
    }
    img.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error('not an image'))
    }
    img.src = url
  })
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

/** A short message for a failed or refused camera; picking a photo still works without it. */
export function cameraErrorMessage(error: unknown): string {
  const name = (error as { name?: unknown } | null)?.name
  const instead = ' You can pick a photo from your gallery instead.'
  switch (name) {
    case 'NotAllowedError':
    case 'SecurityError':
      return 'Camera access was blocked.' + instead
    case 'NotFoundError':
    case 'OverconstrainedError':
      return 'No front camera found.' + instead
    case 'NotReadableError':
    case 'AbortError':
      return 'The camera is busy.' + instead
    default:
      return "Couldn't use the camera." + instead
  }
}
