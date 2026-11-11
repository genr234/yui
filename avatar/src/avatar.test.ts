import { describe, expect, test } from 'bun:test'
import { encodeCreature, imageUrl, isCreature, parseAvatar, parseCreature, withoutPhoto, type Creature } from './format'
import { bodySize, defaultEyes, layoutCreature, photoSquare, withBody } from './layout'
import { randomCreature, seededRng } from './random'
import { atRest, joltPupil, makePupil, stepPupil } from './physics'

const JPEG = 'data:image/jpeg;base64,/9j/4AAQ'

const base: Creature = {
  body: 1,
  eyeStyle: 0,
  mouth: 3,
  nose: 0,
  brow: 2,
  top: 1,
  arms: 2,
  legs: 4,
  eyes: [
    { x: 33, y: 38, r: 13 },
    { x: 67, y: 38, r: 13 },
  ],
  mouthAt: { x: 50, y: 70 },
  noseAt: { x: 50, y: 55 },
  photo: null,
}

describe('creature strings', () => {
  test('round trip', () => {
    const text = encodeCreature(base)
    expect(text).toBe('yui:c1:1.0.3.0.2.1.2.4:33-38-13,67-38-13:50-70:50-55')
    expect(parseCreature(text)).toEqual(base)
    const withPhoto = { ...base, photo: JPEG }
    expect(parseCreature(encodeCreature(withPhoto))).toEqual(withPhoto)
    expect(parseCreature(encodeCreature({ ...base, eyes: [] }))?.eyes).toEqual([])
  })

  test('encoding clamps and rounds', () => {
    const text = encodeCreature({ ...base, body: 99, eyes: [{ x: -4, y: 120.4, r: 1 }], mouthAt: { x: 49.6, y: 0 } })
    expect(text).toBe('yui:c1:5.0.3.0.2.1.2.4:0-100-4:50-0:50-55')
  })

  test('rejects anything off-format', () => {
    for (const bad of [
      'yui:c1:',
      'yui:c2:1.0.3.0.2.1.2.4::50-70:50-55',
      'yui:c1:6.0.3.0.2.1.2.4::50-70:50-55', // body out of range
      'yui:c1:1.0.3.0.2.1.2::50-70:50-55', // seven parts
      'yui:c1:1.0.3.0.2.1.2.4:1-1-4,1-1-4,1-1-4,1-1-4:50-70:50-55', // four eyes
      'yui:c1:1.0.3.0.2.1.2.4:33-38-99:50-70:50-55', // eye too big
      'yui:c1:1.0.3.0.2.1.2.4::50-170:50-55',
      'yui:c1:1.0.3.0.2.1.2.4::50-70:50-55:extra',
      'yui:c1:1.0.3.0.2.1.2.4::5e1-70:50-55',
      'yui:c1:1.0.3.0.2.1.2.4::50-70:50-55|data:image/svg+xml;base64,AAAA',
      'yui:c1:1.0.3.0.2.1.2.4::50-70:50-55|javascript:alert(1)',
      ' yui:c1:1.0.3.0.2.1.2.4::50-70:50-55',
    ]) {
      expect(parseCreature(bad)).toBeNull()
      expect(isCreature(bad)).toBe(false)
    }
  })

  test('parseAvatar sorts every kind', () => {
    expect(parseAvatar(null)).toEqual({ kind: 'none' })
    expect(parseAvatar('')).toEqual({ kind: 'none' })
    expect(parseAvatar('🦊')).toEqual({ kind: 'emoji', text: '🦊' })
    expect(parseAvatar(JPEG)).toEqual({ kind: 'image', url: JPEG })
    expect(parseAvatar(encodeCreature(base))).toEqual({ kind: 'creature', creature: base })
    // A broken creature is nothing, never shown as text.
    expect(parseAvatar('yui:c1:nope')).toEqual({ kind: 'none' })
    expect(imageUrl('data:image/png;base64,AAAA')).toBeNull()
    expect(withoutPhoto({ ...base, photo: JPEG }).photo).toBeNull()
  })
})

describe('layout', () => {
  test('face view leaves out limbs, full view grows the bounds', () => {
    const face = layoutCreature(base, 'face')
    const full = layoutCreature(base, 'full')
    expect(face.back.map((s) => s.name)).toEqual(['top_horn_large', 'top_horn_large'])
    expect(full.back.length).toBe(6)
    expect(full.bounds.w).toBeGreaterThan(face.bounds.w)
    expect(full.bounds.h).toBeGreaterThan(face.bounds.h)
    expect(face.eyes.length).toBe(2)
    expect(face.brows.length).toBe(2)
    // Limbs mirror left and right.
    expect(full.back.filter((s) => s.flip).length).toBe(3)
  })

  test('default eyes stay inside the body', () => {
    for (let body = 0; body < 6; body++) {
      for (const count of [1, 2, 3]) {
        const eyes = defaultEyes(body, count)
        expect(eyes.length).toBe(count)
        for (const e of eyes) {
          expect(e.x - e.r).toBeGreaterThanOrEqual(0)
          expect(e.x + e.r).toBeLessThanOrEqual(100)
        }
      }
    }
  })

  test('changing body keeps the face on the photo', () => {
    const c = { ...base, body: 1, photo: JPEG }
    const moved = withBody(c, 4)
    // The same photo pixel: body 1 is square, so its box is the photo.
    const size = bodySize(4)
    const square = photoSquare(4)
    const eye = moved.eyes[0]
    const u = ((eye.x / 100) * size.w - square.x) / square.w
    expect(u).toBeCloseTo(0.33, 1)
    // And back again lands where it started (within rounding).
    const back = withBody(moved, 1)
    expect(Math.abs(back.eyes[0].x - 33)).toBeLessThanOrEqual(2)
    expect(Math.abs(back.eyes[0].y - 38)).toBeLessThanOrEqual(2)
    // Without a photo nothing is remapped.
    expect(withBody(base, 4).eyes).toEqual(base.eyes)
  })
})

describe('googly physics', () => {
  test('a pupil falls, stays inside the eye and comes to rest at the bottom', () => {
    const p = makePupil(0)
    p.x = 0
    p.y = -1
    for (let i = 0; i < 600; i++) {
      stepPupil(p, 1 / 60, 0, 1)
      expect(Math.hypot(p.x, p.y)).toBeLessThanOrEqual(1.0001)
    }
    expect(atRest(p, 0, 1)).toBe(true)
    expect(p.y).toBeGreaterThan(0.99)
  })

  test('a jolt moves it and it settles again', () => {
    const p = makePupil(1)
    expect(atRest(p, 0, 1)).toBe(true)
    joltPupil(p, 3, -6)
    stepPupil(p, 1 / 60, 0, 1)
    expect(atRest(p, 0, 1)).toBe(false)
    for (let i = 0; i < 900; i++) stepPupil(p, 1 / 60, 0, 1)
    expect(atRest(p, 0, 1)).toBe(true)
  })

  test('two eyes do not move in step', () => {
    const a = makePupil(0)
    const b = makePupil(1)
    joltPupil(a, 2, -5)
    joltPupil(b, 2, -5)
    for (let i = 0; i < 12; i++) {
      stepPupil(a, 1 / 60, 0, 1)
      stepPupil(b, 1 / 60, 0, 1)
    }
    expect(Math.abs(a.x - b.x) + Math.abs(a.y - b.y)).toBeGreaterThan(0.01)
  })

  test('tilting moves the rest point', () => {
    const p = makePupil(0)
    for (let i = 0; i < 900; i++) stepPupil(p, 1 / 60, 1, 0)
    expect(p.x).toBeGreaterThan(0.99)
    expect(atRest(p, 1, 0)).toBe(true)
  })
})

describe('random creatures', () => {
  test('are always valid and repeat for the same seed', () => {
    const rng = seededRng('seed')
    for (let i = 0; i < 200; i++) {
      const c = randomCreature(rng)
      expect(parseCreature(encodeCreature(c))).toEqual(c)
      expect(c.eyes.length).toBeGreaterThan(0)
    }
    expect(randomCreature(seededRng('a'))).toEqual(randomCreature(seededRng('a')))
    expect(encodeCreature(randomCreature(seededRng('a')))).not.toBe(encodeCreature(randomCreature(seededRng('b'))))
  })
})
