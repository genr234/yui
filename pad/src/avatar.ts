// Player avatars (contracts §2): one emoji, or a small round selfie sent as a
// JPEG data URL. The camera code stays thin; cropping and encoding are pure
// so they can be tested without a browser.
import { avatarImage } from '@yui/protocol'

export { avatarImage }

export const AVATAR_EMOJIS = ['🦊', '🐼', '🐸', '🐙', '🦄', '🐯', '🐧', '👾', '🤖', '🌟', '🍕', '🚀'] as const

/** Selfie edge in pixels. */
export const SELFIE_SIZE = 96
/** Target for the data URL (Rails caps it at 12 KB). */
export const SELFIE_MAX_CHARS = 8 * 1024
/** JPEG qualities to try, best first, until the data URL fits. */
export const SELFIE_QUALITIES = [0.82, 0.72, 0.62, 0.5, 0.4, 0.3] as const
/** Fills the corners outside the circle (JPEG has no transparency). */
export const SELFIE_BACKGROUND = '#142233'

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

/** The 2D context calls `drawRoundSelfie` makes (a subset of CanvasRenderingContext2D). */
export interface SelfieContext {
  fillStyle: string | CanvasGradient | CanvasPattern
  fillRect(x: number, y: number, w: number, h: number): void
  save(): void
  restore(): void
  beginPath(): void
  arc(x: number, y: number, r: number, start: number, end: number): void
  clip(): void
  translate(x: number, y: number): void
  scale(x: number, y: number): void
  drawImage(image: CanvasImageSource, sx: number, sy: number, sw: number, sh: number, dx: number, dy: number, dw: number, dh: number): void
}

/**
 * Draws the centred square of `source` into a `size`×`size` circle, mirrored
 * like the live preview so the photo matches what the player saw.
 */
export function drawRoundSelfie(
  ctx: SelfieContext,
  source: CanvasImageSource,
  width: number,
  height: number,
  size = SELFIE_SIZE,
): void {
  const crop = squareCrop(width, height)
  ctx.fillStyle = SELFIE_BACKGROUND
  ctx.fillRect(0, 0, size, size)
  ctx.save()
  ctx.beginPath()
  ctx.arc(size / 2, size / 2, size / 2, 0, Math.PI * 2)
  ctx.clip()
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

/** Grabs the current video frame as a round selfie data URL (null on failure). */
export function captureSelfie(video: HTMLVideoElement, doc: Document = document): string | null {
  const width = video.videoWidth
  const height = video.videoHeight
  if (!width || !height) return null
  const canvas = doc.createElement('canvas')
  canvas.width = SELFIE_SIZE
  canvas.height = SELFIE_SIZE
  const ctx = canvas.getContext('2d')
  if (!ctx) return null
  drawRoundSelfie(ctx, video, width, height)
  return encodeSelfie((type, quality) => canvas.toDataURL(type, quality))
}

export function cameraSupported(nav: Navigator | undefined = typeof navigator === 'undefined' ? undefined : navigator): boolean {
  return typeof nav?.mediaDevices?.getUserMedia === 'function'
}

/** The front camera at a small size (we only keep 96×96). */
export function openFrontCamera(nav: Navigator = navigator): Promise<MediaStream> {
  return nav.mediaDevices.getUserMedia({
    audio: false,
    video: { facingMode: 'user', width: { ideal: 480 }, height: { ideal: 480 } },
  })
}

export function stopStream(stream: MediaStream | null | undefined): void {
  for (const track of stream?.getTracks() ?? []) track.stop()
}

/** A short message for a failed or refused camera; the emoji row stays usable. */
export function cameraErrorMessage(error: unknown): string {
  const name = (error as { name?: unknown } | null)?.name
  switch (name) {
    case 'NotAllowedError':
    case 'SecurityError':
      return 'Camera access was blocked. Pick an emoji instead.'
    case 'NotFoundError':
    case 'OverconstrainedError':
      return 'No front camera found. Pick an emoji instead.'
    case 'NotReadableError':
    case 'AbortError':
      return 'The camera is busy. Pick an emoji instead.'
    default:
      return "Couldn't use the camera. Pick an emoji instead."
  }
}
