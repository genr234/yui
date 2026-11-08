import { writable, type Readable } from 'svelte/store'
import type { InputModel } from './model'
import {
  DEFAULT_CALIBRATION,
  PointerIntegrator,
  calibrate as fitCalibration,
  toRemoteFrame,
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
}

export interface MotionOptions {
  /** Phone needs a permission tap first (iOS). */
  needsPermission: boolean
  supported: boolean
  /** remoteFlip() for the current screen angle. */
  flip(): boolean
  calibration?: Calibration | null
  saveCalibration?(c: Calibration): void
  now?(): number
}

export const CALIBRATE_MS = 600

/**
 * Feeds DeviceMotion (or the simulator) into the input model while the
 * layout has a `motion` node: remote-frame accel/gyro, plus the integrated
 * pointer when a motion node asks for it.
 */
export class MotionController {
  private store = writable<MotionView>({ status: 'off', calibrated: false, gyro: true })
  readonly view: Readable<MotionView> = { subscribe: this.store.subscribe }
  private wanted = false
  private pointer = false
  private granted: boolean
  private running = false
  private cal: Calibration
  private calibrated: boolean
  private integrator = new PointerIntegrator()
  private lastAt: number | null = null
  private sampling: RawMotion[] | null = null
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
  setWanted(wanted: boolean, pointer: boolean) {
    this.wanted = wanted
    this.pointer = pointer
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

  /** Collect CALIBRATE_MS of samples with the phone flat, then fit signs and gyro bias. */
  calibrate(wait: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms))): Promise<CalibrationResult> {
    this.sampling = []
    return wait(CALIBRATE_MS).then(() => {
      const samples = this.sampling ?? []
      this.sampling = null
      const r = fitCalibration(samples)
      if (r.ok) {
        this.cal = r.calibration
        this.calibrated = true
        this.opts.saveCalibration?.(r.calibration)
        this.integrator.recenter()
        this.publish()
      }
      return r
    })
  }

  /** One raw sample (also called directly by tests). */
  sample(raw: RawMotion) {
    if (this.sampling) this.sampling.push(raw)
    if (raw.hasGyro !== this.gyroSeen) {
      this.gyroSeen = raw.hasGyro
      this.publish()
    }
    const f = toRemoteFrame(raw, this.cal, this.opts.flip())
    const t = this.now()
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
    }
    this.publish()
  }

  private publish() {
    this.store.set({ status: this.status(), calibrated: this.calibrated, gyro: this.gyroSeen })
  }
}
