import type { Vec3 } from '@yui/protocol'

export type MotionPermission = 'granted' | 'denied' | 'unsupported'

/** True on browsers (iOS Safari) that need a tap before DeviceMotion fires. */
export function motionNeedsPermission(): boolean {
  if (typeof DeviceMotionEvent === 'undefined') return false
  return typeof (DeviceMotionEvent as unknown as { requestPermission?: unknown }).requestPermission === 'function'
}

export function motionSupported(): boolean {
  return typeof DeviceMotionEvent !== 'undefined'
}

/**
 * iOS gates DeviceMotion behind a permission prompt that must be triggered from a user gesture.
 * Call this from a tap handler. Other browsers need no prompt.
 */
export async function requestMotionPermission(): Promise<MotionPermission> {
  if (typeof DeviceMotionEvent === 'undefined') return 'unsupported'
  const req = (DeviceMotionEvent as unknown as { requestPermission?: () => Promise<string> }).requestPermission
  if (typeof req !== 'function') return 'granted'
  try {
    return (await req.call(DeviceMotionEvent)) === 'granted' ? 'granted' : 'denied'
  } catch {
    return 'denied'
  }
}

export function canVibrate(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function'
}

// ---------------------------------------------------------------- frame

export const STANDARD_GRAVITY = 9.80665

/** One DeviceMotion sample in the device's own axes (W3C: x right, y toward the top edge, z out of the screen). */
export interface RawMotion {
  /** accelerationIncludingGravity, m/s². */
  ax: number
  ay: number
  az: number
  /** rotationRate, deg/s: alpha about z, beta about x, gamma about y. */
  alpha: number
  beta: number
  gamma: number
  hasGyro: boolean
  /** ms between samples (DeviceMotionEvent.interval). */
  interval: number
}

/**
 * Fixes per-device differences. `accelSign` is +1 when the browser reports
 * +1 g on z for a phone lying screen-up (the W3C convention, Android
 * Chrome) and −1 when it reports the opposite (older iOS Safari).
 * `gyroBias` is the rate measured at rest, subtracted from every sample.
 */
export interface Calibration {
  accelSign: 1 | -1
  gyroBias: Vec3
}

export const DEFAULT_CALIBRATION: Calibration = { accelSign: 1, gyroBias: [0, 0, 0] }

export interface RemoteFrame {
  /** g, proper acceleration: a phone at rest screen-up reads (0, 0, +1). */
  accel: Vec3
  /** deg/s, right-handed: pitch about +x (top edge up is +), yaw about +z (turning left is +), roll about +y. */
  gyro: Vec3
}

/**
 * DeviceMotion → the contracts §3 "phone held like a remote" frame: portrait,
 * top edge pointing at the screen, screen facing up; +x right, +y toward the
 * screen, +z up out of the screen. That is the W3C device frame itself, so
 * with `flip` false only units, signs and bias change. `flip` turns the frame
 * 180° about z, for a phone whose top edge points away from where the remote's
 * top should be (upside-down portrait, or landscape with the top to the right;
 * see `remoteFlip`).
 */
export function toRemoteFrame(raw: RawMotion, cal: Calibration, flip = false): RemoteFrame {
  const s = cal.accelSign / STANDARD_GRAVITY
  let accel: Vec3 = [raw.ax * s, raw.ay * s, raw.az * s]
  let gyro: Vec3 = raw.hasGyro
    ? [raw.beta - cal.gyroBias[0], raw.alpha - cal.gyroBias[1], raw.gamma - cal.gyroBias[2]]
    : [0, 0, 0]
  if (flip) {
    accel = [-accel[0], -accel[1], accel[2]]
    gyro = [-gyro[0], gyro[1], -gyro[2]]
  }
  return { accel: accel.map(clean) as Vec3, gyro: gyro.map(clean) as Vec3 }
}

/**
 * DeviceMotion → the screen frame, for a phone held like a handheld
 * controller: +x is the displayed image's right, +y its top, +z out of the
 * screen. The remote frame's units, signs and bias, then the device axes are
 * rotated about z by the screen angle (the phone is turned `angle` degrees
 * counter-clockwise from natural, so the displayed axes are the device axes
 * rotated by `angle`). z is unchanged.
 */
export function toScreenFrame(raw: RawMotion, cal: Calibration, angle: number): RemoteFrame {
  const f = toRemoteFrame(raw, cal, false)
  const a = ((Math.round(angle / 90) * 90) % 360 + 360) % 360
  const rot = (v: Vec3): Vec3 => {
    switch (a) {
      case 90:
        return [-v[1], v[0], v[2]]
      case 180:
        return [-v[0], -v[1], v[2]]
      case 270:
        return [v[1], -v[0], v[2]]
      default:
        return v
    }
  }
  return { accel: rot(f.accel).map(clean) as Vec3, gyro: rot(f.gyro).map(clean) as Vec3 }
}

const clean = (v: number) => (Number.isFinite(v) ? v + 0 : 0) // also turns -0 into 0

/**
 * The remote's top is the screen's top in portrait and the screen's left edge
 * in landscape (a remote held sideways, buttons on the right). `angle` is
 * screen.orientation.angle: 0 portrait, 90 landscape with the phone's top to
 * the left, 180 upside down, 270 landscape with the top to the right.
 */
export function remoteFlip(angle: number): boolean {
  const a = ((Math.round(angle / 90) * 90) % 360 + 360) % 360
  return a === 180 || a === 270
}

export type CalibrationResult = { ok: true; calibration: Calibration } | { ok: false; reason: 'flat' | 'still' | 'empty' }

/**
 * One-tap calibration: the phone lies flat, screen up, and keeps still for
 * the sampling window. The sign of gravity on z gives `accelSign`; the mean
 * rotation rate is the gyro bias.
 */
export function calibrate(samples: RawMotion[]): CalibrationResult {
  if (!samples.length) return { ok: false, reason: 'empty' }
  const mean = (f: (r: RawMotion) => number) => samples.reduce((a, r) => a + f(r), 0) / samples.length
  const ax = mean((r) => r.ax)
  const ay = mean((r) => r.ay)
  const az = mean((r) => r.az)
  // Mostly on z: lying flat (within ~35°).
  if (Math.abs(az) < 0.8 * STANDARD_GRAVITY || Math.hypot(ax, ay) > 0.6 * STANDARD_GRAVITY) return { ok: false, reason: 'flat' }
  const gyro = samples.filter((r) => r.hasGyro)
  let bias: Vec3 = [0, 0, 0]
  if (gyro.length) {
    const m = (f: (r: RawMotion) => number) => gyro.reduce((a, r) => a + f(r), 0) / gyro.length
    bias = [m((r) => r.beta), m((r) => r.alpha), m((r) => r.gamma)]
    if (bias.some((b) => Math.abs(b) > 6)) return { ok: false, reason: 'still' }
  }
  return { ok: true, calibration: { accelSign: az >= 0 ? 1 : -1, gyroBias: bias } }
}

const CAL_KEY = 'yui_pad_motion_cal'

export function loadCalibration(): Calibration | null {
  try {
    const raw = localStorage.getItem(CAL_KEY)
    if (!raw) return null
    const c = JSON.parse(raw)
    if ((c.accelSign === 1 || c.accelSign === -1) && Array.isArray(c.gyroBias) && c.gyroBias.length === 3) return c
  } catch {
    /* storage unavailable or corrupt */
  }
  return null
}

export function saveCalibration(c: Calibration) {
  try {
    localStorage.setItem(CAL_KEY, JSON.stringify(c))
  } catch {
    /* storage unavailable */
  }
}

// -------------------------------------------------------------- pointer

/** Degrees of yaw / pitch from centre to the screen edge. */
export const POINTER_YAW_RANGE = 25
export const POINTER_PITCH_RANGE = 15

/**
 * Integrates gyro yaw/pitch into a pointer: −1…1, +x right, +y down, 0 at the
 * direction the phone pointed at the last recenter. The angle is clamped at the
 * edges so swinging past them does not have to be undone.
 */
export class PointerIntegrator {
  private yaw = 0
  private pitch = 0

  update(gyro: Vec3, dtMs: number): [number, number] {
    const dt = Math.max(0, Math.min(dtMs, 100)) / 1000
    this.yaw = clamp(this.yaw + gyro[1] * dt, POINTER_YAW_RANGE)
    this.pitch = clamp(this.pitch + gyro[0] * dt, POINTER_PITCH_RANGE)
    return this.value()
  }

  value(): [number, number] {
    // Turning right is negative yaw (right-handed about +z) → pointer right.
    return [clean(-this.yaw / POINTER_YAW_RANGE), clean(-this.pitch / POINTER_PITCH_RANGE)]
  }

  recenter() {
    this.yaw = 0
    this.pitch = 0
  }
}

const clamp = (v: number, r: number) => Math.max(-r, Math.min(r, v))

// --------------------------------------------------------------- sources

export type MotionListener = (raw: RawMotion) => void

/** Something that produces DeviceMotion-like samples: the real sensor, or the simulator's mouse. */
export interface MotionSource {
  start(fn: MotionListener): void
  stop(): void
}

export function rawFromEvent(e: DeviceMotionEvent): RawMotion | null {
  const g = e.accelerationIncludingGravity
  if (!g || g.x === null || g.y === null || g.z === null) return null
  const r = e.rotationRate
  const hasGyro = !!r && r.alpha !== null && r.beta !== null && r.gamma !== null
  return {
    ax: g.x ?? 0,
    ay: g.y ?? 0,
    az: g.z ?? 0,
    alpha: hasGyro ? r!.alpha! : 0,
    beta: hasGyro ? r!.beta! : 0,
    gamma: hasGyro ? r!.gamma! : 0,
    hasGyro,
    interval: e.interval || 16,
  }
}

export class DeviceMotionSource implements MotionSource {
  private handler: ((e: DeviceMotionEvent) => void) | null = null

  start(fn: MotionListener) {
    this.stop()
    this.handler = (e) => {
      const raw = rawFromEvent(e)
      if (raw) fn(raw)
    }
    window.addEventListener('devicemotion', this.handler)
  }

  stop() {
    if (this.handler) window.removeEventListener('devicemotion', this.handler)
    this.handler = null
  }
}

/**
 * The /sim stand-in: mouse drags turn into rotation (horizontal = yaw,
 * vertical = pitch) and a tilted gravity vector; `shake` adds a short swing.
 * Samples are produced at ~60 Hz while started.
 */
export class SimMotionSource implements MotionSource {
  private fn: MotionListener | null = null
  private timer: ReturnType<typeof setInterval> | null = null
  private rate: Vec3 = [0, 0, 0] // beta, alpha, gamma deg/s
  private pitch = 0
  private shakeUntil = 0

  start(fn: MotionListener) {
    this.fn = fn
    if (this.timer) return
    this.timer = setInterval(() => this.emit(), 16)
  }

  stop() {
    if (this.timer) clearInterval(this.timer)
    this.timer = null
    this.fn = null
  }

  /** Mouse moved by (dx, dy) px over `dtMs`: dragging right turns right, dragging up tilts the top up. */
  drag(dx: number, dy: number, dtMs: number) {
    const dt = Math.max(dtMs, 8) / 1000
    const degPerPx = 0.25
    this.rate = [(-dy * degPerPx) / dt, (-dx * degPerPx) / dt, 0]
    this.pitch = Math.max(-80, Math.min(80, this.pitch - dy * degPerPx))
  }

  release() {
    this.rate = [0, 0, 0]
  }

  shake() {
    this.shakeUntil = performance.now() + 160
  }

  private emit() {
    if (!this.fn) return
    const p = (this.pitch * Math.PI) / 180
    const shaking = performance.now() < this.shakeUntil
    const g = STANDARD_GRAVITY
    this.fn({
      ax: 0,
      ay: g * Math.sin(p) + (shaking ? 2.5 * g : 0),
      az: g * Math.cos(p),
      beta: this.rate[0],
      alpha: this.rate[1],
      gamma: shaking ? 400 : this.rate[2],
      hasGyro: true,
      interval: 16,
    })
    // A drag only reports rates while the mouse moves.
    this.rate = [this.rate[0] * 0.5, this.rate[1] * 0.5, 0]
  }
}
