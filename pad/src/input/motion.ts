import { writable, type Readable } from 'svelte/store'
import type { InputModel } from './model'
import { CalibrationWizard, type CalibView } from './calibration'
import type { Pointer, Pt, ViewProblem } from '../platform/viewpoint'
import {
  DEFAULT_CALIBRATION,
  isFullCalibration,
  pointerFor,
  toRemoteFrame,
  toScreenFrame,
  type Calibration,
  type MotionSource,
  type RawMotion,
} from '../platform/motion'
import type { MotionStatus } from '../controls/context'

export interface MotionView {
  status: MotionStatus
  /** A full (guided) calibration is in place. Motion is only sent once it is. */
  calibrated: boolean
  /** False once samples show the phone has no gyro (motion-remote then aims by touch). */
  gyro: boolean
  /** Set while the guided calibration runs. */
  calib: CalibView | null
  /** Verify step: where the pointer would be (−1…1, +y down), to check on the phone. */
  preview: [number, number] | null
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
  /** The phone has a camera to scan the kiosk with; without one, calibration aims at the game's edges. */
  camera?: boolean
  now?(): number
}

/** The verify preview re-publishes at most this often. */
const PREVIEW_MS = 50

/**
 * Feeds DeviceMotion (or the simulator) into the input model while the
 * layout has a `motion` node: remote-frame accel/gyro, plus the integrated
 * pointer when a motion node asks for it.
 *
 * Nothing is sent until the phone has a full calibration
 * (input/calibration.ts): an uncalibrated phone sends no motion at all
 * rather than wrong motion. A calibration from the old one-tap flow does not
 * count, so those phones calibrate again.
 */
export class MotionController {
  private store = writable<MotionView>({ status: 'off', calibrated: false, gyro: true, calib: null, preview: null })
  readonly view: Readable<MotionView> = { subscribe: this.store.subscribe }
  private wanted = false
  private pointer = false
  private frame: 'remote' | 'screen' = 'remote'
  private granted: boolean
  private denied = false
  private skipped = false
  private running = false
  private cal: Calibration
  private calibrated: boolean
  private integrator: Pointer
  private lastAt: number | null = null
  private wizard: CalibrationWizard | null = null
  private wizardKey = ''
  private preview: [number, number] | null = null
  private previewAt = -Infinity
  private previewIntegrator: Pointer | null = null
  private gyroSeen = true
  private now: () => number

  constructor(
    private model: InputModel,
    private source: MotionSource,
    private opts: MotionOptions,
  ) {
    this.granted = !opts.needsPermission
    const cal = opts.calibration ?? null
    this.calibrated = isFullCalibration(cal)
    this.cal = this.calibrated ? cal! : DEFAULT_CALIBRATION
    this.integrator = pointerFor(this.cal)
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

  /** "Play without motion" on the calibration screen: the layout falls back to touch. */
  skip() {
    this.skipped = true
    this.cancelCalibration()
    this.sync()
  }

  recenter() {
    this.integrator.recenter()
    this.previewIntegrator?.recenter()
  }

  /** Starts (or restarts) the guided calibration. Motion stops reaching the game until it is finished or cancelled. */
  startCalibration() {
    this.skipped = false
    this.wizard = new CalibrationWizard({ scan: !!this.opts.camera })
    this.wizardKey = ''
    this.preview = null
    this.previewIntegrator = null
    this.model.clearMotion()
    this.sync()
  }

  /** Calibration scans the kiosk with the camera (else it aims at the game's edges). */
  get hasCamera(): boolean {
    return !!this.opts.camera
  }

  /** The scan step found the kiosk's markers (TL, TR, BR, BL, frame pixels). Returns why it can't be used, or null. */
  scanned(corners: readonly Pt[], frame: { w: number; h: number }): ViewProblem | null {
    const problem = this.wizard?.scanned(corners, frame, this.opts.angle?.() ?? 0) ?? null
    this.publish()
    return problem
  }

  /** No camera, or no luck with it: aim at the game's edges instead. */
  skipScan() {
    this.wizard?.skipScan()
    this.publish()
  }

  cancelCalibration() {
    if (!this.wizard) return
    this.wizard = null
    this.preview = null
    this.publish()
  }

  /** Verify step accepted: keep the new calibration and aim from where the phone points now. */
  finishCalibration(): boolean {
    const c = this.wizard?.result()
    if (!c) return false
    this.wizard = null
    this.preview = null
    this.cal = c
    this.calibrated = true
    this.opts.saveCalibration?.(c)
    this.integrator = pointerFor(c)
    this.integrator.recenter()
    this.publish()
    return true
  }

  /** One raw sample (also called directly by tests). */
  sample(raw: RawMotion) {
    if (raw.hasGyro !== this.gyroSeen) {
      this.gyroSeen = raw.hasGyro
      this.publish()
    }
    const t = this.now()
    const dt = this.lastAt === null ? raw.interval : t - this.lastAt
    this.lastAt = t

    if (this.wizard) {
      this.wizard.feed(raw, t)
      const result = this.wizard.result()
      if (result) this.updatePreview(raw, result, dt, t)
      const key = JSON.stringify(this.wizard.view())
      if (key !== this.wizardKey) {
        this.wizardKey = key
        this.publish()
      }
      return
    }
    if (!this.calibrated) return

    const f = this.toFrame(raw, this.cal)
    const p = this.pointer && raw.hasGyro ? this.integrator.update(f.gyro, dt) : null
    this.model.motion(f.accel, f.gyro, p)
  }

  private toFrame(raw: RawMotion, cal: Calibration) {
    return this.frame === 'screen' ? toScreenFrame(raw, cal, this.opts.angle?.() ?? 0) : toRemoteFrame(raw, cal, this.opts.flip())
  }

  private updatePreview(raw: RawMotion, cal: Calibration, dt: number, t: number) {
    if (!raw.hasGyro) return
    this.previewIntegrator ??= pointerFor(cal)
    // The preview is the remote pointer whatever the layout's frame: it checks the aim.
    const p = this.previewIntegrator.update(toRemoteFrame(raw, cal, this.opts.flip()).gyro, dt)
    if (t - this.previewAt < PREVIEW_MS) return
    this.previewAt = t
    this.preview = p
    this.publish()
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
    if (this.skipped) return 'skipped'
    return 'on'
  }

  private sync() {
    // The sensor also runs while calibrating, whatever the layout wants.
    const on = this.status() === 'on' || (!!this.wizard && this.granted && !this.denied && this.opts.supported)
    if (on && !this.running) {
      this.running = true
      this.lastAt = null
      this.source.start((raw) => this.sample(raw))
    } else if (!on && this.running) {
      this.running = false
      this.source.stop()
      this.model.clearMotion()
      this.wizard = null
      this.preview = null
    }
    this.publish()
  }

  private publish() {
    this.store.set({
      status: this.status(),
      calibrated: this.calibrated,
      gyro: this.gyroSeen,
      calib: this.wizard?.view() ?? null,
      preview: this.preview,
    })
  }
}
