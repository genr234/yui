import { describe, expect, test } from 'bun:test'
import { get } from 'svelte/store'
import {
  DEFAULT_CALIBRATION,
  POINTER_YAW_RANGE,
  PointerIntegrator,
  STANDARD_GRAVITY as G,
  calibrate,
  remoteFlip,
  toRemoteFrame,
  type MotionListener,
  type RawMotion,
} from './motion'
import { cleanPattern, tickTimes } from './haptics'
import { soundAllowed } from './sound'
import { InputModel } from '../input/model'
import { MotionController } from '../input/motion'

const raw = (p: Partial<RawMotion> = {}): RawMotion => ({
  ax: 0,
  ay: 0,
  az: G,
  alpha: 0,
  beta: 0,
  gamma: 0,
  hasGyro: true,
  interval: 16,
  ...p,
})

describe('remote frame', () => {
  test('flat, screen up reads +1 g on z (W3C / Android signs)', () => {
    const f = toRemoteFrame(raw(), DEFAULT_CALIBRATION)
    expect(f.accel[0]).toBeCloseTo(0)
    expect(f.accel[1]).toBeCloseTo(0)
    expect(f.accel[2]).toBeCloseTo(1)
  })

  test('gyro: pitch = beta (about x), yaw = alpha (about z), roll = gamma (about y)', () => {
    const f = toRemoteFrame(raw({ beta: 10, alpha: 20, gamma: 30 }), DEFAULT_CALIBRATION)
    expect(f.gyro).toEqual([10, 20, 30])
  })

  test('calibration flips inverted accel and removes gyro bias', () => {
    const cal = { accelSign: -1 as const, gyroBias: [1, 2, 3] as [number, number, number] }
    const f = toRemoteFrame(raw({ az: -G, ay: -G / 2, beta: 11, alpha: 22, gamma: 33 }), cal)
    expect(f.accel[2]).toBeCloseTo(1)
    expect(f.accel[1]).toBeCloseTo(0.5)
    expect(f.gyro).toEqual([10, 20, 30])
  })

  test('no gyro → zero rates', () => {
    expect(toRemoteFrame(raw({ hasGyro: false, beta: 5 }), DEFAULT_CALIBRATION).gyro).toEqual([0, 0, 0])
  })

  test('flip turns the frame 180° about z', () => {
    const f = toRemoteFrame(raw({ ax: G, ay: G, beta: 1, alpha: 2, gamma: 3 }), DEFAULT_CALIBRATION, true)
    expect(f.accel[0]).toBeCloseTo(-1)
    expect(f.accel[1]).toBeCloseTo(-1)
    expect(f.gyro).toEqual([-1, 2, -3])
  })

  test('remote top: screen top in portrait, screen left in landscape', () => {
    expect(remoteFlip(0)).toBe(false)
    expect(remoteFlip(90)).toBe(false)
    expect(remoteFlip(180)).toBe(true)
    expect(remoteFlip(270)).toBe(true)
    expect(remoteFlip(-90)).toBe(true)
  })
})

describe('calibration', () => {
  test('iOS-style inverted gravity → accelSign -1, bias = mean rate', () => {
    const r = calibrate([raw({ az: -G, beta: 0.4, alpha: -0.2 }), raw({ az: -G * 0.98, beta: 0.6, alpha: -0.4 })])
    expect(r.ok).toBe(true)
    if (!r.ok) return
    expect(r.calibration.accelSign).toBe(-1)
    expect(r.calibration.gyroBias[0]).toBeCloseTo(0.5)
    expect(r.calibration.gyroBias[1]).toBeCloseTo(-0.3)
  })

  test('Android-style gravity → +1', () => {
    const r = calibrate([raw()])
    expect(r.ok && r.calibration.accelSign).toBe(1)
  })

  test('rejects a phone that is not flat or not still', () => {
    expect(calibrate([raw({ ay: G, az: 0 })])).toEqual({ ok: false, reason: 'flat' })
    expect(calibrate([raw({ alpha: 90 })])).toEqual({ ok: false, reason: 'still' })
    expect(calibrate([])).toEqual({ ok: false, reason: 'empty' })
  })
})

describe('pointer', () => {
  test('turning right moves right, tilting up moves up (+y down), clamped, recenter', () => {
    const p = new PointerIntegrator()
    // dt is capped at 100 ms per sample (a stalled sensor must not jump the pointer)
    for (let i = 0; i < 5; i++) p.update([0, -POINTER_YAW_RANGE, 0], 100) // half range to the right
    expect(p.value()[0]).toBeCloseTo(0.5)
    expect(p.update([0, -POINTER_YAW_RANGE, 0], 5000)[0]).toBeCloseTo(0.6)
    const [, y] = p.update([10, 0, 0], 50)
    expect(y).toBeLessThan(0)
    for (let i = 0; i < 20; i++) p.update([0, -100, 0], 100)
    expect(p.value()[0]).toBe(1)
    p.recenter()
    expect(p.value()).toEqual([0, 0])
  })
})

describe('MotionController', () => {
  function setup(needsPermission = false) {
    const model = new InputModel()
    let listener: MotionListener | null = null
    let t = 0
    const source = { start: (fn: MotionListener) => (listener = fn), stop: () => (listener = null) }
    const c = new MotionController(model, source, { needsPermission, supported: true, flip: () => false, now: () => t })
    return { model, c, emit: (r: RawMotion) => listener?.(r), started: () => listener !== null, tick: (ms: number) => (t += ms) }
  }

  test('streams only while a motion node is present', () => {
    const s = setup()
    expect(s.started()).toBe(false)
    s.c.setWanted(true, false)
    expect(s.started()).toBe(true)
    s.emit(raw({ beta: 5 }))
    expect(s.model.frame().gyro).toEqual([5, 0, 0])
    expect(s.model.frame().px).toBe(0)
    s.c.setWanted(false, false)
    expect(s.started()).toBe(false)
    expect(s.model.frame().gyro).toEqual([0, 0, 0])
  })

  test('iOS: waits for the permission tap; denial is reported', () => {
    const s = setup(true)
    s.c.setWanted(true, true)
    expect(get(s.c.view).status).toBe('needs-permission')
    expect(s.started()).toBe(false)
    s.c.setPermission(true)
    expect(get(s.c.view).status).toBe('on')
    s.c.setPermission(false)
    expect(get(s.c.view).status).toBe('denied')
  })

  test('pointer integrates and recenters', () => {
    const s = setup()
    s.c.setWanted(true, true)
    s.emit(raw())
    for (let i = 0; i < 5; i++) {
      s.tick(100)
      s.emit(raw({ alpha: -POINTER_YAW_RANGE }))
    }
    expect(s.model.frame().px).toBe(16384)
    s.c.recenter()
    s.tick(16)
    s.emit(raw())
    expect(s.model.frame().px).toBe(0)
  })

  test('one-tap calibrate uses the samples that arrive while waiting', async () => {
    const s = setup()
    s.c.setWanted(true, false)
    const done = s.c.calibrate(async () => {
      s.emit(raw({ az: -G }))
      s.emit(raw({ az: -G }))
    })
    const r = await done
    expect(r.ok).toBe(true)
    expect(get(s.c.view).calibrated).toBe(true)
    s.emit(raw({ az: -G }))
    expect(s.model.frame().accel[2]).toBeCloseTo(1)
  })

  test('a phone without gyro is reported (touch-aim fallback)', () => {
    const s = setup()
    s.c.setWanted(true, true)
    s.emit(raw({ hasGyro: false }))
    expect(get(s.c.view).gyro).toBe(false)
  })
})

describe('haptics and sound', () => {
  test('iOS ticks: one per pulse, every 80 ms on long pulses', () => {
    expect(tickTimes([30, 50, 30])).toEqual([0, 80])
    expect(tickTimes([200])).toEqual([0, 80])
    expect(tickTimes([0, 100, 10])).toEqual([100])
  })

  test('patterns are sanitised', () => {
    expect(cleanPattern([10, -5, 'x', 99999, NaN])).toEqual([10, 0, 5000])
  })

  test('sounds only from this server or data:', () => {
    expect(soundAllowed('/sounds/a.mp3', 'https://yui.example')).toBe(true)
    expect(soundAllowed('data:audio/wav;base64,AA', 'https://yui.example')).toBe(true)
    expect(soundAllowed('https://other.example/a.mp3', 'https://yui.example')).toBe(false)
  })
})
