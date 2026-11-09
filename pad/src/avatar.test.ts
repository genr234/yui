import { describe, expect, test } from 'bun:test'
import {
  SELFIE_BACKGROUND,
  SELFIE_MAX_CHARS,
  avatarImage,
  cameraErrorMessage,
  drawRoundSelfie,
  encodeSelfie,
  squareCrop,
  stopStream,
  type SelfieContext,
} from './avatar'

describe('squareCrop', () => {
  test('centres the square on landscape and portrait frames', () => {
    expect(squareCrop(640, 480)).toEqual({ sx: 80, sy: 0, size: 480 })
    expect(squareCrop(480, 640)).toEqual({ sx: 0, sy: 80, size: 480 })
    expect(squareCrop(300, 300)).toEqual({ sx: 0, sy: 0, size: 300 })
  })
})

describe('drawRoundSelfie', () => {
  test('fills the corners, clips a circle and draws the mirrored centre square', () => {
    const calls: string[] = []
    const ctx: SelfieContext = {
      fillStyle: '',
      fillRect: (...a) => calls.push(`fillRect ${a.join(',')}`),
      save: () => calls.push('save'),
      restore: () => calls.push('restore'),
      beginPath: () => calls.push('beginPath'),
      arc: (x, y, r) => calls.push(`arc ${x},${y},${r}`),
      clip: () => calls.push('clip'),
      translate: (...a) => calls.push(`translate ${a.join(',')}`),
      scale: (...a) => calls.push(`scale ${a.join(',')}`),
      drawImage: (_img, ...a) => calls.push(`drawImage ${a.join(',')}`),
    }
    drawRoundSelfie(ctx, {} as CanvasImageSource, 640, 480, 96)
    expect(ctx.fillStyle).toBe(SELFIE_BACKGROUND)
    expect(calls).toEqual([
      'fillRect 0,0,96,96',
      'save',
      'beginPath',
      'arc 48,48,48',
      'clip',
      'translate 96,0',
      'scale -1,1',
      'drawImage 80,0,480,480,0,0,96,96',
      'restore',
    ])
  })
})

describe('encodeSelfie', () => {
  const jpeg = (chars: number) => 'data:image/jpeg;base64,' + 'A'.repeat(chars)

  test('returns the first quality that fits', () => {
    const tried: number[] = []
    const url = encodeSelfie((type, q) => {
      expect(type).toBe('image/jpeg')
      tried.push(q)
      return jpeg(q > 0.6 ? SELFIE_MAX_CHARS : 4000)
    })
    expect(url).toBe(jpeg(4000))
    expect(tried).toEqual([0.82, 0.72, 0.62, 0.5])
  })

  test('gives up when nothing fits or the browser made a PNG', () => {
    expect(encodeSelfie(() => jpeg(SELFIE_MAX_CHARS))).toBeNull()
    expect(encodeSelfie(() => 'data:image/png;base64,AAAA')).toBeNull()
    expect(encodeSelfie(() => 'data:,')).toBeNull()
  })
})

describe('avatarImage', () => {
  test('only jpeg and webp data urls render as images', () => {
    expect(avatarImage('data:image/jpeg;base64,/9j/')).toBe('data:image/jpeg;base64,/9j/')
    expect(avatarImage('data:image/webp;base64,UklG')).toBe('data:image/webp;base64,UklG')
    expect(avatarImage('data:image/svg+xml;base64,PHN2Zz4=')).toBeNull()
    expect(avatarImage('https://example.com/a.jpg')).toBeNull()
    expect(avatarImage('🦊')).toBeNull()
    expect(avatarImage(null)).toBeNull()
  })
})

describe('camera helpers', () => {
  test('error messages fall back to the emoji choices', () => {
    expect(cameraErrorMessage({ name: 'NotAllowedError' })).toContain('blocked')
    expect(cameraErrorMessage({ name: 'NotFoundError' })).toContain('No front camera')
    expect(cameraErrorMessage({ name: 'NotReadableError' })).toContain('busy')
    expect(cameraErrorMessage(new Error('x'))).toContain('emoji')
    expect(cameraErrorMessage(null)).toContain('emoji')
  })

  test('stopStream stops every track', () => {
    const stopped: string[] = []
    const track = (id: string) => ({ stop: () => stopped.push(id) })
    stopStream({ getTracks: () => [track('a'), track('b')] } as unknown as MediaStream)
    stopStream(null)
    expect(stopped).toEqual(['a', 'b'])
  })
})
