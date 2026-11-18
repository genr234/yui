import { writable, type Readable } from 'svelte/store'
import type { InputModel } from './model'
import {
  DEFAULT_CALIBRATION,
  STANDARD_GRAVITY,
  PointerIntegrator,
  calibrate as fitCalibration,
  toRemoteFrame,
  toScreenFrame,
  type Calibration,
  type CalibrationResult,
  type MotionSource,
  type RawMotion,
} from '../platform/motion'
import type { MotionStatus } from '../controls/context'

export interface MotionView {
  status: MotionStatus
  calibrated: boolean
  /** False once samples show the phone has no gyro (motion-remote then aims by touch). */
  gyro: boolean
  /** Set while a guided calibration runs: what the player should do, and how far along it is (0–1). */
  calib: CalibProgress | null
}

export interface CalibProgress {
  /** 'flat': lay the phone down; 'still': stop moving it; 'hold': stay put, sampling. */
  phase: 'flat' | 'still' | 'hold'
  progress: number
}

export interface MotionOptions {
  /** Phone needs a permission tap first (iOS). */
  needsPermission: boolean
  supported: boolean
  /** remoteFlip() for the current screen angle. */
  flip(): boolean
  /** screen.orientation.angle, for the `screen` frame (default 0). */
  angle?(): number
  calibration?: Calibration | null
  saveCalibration?(c: Calibration): void
  now?(): number
}

export const CALIBRATE_MS = 600
/** A guided calibration gives up after this long without a flat, still window. */
export const CALIBRATE_TIMEOUT_MS = 20000
/** Unprompted calibration needs a longer, steadier window: a phone resting on a table, not in a hand. */
const AUTO_MS = 1500
const AUTO_MAX_SPREAD = 0.8 // deg/s, per gyro axis; hand tremor is well above this
const STILL_DPS = 20 // a single sample spinning faster than this restarts the window

/** Rolling window of flat, still samples. */
class StillWindow {
  samples: RawMotion[] = []
  private start: number | null = null
  reset() {
    this.samples = []
    this.start = null
  }
  /** Adds one sample; returns why it broke the window, or null. */
  push(raw: RawMotion, t: number): 'flat' | 'still' | null {
    const g = STANDARD_GRAVITY
    if (Math.abs(raw.az) < 0.8 * g || Math.hypot(raw.ax, raw.ay) > 0.6 * g) return this.reset(), 'flat'
    if (raw.hasGyro && Math.hypot(raw.alpha, raw.beta, raw.gamma) > STILL_DPS) return this.reset(), 'still'
    if (this.start === null) this.start = t
    this.samples.push(raw)
    return null
  }
  elapsed(t: number) {
    return this.start === null ? 0 : t - this.start
  }
}

/**
 * Feeds DeviceMotion (or the simulator) into the input model while the
 * layout has a `motion` node: remote-frame accel/gyro, plus the integrated
 * pointer when a motion node asks for it.
 */
export class MotionController {
  private store = writable<MotionView>({ status: 'off', calibrated: false, gyro: true, calib: null })
  readonly view: Readable<MotionView> = { subscribe: this.store.subscribe }
  private wanted = false
  private pointer = false
  private frame: 'remote' | 'screen' = 'remote'
  private granted: boolean
  private running = false
  private cal: Calibration
  private calibrated: boolean
  private integrator = new PointerIntegrator()
  private lastAt: number | null = null
  private window = new StillWindow()
  private guided: {
    progress: CalibProgress
    resolve(r: CalibrationResult): void
    timer: ReturnType<typeof setTimeout>
    lastFail: 'flat' | 'still' | 'empty'
  } | null = null
  private gyroSeen = true
  private now: () => number

  constructor(
    private model: InputModel,
    private source: MotionSource,
    private opts: MotionOptions,
  ) {
    this.granted = !opts.needsPermission
    this.cal = opts.calibration ?? DEFAULT_CALIBRATION
    this.calibrated = !!opts.calibration
    this.now = opts.now ?? (() => performance.now())
  }

  /** The current layout has (or no longer has) a motion node. */
  setWanted(wanted: boolean, pointer: boolean, frame: 'remote' | 'screen' = 'remote') {
    this.wanted = wanted
    this.pointer = pointer
    this.frame = frame
    if (!pointer) this.integrator.recenter()
    this.sync()
  }

  /** Result of the permission tap. */
  setPermission(granted: boolean) {
    this.granted = granted
    if (!granted) this.denied = true
    this.sync()
  }

  private denied = false

  recenter() {
    this.integrator.recenter()
  }

  /**
   * Guided calibration: waits (up to `timeoutMs`) for the phone to lie flat
   * and still for CALIBRATE_MS, publishing progress as it goes, then fits
   * signs and gyro bias. Moving or lifting the phone restarts the window
   * instead of failing, so one tap is enough.
   */
  calibrate(timeoutMs = CALIBRATE_TIMEOUT_MS): Promise<CalibrationResult> {
    this.cancelCalibration()
    this.window.reset()
    return new Promise((resolve) => {
      const timer = setTimeout(() => this.finish({ ok: false, reason: this.guided?.lastFail ?? 'empty' }), timeoutMs)
      this.guided = { progress: { phase: 'flat', progress: 0 }, resolve, timer, lastFail: 'empty' }
      this.publish()
    })
  }

  cancelCalibration() {
    if (this.guided) this.finish({ ok: false, reason: this.guided.lastFail })
  }

  private finish(r: CalibrationResult) {
    const g = this.guided
    if (!g) return
    clearTimeout(g.timer)
    this.guided = null
    this.window.reset()
    if (r.ok) this.apply(r.calibration)
    this.publish()
    g.resolve(r)
  }

  /** `recenter` after a guided run; an unprompted one leaves the pointer where it is. */
  private apply(c: Calibration, recenter = true) {
    this.cal = c
    this.calibrated = true
    this.opts.saveCalibration?.(c)
    if (recenter) this.integrator.recenter()
    this.publish()
  }

  /** Feeds the still window; finishes a guided run, or calibrates unprompted if never calibrated. */
  private watchStill(raw: RawMotion, t: number) {
    const g = this.guided
    if (!g && this.calibrated) return
    const broke = this.window.push(raw, t)
    if (g) {
      let next: CalibProgress
      if (broke) {
        g.lastFail = broke
        next = { phase: broke, progress: 0 }
      } else {
        const done = this.window.elapsed(t) >= CALIBRATE_MS
        if (done) {
          const r = fitCalibration(this.window.samples)
          if (r.ok) return this.finish(r)
          g.lastFail = r.reason
          this.window.reset()
          next = { phase: r.reason === 'flat' ? 'flat' : 'still', progress: 0 }
        } else next = { phase: 'hold', progress: Math.min(1, this.window.elapsed(t) / CALIBRATE_MS) }
      }
      // Progress only re-publishes in visible steps.
      if (next.phase !== g.progress.phase || Math.abs(next.progress - g.progress.progress) >= 0.05) {
        g.progress = next
        this.publish()
      }
      return
    }
    if (broke || this.window.elapsed(t) < AUTO_MS) return
    const samples = this.window.samples
    this.window.reset()
    const gyro = samples.filter((r) => r.hasGyro)
    const spread = (f: (r: RawMotion) => number) => {
      const m = gyro.reduce((a, r) => a + f(r), 0) / gyro.length
      return Math.sqrt(gyro.reduce((a, r) => a + (f(r) - m) ** 2, 0) / gyro.length)
    }
    if (gyro.length && [(r: RawMotion) => r.alpha, (r: RawMotion) => r.beta, (r: RawMotion) => r.gamma].some((f) => spread(f) > AUTO_MAX_SPREAD)) return
    const r = fitCalibration(samples)
    if (r.ok) this.apply(r.calibration, false)
  }

  /** One raw sample (also called directly by tests). */
  sample(raw: RawMotion) {
    if (raw.hasGyro !== this.gyroSeen) {
      this.gyroSeen = raw.hasGyro
      this.publish()
    }
    const f =
      this.frame === 'screen'
        ? toScreenFrame(raw, this.cal, this.opts.angle?.() ?? 0)
        : toRemoteFrame(raw, this.cal, this.opts.flip())
    const t = this.now()
    this.watchStill(raw, t)
    const dt = this.lastAt === null ? raw.interval : t - this.lastAt
    this.lastAt = t
    const p = this.pointer && raw.hasGyro ? this.integrator.update(f.gyro, dt) : null
    this.model.motion(f.accel, f.gyro, p)
  }

  stop() {
    this.wanted = false
    this.sync()
  }

  private status(): MotionStatus {
    if (!this.wanted) return 'off'
    if (!this.opts.supported) return 'unsupported'
    if (this.denied) return 'denied'
    if (!this.granted) return 'needs-permission'
    return 'on'
  }

  private sync() {
    const on = this.status() === 'on'
    if (on && !this.running) {
      this.running = true
      this.lastAt = null
      this.source.start((raw) => this.sample(raw))
    } else if (!on && this.running) {
      this.running = false
      this.source.stop()
      this.model.clearMotion()
      this.cancelCalibration()
      this.window.reset()
    }
    this.publish()
  }

  private publish() {
    this.store.set({ status: this.status(), calibrated: this.calibrated, gyro: this.gyroSeen, calib: this.guided?.progress ?? null })
  }
}
