import { describe, expect, test } from 'bun:test'
import { encodeCreature } from '@yui/avatar'
import {
  SELFIE_MAX_CHARS,
  avatarImage,
  cameraErrorMessage,
  creatureOf,
  drawSelfie,
  encodeSelfie,
  rememberAvatar,
  squareCrop,
  startingAvatar,
  stopStream,
  withSelfie,
  type SelfieContext,
} from './avatar'
import { faceSpots } from './face/face'

describe('squareCrop', () => {
  test('centres the square on landscape and portrait frames', () => {
    expect(squareCrop(640, 480)).toEqual({ sx: 80, sy: 0, size: 480 })
    expect(squareCrop(480, 640)).toEqual({ sx: 0, sy: 80, size: 480 })
    expect(squareCrop(300, 300)).toEqual({ sx: 0, sy: 0, size: 300 })
  })
})

describe('drawSelfie', () => {
  test('draws the mirrored centre square', () => {
    const calls: string[] = []
    const ctx: SelfieContext = {
      save: () => calls.push('save'),
      restore: () => calls.push('restore'),
      translate: (...a) => calls.push(`translate ${a.join(',')}`),
      scale: (...a) => calls.push(`scale ${a.join(',')}`),
      drawImage: (_img, ...a) => calls.push(`drawImage ${a.join(',')}`),
    }
    drawSelfie(ctx, {} as CanvasImageSource, 640, 480, 128)
    expect(calls).toEqual(['save', 'translate 128,0', 'scale -1,1', 'drawImage 80,0,480,480,0,0,128,128', 'restore'])
  })
})

describe('creature avatars', () => {
  const memory = () => {
    const data = new Map<string, string>()
    return {
      data,
      getItem: (k: string) => data.get(k) ?? null,
      setItem: (k: string, v: string) => void data.set(k, v),
      removeItem: (k: string) => void data.delete(k),
    }
  }
  const JPEG = 'data:image/jpeg;base64,/9j/4AAQ'

  test('a new phone starts with a random creature to build on', () => {
    const avatar = startingAvatar(memory(), () => 0.5)
    expect(creatureOf(avatar)).not.toBeNull()
    expect(startingAvatar(null, () => 0.5)).toBe(avatar)
  })

  test('the creature is remembered without its photo; old emoji are dropped', () => {
    const store = memory()
    const creature = { ...creatureOf(startingAvatar(store, () => 0.2))!, photo: JPEG }
    rememberAvatar(store, encodeCreature(creature))
    expect(store.data.get('yui_pad_avatar')).toBe(encodeCreature({ ...creature, photo: null }))
    expect(startingAvatar(store)).toBe(encodeCreature({ ...creature, photo: null }))

    rememberAvatar(store, null)
    expect(store.data.has('yui_pad_avatar')).toBe(false)
    store.data.set('yui_pad_avatar', '🦊')
    expect(creatureOf(startingAvatar(store, () => 0.5))).not.toBeNull()
  })

  test('a selfie moves the eyes, nose and mouth onto the face', () => {
    const base = { ...creatureOf(startingAvatar(null, () => 0.5))!, body: 1 } // body 1 is square: its box is the photo
    const spots = faceSpots({ x: 0.5, y: 0.5, size: 0.6 })
    const c = withSelfie(base, JPEG, spots)
    expect(c.photo).toBe(JPEG)
    expect(c.eyes.length).toBe(2)
    expect(c.eyes[0].x).toBeLessThan(50)
    expect(c.eyes[1].x).toBeGreaterThan(50)
    expect(c.eyes[0].y).toBeLessThan(c.noseAt.y)
    expect(c.noseAt.y).toBeLessThan(c.mouthAt.y)
    expect(creatureOf(encodeCreature(c))).toEqual(c)

    // On a tall body the photo is wider than the body, so the face spreads out.
    const tall = withSelfie({ ...base, body: 4 }, JPEG, spots)
    expect(tall.eyes[1].x - tall.eyes[0].x).toBeGreaterThan(c.eyes[1].x - c.eyes[0].x)

    // No face found: only the photo changes.
    expect(withSelfie(base, JPEG, null)).toEqual({ ...base, photo: JPEG })
  })
})

describe('encodeSelfie', () => {
  const jpeg = (chars: number) => 'data:image/jpeg;base64,' + 'A'.repeat(chars)

  test('returns the first quality that fits', () => {
    const tried: number[] = []
    const url = encodeSelfie((type, q) => {
      expect(type).toBe('image/jpeg')
      tried.push(q)
      return jpeg(q > 0.6 ? SELFIE_MAX_CHARS + 1 : 4000)
    })
    expect(url).toBe(jpeg(4000))
    expect(tried).toEqual([0.8, 0.7, 0.6])
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
  test('error messages say the builder still works', () => {
    expect(cameraErrorMessage({ name: 'NotAllowedError' })).toContain('blocked')
    expect(cameraErrorMessage({ name: 'NotFoundError' })).toContain('No front camera')
    expect(cameraErrorMessage({ name: 'NotReadableError' })).toContain('busy')
    expect(cameraErrorMessage(new Error('x'))).toContain('build your creature')
    expect(cameraErrorMessage(null)).toContain('build your creature')
  })

  test('stopStream stops every track', () => {
    const stopped: string[] = []
    const track = (id: string) => ({ stop: () => stopped.push(id) })
    stopStream({ getTracks: () => [track('a'), track('b')] } as unknown as MediaStream)
    stopStream(null)
    expect(stopped).toEqual(['a', 'b'])
  })
})
