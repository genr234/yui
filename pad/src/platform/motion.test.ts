import { describe, expect, test } from 'bun:test'
import { get } from 'svelte/store'
import {
  CALIBRATION_VERSION,
  DEFAULT_CALIBRATION,
  DEFAULT_GYRO_MAP,
  POINTER_YAW_RANGE,
  PointerIntegrator,
  STANDARD_GRAVITY as G,
  isFullCalibration,
  parseCalibration,
  remoteFlip,
  type Calibration,
  toRemoteFrame,
  toScreenFrame,
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

const FULL: Calibration = {
  version: CALIBRATION_VERSION,
  accelSign: 1,
  accelScale: 1,
  gyroBias: [0, 0, 0],
  gyroMap: DEFAULT_GYRO_MAP,
  gyroScale: 1,
  pointer: { yaw: POINTER_YAW_RANGE, pitch: 15 },
}

describe('calibration v2', () => {
  test('a measured map, scale and accel scale are applied', () => {
    const cal: Calibration = {
      ...FULL,
      accelSign: -1,
      accelScale: 0.5,
      gyroBias: [0.1, 0.2, 0.3],
      gyroMap: [
        { ch: 'alpha', sign: 1 },
        { ch: 'gamma', sign: -1 },
        { ch: 'beta', sign: 1 },
      ],
      gyroScale: 2,
    }
    const f = toRemoteFrame(raw({ az: -2 * G, alpha: 5.1, beta: 7.3, gamma: 6.2 }), cal)
    expect(f.accel[2]).toBeCloseTo(1)
    expect(f.gyro[0]).toBeCloseTo(10) // pitch from alpha
    expect(f.gyro[1]).toBeCloseTo(-12) // yaw from gamma, flipped
    expect(f.gyro[2]).toBeCloseTo(14) // roll from beta
  })

  test('only a guided calibration counts; stored ones are checked field by field', () => {
    expect(isFullCalibration(FULL)).toBe(true)
    expect(isFullCalibration({ accelSign: 1, gyroBias: [0, 0, 0] })).toBe(false) // the old one-tap kind
    expect(isFullCalibration(null)).toBe(false)
    expect(parseCalibration(JSON.parse(JSON.stringify(FULL)))).toEqual(FULL)
    expect(parseCalibration({ accelSign: 1, gyroBias: [0, 0, 0] })).toEqual({ accelSign: 1, gyroBias: [0, 0, 0] })
    const dup = { ...FULL, gyroMap: [DEFAULT_GYRO_MAP[0], DEFAULT_GYRO_MAP[0], DEFAULT_GYRO_MAP[2]] }
    expect(parseCalibration(dup)).toBeNull()
    expect(parseCalibration({ ...FULL, gyroScale: -1 })).toBeNull()
    expect(parseCalibration({ ...FULL, pointer: { yaw: 0, pitch: 10 } })).toBeNull()
    expect(parseCalibration('junk')).toBeNull()
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

  test("the calibrated ranges put the screen's edge where the player aimed at it", () => {
    const p = new PointerIntegrator()
    p.setRanges(12, 6)
    for (let i = 0; i < 4; i++) p.update([0, -30, 0], 100) // 12° right
    for (let i = 0; i < 2; i++) p.update([30, 0, 0], 100) // 6° up
    expect(p.value()[0]).toBeCloseTo(1)
    expect(p.value()[1]).toBeCloseTo(-1)
  })
})

describe('toScreenFrame', () => {
  const sample = raw({ ax: 1 * G, ay: 2 * G, az: 3 * G, beta: 10, alpha: 20, gamma: 30 })
  const f = (angle: number) => toScreenFrame(sample, DEFAULT_CALIBRATION, angle)
  test('0: the device axes', () => {
    expect(f(0).accel).toEqual([1, 2, 3])
    expect(f(0).gyro).toEqual([10, 20, 30])
  })
  test('90: the displayed top is the device +x, the displayed right is the device -y', () => {
    expect(f(90).accel).toEqual([-2, 1, 3])
    // gyro is (about x, about z, about y): about-x and about-y trade places, yaw stays yaw.
    expect(f(90).gyro).toEqual([-30, 20, 10])
  })
  test('180: turned about z', () => {
    expect(f(180).accel).toEqual([-1, -2, 3])
    expect(f(180).gyro).toEqual([-10, 20, -30])
  })
  test('270', () => {
    expect(f(270).accel).toEqual([2, -1, 3])
    expect(f(270).gyro).toEqual([30, 20, -10])
  })
  test('other angles snap to the nearest quarter; negative angles wrap', () => {
    expect(f(88).accel).toEqual(f(90).accel)
    expect(f(-90).accel).toEqual(f(270).accel)
  })
  test('a phone lying face up still reads +1 g on z whatever the angle', () => {
    for (const a of [0, 90, 180, 270]) expect(toScreenFrame(raw({ az: G }), DEFAULT_CALIBRATION, a).accel[2]).toBe(1)
  })
})

describe('MotionController frame', () => {
  test('a screen-frame layout rotates by the screen angle, a remote one does not', () => {
    const model = new InputModel()
    let listener: MotionListener | null = null
    const source = { start: (fn: MotionListener) => (listener = fn), stop: () => (listener = null) }
    const c = new MotionController(model, source, {
      needsPermission: false,
      supported: true,
      flip: () => false,
      angle: () => 90,
      now: () => 0,
      calibration: FULL,
    })
    c.setWanted(true, false, 'screen')
    listener!(raw({ ax: G, ay: 2 * G }))
    expect(model.frame().accel.slice(0, 2)).toEqual([-2, 1])
    c.setWanted(true, false)
    listener!(raw({ ax: G, ay: 2 * G }))
    expect(model.frame().accel.slice(0, 2)).toEqual([1, 2])
  })
})

describe('MotionController', () => {
  function setup(needsPermission = false, calibration: Calibration | null = FULL) {
    const model = new InputModel()
    let listener: MotionListener | null = null
    let t = 0
    const source = { start: (fn: MotionListener) => (listener = fn), stop: () => (listener = null) }
    const saved: Calibration[] = []
    const c = new MotionController(model, source, {
      needsPermission,
      supported: true,
      flip: () => false,
      now: () => t,
      calibration,
      saveCalibration: (cal) => saved.push(cal),
    })
    return { model, c, saved, emit: (r: RawMotion) => listener?.(r), started: () => listener !== null, tick: (ms: number) => (t += ms) }
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

  test('an uncalibrated phone sends no motion, and an old one-tap calibration does not count', () => {
    for (const cal of [null, { accelSign: -1 as const, gyroBias: [0, 0, 0] as [number, number, number] }]) {
      const s = setup(false, cal)
      s.c.setWanted(true, true)
      expect(s.started()).toBe(true)
      expect(get(s.c.view)).toMatchObject({ status: 'on', calibrated: false })
      s.emit(raw({ beta: 50, az: -G }))
      expect(s.model.frame().gyro).toEqual([0, 0, 0])
      expect(s.model.frame().accel).toEqual([0, 0, 0])
    }
  })

  test('the guided calibration runs through the controller, is saved, and only then sends motion', () => {
    const s = setup(false, null)
    s.c.setWanted(true, true)
    s.c.startCalibration()
    expect(get(s.c.view).calib?.step).toBe('rest')
    // A phone with no gyro only needs the rest step.
    for (let i = 0; i <= 3100; i += 20) {
      s.emit(raw({ hasGyro: false }))
      s.tick(20)
    }
    expect(get(s.c.view).calib).toMatchObject({ step: 'verify', noGyro: true })
    expect(s.model.frame().accel).toEqual([0, 0, 0]) // nothing sent while calibrating
    expect(s.c.finishCalibration()).toBe(true)
    expect(s.saved).toHaveLength(1)
    expect(get(s.c.view)).toMatchObject({ calibrated: true, calib: null })
    s.emit(raw({ hasGyro: false }))
    expect(s.model.frame().accel[2]).toBeCloseTo(1)
  })

  test('skipping falls back to touch; cancelling keeps the old calibration', () => {
    const s = setup(false, null)
    s.c.setWanted(true, true)
    s.c.skip()
    expect(get(s.c.view).status).toBe('skipped')
    s.c.startCalibration()
    expect(get(s.c.view).status).toBe('on')
    s.c.cancelCalibration()
    expect(get(s.c.view)).toMatchObject({ calib: null, calibrated: false })
    expect(s.c.finishCalibration()).toBe(false)
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
    expect(cleanPattern([10, -5, 'x', 99999, NaN])).toEqual([10, 0, 1000])
  })

  test('sounds only from this server or data:', () => {
    expect(soundAllowed('/sounds/a.mp3', 'https://yui.example')).toBe(true)
    expect(soundAllowed('data:audio/wav;base64,AA', 'https://yui.example')).toBe(true)
    expect(soundAllowed('https://other.example/a.mp3', 'https://yui.example')).toBe(false)
  })
})
