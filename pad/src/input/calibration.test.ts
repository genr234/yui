import { describe, expect, test } from 'bun:test'
import type { Vec3 } from '@yui/protocol'
import { CalibrationWizard } from './calibration'
import { STANDARD_GRAVITY as G, toRemoteFrame, type Calibration, type RawMotion } from '../platform/motion'
import { FOCAL_PER_LONG_SIDE, ViewPointer, describeViewpoint } from '../platform/viewpoint'
import { seeCodes, standAt } from './scanScene'

/**
 * A simulated phone: a true orientation R (device → world), turned by the
 * test, read through a "browser" that may permute, flip and rescale the
 * rotation-rate channels, invert gravity, and add gyro bias.
 */
type Mat = [Vec3, Vec3, Vec3] // rows
const I: Mat = [
  [1, 0, 0],
  [0, 1, 0],
  [0, 0, 1],
]
const mul = (a: Mat, b: Mat): Mat =>
  a.map((row) => [0, 1, 2].map((j) => row[0] * b[0][j] + row[1] * b[1][j] + row[2] * b[2][j])) as Mat
const tr = (a: Mat): Mat => [0, 1, 2].map((i) => [a[0][i], a[1][i], a[2][i]]) as Mat
const apply = (a: Mat, v: Vec3): Vec3 => a.map((row) => row[0] * v[0] + row[1] * v[1] + row[2] * v[2]) as Vec3
/** Rotation by `deg` about unit axis `u` (Rodrigues). */
function rot(u: Vec3, deg: number): Mat {
  const t = (deg * Math.PI) / 180
  const c = Math.cos(t)
  const s = Math.sin(t)
  const [x, y, z] = u
  return [
    [c + x * x * (1 - c), x * y * (1 - c) - z * s, x * z * (1 - c) + y * s],
    [y * x * (1 - c) + z * s, c + y * y * (1 - c), y * z * (1 - c) - x * s],
    [z * x * (1 - c) - y * s, z * y * (1 - c) + x * s, c + z * z * (1 - c)],
  ]
}

interface Browser {
  /** Device-frame rate (deg/s about x, y, z) → { alpha, beta, gamma } as this browser reports it. */
  rates(w: Vec3): { alpha: number; beta: number; gamma: number }
  accelSign: 1 | -1
  accelGain: number
}

const W3C: Browser = { rates: (w) => ({ beta: w[0], gamma: w[1], alpha: w[2] }), accelSign: 1, accelGain: 1 }

// Everything wrong at once: channels rotated, rad/s, yaw flipped, gravity inverted and 2% off, plus drift.
const RAD = Math.PI / 180
const QUIRKY: Browser = {
  rates: (w) => ({ alpha: w[0] * RAD + 0.004, beta: w[1] * RAD - 0.002, gamma: -w[2] * RAD + 0.003 }),
  accelSign: -1,
  accelGain: 0.98,
}

class Phone {
  R: Mat = I
  t = 0
  /**
   * `tremor`: deg/s of hand shake while "holding still" (standing, no table).
   * Three incommensurate sines per axis: zero-mean, and it really turns the phone.
   */
  /** m/s² of jolts from the hand moving the phone around, on top of gravity. */
  jolts = 0
  private seed = 7

  constructor(
    private wiz: CalibrationWizard,
    private browser: Browser,
    private tremor = 0,
  ) {}

  private jolt() {
    this.seed = (this.seed * 1664525 + 1013904223) >>> 0
    return (this.seed / 2 ** 32 - 0.5) * 2 * this.jolts
  }

  private emit(w: Vec3) {
    const up = apply(tr(this.R), [0, 0, G]) // proper acceleration at rest, device frame
    if (this.jolts) for (let i = 0; i < 3; i++) up[i] += this.jolt()
    const k = this.browser.accelSign * this.browser.accelGain
    const r = this.browser.rates(w)
    const raw: RawMotion = { ax: up[0] * k, ay: up[1] * k, az: up[2] * k, ...r, hasGyro: true, interval: 20 }
    this.wiz.feed(raw, this.t)
    this.t += 20
  }

  hold(ms: number) {
    for (let i = 0; i < ms; i += 20) {
      if (!this.tremor) {
        this.emit([0, 0, 0])
        continue
      }
      const s = this.t / 1000
      const a = this.tremor
      const w: Vec3 = [
        a * Math.sin(2 * Math.PI * 7.3 * s),
        a * Math.sin(2 * Math.PI * 5.1 * s + 1),
        a * Math.sin(2 * Math.PI * 9.7 * s + 2),
      ]
      const n = Math.hypot(w[0], w[1], w[2])
      if (n > 0) this.R = mul(this.R, rot([w[0] / n, w[1] / n, w[2] / n], n * 0.02))
      this.emit(w)
    }
  }

  /** Turns `deg` about a device axis over `ms`. */
  turnBody(axis: Vec3, deg: number, ms = 1000) {
    const n = ms / 20
    const rate = deg / (ms / 1000)
    for (let i = 0; i < n; i++) {
      this.R = mul(this.R, rot(axis, deg / n))
      this.emit([axis[0] * rate, axis[1] * rate, axis[2] * rate])
    }
  }

  /** Turns `deg` about a world axis (a person turning to aim) over `ms`. */
  turnWorld(axis: Vec3, deg: number, ms = 500) {
    const n = ms / 20
    const rate = deg / (ms / 1000)
    for (let i = 0; i < n; i++) {
      this.R = mul(rot(axis, deg / n), this.R)
      const w = apply(tr(this.R), axis)
      this.emit([w[0] * rate, w[1] * rate, w[2] * rate])
    }
  }
}

const X: Vec3 = [1, 0, 0]
const Y: Vec3 = [0, 1, 0]
const Z: Vec3 = [0, 0, 1]

/** The whole guided run, done the way the screen asks. */
function runAll(browser: Browser, tremor = 0) {
  const wiz = new CalibrationWizard({ scan: false })
  const p = new Phone(wiz, browser, tremor)
  p.hold(3200) // flat in the palm (or on a table)
  expect(wiz.step).toBe('pitch')
  p.hold(500)
  p.turnBody(X, 90) // top edge up to standing
  p.hold(1000)
  expect(wiz.step).toBe('roll')
  p.turnBody(X, -90) // flat again
  p.hold(600)
  p.turnBody(Y, 90) // onto the side edge
  p.hold(1000)
  expect(wiz.view()).toMatchObject({ step: 'aim-center', phase: 'settle', problem: null })
  p.turnBody(Y, -90)
  p.turnBody(X, 10, 300) // held like a remote, a little nose-up, at the middle of the TV
  p.hold(1200)
  expect(wiz.step).toBe('aim-right')
  p.turnWorld(Z, -20) // turn right to the TV's right edge
  p.hold(1200)
  expect(wiz.step).toBe('aim-top')
  p.turnWorld(Z, 20) // back to the middle
  p.turnBody(X, 8, 300) // and up to the top edge
  p.hold(1200)
  expect(wiz.step).toBe('verify')
  return { wiz, cal: wiz.result()! }
}

/** What the calibrated frame says for a known device-frame rotation. */
function readBack(cal: Calibration, browser: Browser, w: Vec3) {
  const r = browser.rates(w)
  return toRemoteFrame({ ax: 0, ay: 0, az: G * browser.accelSign * browser.accelGain, ...r, hasGyro: true, interval: 20 }, cal)
}

describe('guided calibration', () => {
  for (const [name, browser] of [
    ['W3C browser', W3C],
    ['swapped channels, rad/s, flipped yaw, inverted gravity, drift', QUIRKY],
  ] as const) {
    test(`${name}: measures pitch, yaw and roll exactly`, () => {
      const { cal } = runAll(browser)
      expectExact(cal, browser, 0.05)
    })

    test(`${name}: standing, hand-held with tremor, still within a fraction of a degree`, () => {
      const { cal } = runAll(browser, 2.5)
      expectExact(cal, browser, 0.4)
    })
  }

  function expectExact(cal: Calibration, browser: Browser, driftLimit: number) {
    expect(cal.version).toBe(2)
    // pitch = about x, yaw = about z, roll = about y, in deg/s
    const f = readBack(cal, browser, [30, 50, -70])
    expect(f.gyro[0]).toBeCloseTo(30, 0)
    expect(f.gyro[1]).toBeCloseTo(-70, 0)
    expect(f.gyro[2]).toBeCloseTo(50, 0)
    expect(f.accel[2]).toBeCloseTo(1, 2)
    // Rest reads zero: the drift is gone.
    const still = readBack(cal, browser, [0, 0, 0])
    for (const v of still.gyro) expect(Math.abs(v)).toBeLessThan(driftLimit)
    // The TV's edges as this player aimed at them (20° right, 8° up, nose 10° up while turning).
    expect(cal.pointer!.yaw).toBeGreaterThan(19)
    expect(cal.pointer!.yaw).toBeLessThan(21)
    expect(cal.pointer!.pitch).toBeGreaterThan(7)
    expect(cal.pointer!.pitch).toBeLessThan(9)
  }

  test('rest: waits while held in the hand or not flat, says why', () => {
    const wiz = new CalibrationWizard({ scan: false })
    const p = new Phone(wiz, W3C)
    p.turnBody(X, 45, 200)
    p.hold(3000)
    expect(wiz.view()).toMatchObject({ step: 'rest', problem: 'not-flat' })
    p.turnBody(X, -45, 200)
    p.hold(1000)
    expect(wiz.view().phase).toBe('hold')
    expect(wiz.view().progress).toBeGreaterThan(0.25)
  })

  test('pitch: lifting a side edge instead is caught and the step starts over', () => {
    const wiz = new CalibrationWizard({ scan: false })
    const p = new Phone(wiz, W3C)
    p.hold(3200)
    p.hold(500)
    p.turnBody(Y, 80)
    p.hold(1000)
    expect(wiz.view()).toMatchObject({ step: 'pitch', problem: 'wrong-edge', phase: 'settle' })
    p.turnBody(Y, -80)
    p.hold(600)
    p.turnBody(X, 90)
    p.hold(1000)
    expect(wiz.step).toBe('roll')
  })

  test('pitch: stopping short says keep going, and the live angle is shown', () => {
    const wiz = new CalibrationWizard({ scan: false })
    const p = new Phone(wiz, W3C)
    p.hold(3700)
    p.turnBody(X, 40)
    p.hold(1000)
    expect(wiz.view()).toMatchObject({ step: 'pitch', problem: 'further', angle: 40, target: 60 })
    p.turnBody(X, 50)
    p.hold(1000)
    expect(wiz.step).toBe('roll')
  })

  test('aim-top: pointing down instead is refused', () => {
    const wiz = new CalibrationWizard({ scan: false })
    const p = new Phone(wiz, W3C)
    p.hold(3700)
    p.turnBody(X, 90)
    p.hold(1000)
    p.turnBody(X, -90)
    p.hold(600)
    p.turnBody(Y, 90)
    p.hold(1000)
    p.turnBody(Y, -90)
    p.turnBody(X, 10, 300)
    p.hold(1200)
    p.turnWorld(Z, -20)
    p.hold(1200)
    expect(wiz.step).toBe('aim-top')
    p.turnWorld(Z, 20)
    p.turnBody(X, -8, 300)
    p.hold(1200)
    expect(wiz.view()).toMatchObject({ step: 'aim-top', problem: 'wrong-way' })
  })
})

/**
 * The scan path. The phone's world here is x right, y towards the kiosk, z
 * up; the kiosk scene's (scanScene) is x right, y down, z into the screen,
 * seen from `at`, so the phone's "towards the kiosk" is the line to the
 * game's centre.
 */
function cameraView(p: Phone, at: Vec3) {
  const fwd = (() => {
    const h = Math.hypot(at[0], at[2])
    return [-at[0] / h, 0, -at[2] / h] as Vec3
  })()
  const up: Vec3 = [0, -1, 0]
  const right: Vec3 = [fwd[1] * up[2] - fwd[2] * up[1], fwd[2] * up[0] - fwd[0] * up[2], fwd[0] * up[1] - fwd[1] * up[0]]
  const scene = (v: Vec3): Vec3 => [0, 1, 2].map((i) => v[0] * right[i] + v[1] * fwd[i] + v[2] * up[i]) as Vec3
  // The rear camera: +x the device's x, +y its −y, looking out of its back (−z).
  const axes: [Vec3, Vec3, Vec3] = [scene(apply(p.R, X)), scene(apply(p.R, [0, -1, 0])), scene(apply(p.R, [0, 0, -1]))]
  const frame = { w: 360, h: 480 }
  const { plane, image } = seeCodes(at, axes, frame.w, frame.h, FOCAL_PER_LONG_SIDE * 480).truth
  return { plane, image, frame }
}

/** rest, pitch, roll: the phone ends on its long side, top towards the kiosk. */
function tilts(wiz: CalibrationWizard, browser: Browser, tremor = 0) {
  const p = new Phone(wiz, browser, tremor)
  p.hold(3200)
  p.hold(500)
  p.turnBody(X, 90)
  p.hold(1000)
  p.turnBody(X, -90)
  p.hold(600)
  p.turnBody(Y, 90)
  p.hold(1000)
  expect(wiz.view()).toMatchObject({ step: 'scan', index: 3, total: 6, phase: 'settle' })
  return p
}

describe('guided calibration: scanning the kiosk', () => {
  const at = standAt(1.5, -40) // well to the left of the kiosk

  for (const [name, browser] of [
    ['W3C browser', W3C],
    ['swapped channels, rad/s, flipped yaw, inverted gravity, drift', QUIRKY],
  ] as const) {
    test(`${name}: on its side to upright, scan, aim at the middle: done, and which way is left is known`, () => {
      const wiz = new CalibrationWizard()
      const p = tilts(wiz, browser)
      // From on its side straight up, like a steering wheel, then turn the screen to face you.
      p.turnBody(Z, 90)
      p.turnBody(Y, -90)
      p.hold(400)
      const { plane, image, frame } = cameraView(p, at)
      expect(wiz.scanned(plane, image, frame)).toBeNull()
      expect(wiz.view()).toMatchObject({ step: 'aim-center', standing: { side: -40, above: 0 } })
      p.turnBody(X, -80) // down into the remote hold, a little nose-up
      p.hold(1200)
      expect(wiz.step).toBe('verify')
      const cal = wiz.result()!
      expect(describeViewpoint(cal.viewpoint!).side).toBeCloseTo(-40, 3)
      // Yaw's sign came from gravity: a turn to the right reads as one.
      const f = readBack(cal, browser, [0, 0, -30])
      expect(f.gyro[1]).toBeCloseTo(-30, 0)
      // `pointer` is filled in from the viewpoint, for anything that reads the old field.
      const r = new ViewPointer(cal.viewpoint!).ranges()
      expect(cal.pointer).toEqual({ yaw: r.yaw, pitch: r.pitch })
    })
  }

  test('tipped straight up from flat (no turn that shows yaw): one turn to the right edge is added', () => {
    const wiz = new CalibrationWizard()
    const p = tilts(wiz, QUIRKY)
    p.turnBody(Y, -90) // flat
    p.turnBody(X, 90) // upright, camera at the kiosk
    p.hold(400)
    const { plane, image, frame } = cameraView(p, at)
    expect(wiz.scanned(plane, image, frame)).toBeNull()
    p.turnBody(X, -80)
    p.hold(1200)
    expect(wiz.view()).toMatchObject({ step: 'aim-right', index: 5, total: 7 })
    p.turnWorld(Z, -15)
    p.hold(1200)
    expect(wiz.step).toBe('verify')
    const cal = wiz.result()!
    expect(readBack(cal, QUIRKY, [0, 0, -30]).gyro[1]).toBeCloseTo(-30, 0)
    expect(cal.viewpoint).toBeDefined()
  })

  test('a scan that cannot be from in front of the kiosk is refused and the step goes on', () => {
    const wiz = new CalibrationWizard()
    const p = tilts(wiz, W3C)
    p.turnBody(Y, -90)
    p.turnBody(X, 90)
    p.hold(400)
    const { plane, image, frame } = cameraView(p, at)
    const mixed = [image[1], image[0], image[2], image[3]]
    expect(wiz.scanned(plane, mixed, frame)).toBe('scan-odd')
    expect(wiz.view()).toMatchObject({ step: 'scan', problem: 'scan-odd' })
    expect(wiz.scanned(plane, image, frame)).toBeNull()
  })

  test('no scan after all: aiming at the edges takes over', () => {
    const wiz = new CalibrationWizard()
    const p = tilts(wiz, QUIRKY)
    wiz.skipScan()
    expect(wiz.view()).toMatchObject({ step: 'aim-center', index: 3, total: 7, phase: 'settle' })
    p.turnBody(Y, -90)
    p.turnBody(X, 10, 300)
    p.hold(1200)
    expect(wiz.step).toBe('aim-right')
    p.turnWorld(Z, -20)
    p.hold(1200)
    p.turnWorld(Z, 20)
    p.turnBody(X, 8, 300)
    p.hold(1200)
    expect(wiz.step).toBe('verify')
    expect(wiz.result()!.viewpoint).toBeUndefined()
    expect(wiz.result()!.pointer!.yaw).toBeCloseTo(20, 0)
  })
})

describe('yaw from gravity', () => {
  test('a shaky hand and a jolting accelerometer never give the wrong sign', () => {
    for (const [browser, seed] of [
      [W3C, 1],
      [QUIRKY, 2],
      [QUIRKY, 3],
    ] as const) {
      const wiz = new CalibrationWizard()
      const p = tilts(wiz, browser, 2.5)
      ;(p as unknown as { seed: number }).seed = seed
      p.jolts = 0.15 * G // hand-held, moving: a lot of jolt
      p.turnBody(Z, 90, 1500)
      p.turnBody(Y, -90, 1500)
      p.hold(600)
      const { plane, image, frame } = cameraView(p, standAt(1.5, 30))
      expect(wiz.scanned(plane, image, frame)).toBeNull()
      p.turnBody(X, -80)
      p.jolts = 0
      p.hold(1200)
      expect(wiz.step).toBe('verify')
      expect(readBack(wiz.result()!, browser, [0, 0, -30]).gyro[1]).toBeCloseTo(-30, 0)
    }
  })

  test('held upright and only trembling: no verdict from tremor alone', () => {
    const wiz = new CalibrationWizard()
    const p = tilts(wiz, W3C)
    p.turnBody(Y, -90)
    p.turnBody(X, 90)
    const q = new Phone(wiz, W3C, 2.5)
    q.R = p.R
    q.t = p.t
    q.jolts = 0.05 * G
    q.hold(2000)
    const { plane, image, frame } = cameraView(q, standAt(1.5, 0))
    expect(wiz.scanned(plane, image, frame)).toBeNull()
    q.turnBody(X, -80)
    q.hold(1200)
    expect(wiz.step).toBe('aim-right')
  })
})
