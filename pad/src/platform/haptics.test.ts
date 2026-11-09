import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import {
  HAPTIC_PATTERNS,
  cleanPattern,
  playHaptic,
  resolveHaptic,
  scaleIntensity,
  setVibrationEnabled,
  vibrationEnabled,
} from './haptics'

const g = globalThis as any
let vibrated: unknown[] = []
let store: Map<string, string>
const saved = { navigator: g.navigator, localStorage: g.localStorage }

beforeEach(() => {
  vibrated = []
  store = new Map()
  Object.defineProperty(g, 'navigator', { configurable: true, value: { vibrate: (p: unknown) => (vibrated.push(p), true), userAgent: 'Android' } })
  Object.defineProperty(g, 'localStorage', {
    configurable: true,
    value: {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    },
  })
})
afterEach(() => {
  Object.defineProperty(g, 'navigator', { configurable: true, value: saved.navigator })
  Object.defineProperty(g, 'localStorage', { configurable: true, value: saved.localStorage })
})

describe('haptic vocabulary', () => {
  test('every protocol name resolves to a valid pattern', () => {
    for (const name of ['tap', 'success', 'fail', 'hit', 'heavy', 'heartbeat', 'countdown', 'win']) {
      const steps = resolveHaptic({ name })!
      expect(steps.length).toBeGreaterThan(0)
      expect(cleanPattern(steps)).toEqual(steps) // within 16 steps / 1 s / 4 s
    }
    expect(Object.keys(HAPTIC_PATTERNS)).toHaveLength(8)
  })

  test('unknown names and junk resolve to nothing', () => {
    expect(resolveHaptic({ name: 'zap' })).toBeNull()
    expect(resolveHaptic({ name: 'constructor' })).toBeNull()
    expect(resolveHaptic({})).toBeNull()
    expect(resolveHaptic({ pattern: [] })).toBeNull()
    expect(resolveHaptic({ pattern: 'x' })).toBeNull()
  })

  test('custom patterns pass through and are clamped', () => {
    expect(resolveHaptic({ pattern: [30, 20, 30] })).toEqual([30, 20, 30]) // the original message form
    expect(resolveHaptic({ pattern: Array(20).fill(1) })).toHaveLength(16)
    expect(resolveHaptic({ pattern: [1000, 1000, 1000, 1000, 1000] })).toEqual([1000, 1000, 1000, 1000, 0])
  })

  test('intensity shortens pulses, keeps gaps, and 0 is silent', () => {
    expect(scaleIntensity([100, 50, 100], 0.5)).toEqual([50, 50, 50])
    expect(scaleIntensity([100], 1)).toEqual([100])
    expect(resolveHaptic({ name: 'heavy', intensity: 0 })).toBeNull()
    expect(resolveHaptic({ pattern: [100, 50, 100], intensity: 0.25 })).toEqual([25, 50, 25])
    expect(resolveHaptic({ name: 'hit', intensity: 5 })).toEqual(HAPTIC_PATTERNS.hit)
  })
})

describe('playHaptic', () => {
  test('Android: names and patterns go to navigator.vibrate', () => {
    playHaptic({ name: 'win' })
    playHaptic({ pattern: [40, 60, 40] })
    expect(vibrated).toEqual([HAPTIC_PATTERNS.win, [40, 60, 40]])
  })

  test('the Vibration switch silences it and persists', () => {
    expect(vibrationEnabled()).toBe(true)
    setVibrationEnabled(false)
    expect(vibrationEnabled()).toBe(false)
    expect(store.get('yui_pad_vibration')).toBe('off')
    vibrated = []
    playHaptic({ name: 'win' })
    expect(vibrated).toEqual([])
    setVibrationEnabled(true)
    expect(vibrationEnabled()).toBe(true)
    playHaptic({ name: 'tap' })
    expect(vibrated).toEqual([HAPTIC_PATTERNS.tap])
  })

  test('no vibrate API and not iOS: degrades silently', () => {
    Object.defineProperty(g, 'navigator', { configurable: true, value: { userAgent: 'Desktop' } })
    expect(() => playHaptic({ name: 'win' })).not.toThrow()
  })

  test('unreadable storage defaults to on', () => {
    Object.defineProperty(g, 'localStorage', {
      configurable: true,
      value: {
        getItem() {
          throw new Error('blocked')
        },
      },
    })
    expect(vibrationEnabled()).toBe(true)
  })
})
