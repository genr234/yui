import type { Vec3 } from '@yui/protocol'
import { ViewPointer, describeViewpoint, viewpointFromScan, type Pt, type ViewProblem, type Viewpoint } from '../platform/viewpoint'
import {
  CALIBRATION_VERSION,
  DEFAULT_GYRO_MAP,
  POINTER_PITCH_RANGE,
  POINTER_YAW_RANGE,
  STANDARD_GRAVITY as G,
  type AxisMap,
  type Calibration,
  type GyroChannel,
  type RawMotion,
} from '../platform/motion'

/**
 * The guided motion calibration. Nothing about the browser's sensors is
 * assumed: each step measures one thing against gravity or against the TV.
 *
 *  1. rest       held flat in the palm, screen up, as still as the hand allows (or on a
 *                 table): accel sign and scale, gyro bias. Tremor is zero-mean, so a
 *                 3 s average cancels it; only a steady drift survives into the bias.
 *  2. pitch      tilt the top edge up to standing: which channel is pitch, its sign, deg/s scale
 *  3. roll       flat again, roll onto the right or left edge: roll channel, sign, scale (must agree)
 *                yaw is the channel left over
 *  4. scan       hold the phone up and point its camera at the kiosk from where you'll
 *                 play: the kiosk shows a QR code on its game, and how it looks from
 *                 here says where the player stands (platform/viewpoint.ts)
 *  5. aim-center hold it like a remote at the middle of the TV: the pointer's zero
 *  6. verify     the dot follows the phone; the player accepts or starts over
 *
 * Without a camera (or when the player can't get a scan) the scan is swapped
 * for aiming at the edges: aim-center, then
 *     aim-right  turn to the TV's right edge: yaw's sign, and the yaw range for this distance
 *     aim-top    point at the middle of the top edge: the pitch range
 *
 * Yaw's sign can't be seen from flat, where the tilt steps start, and
 * browsers do flip it. On the scan path it is read off the way gravity swings
 * while the phone turns about its screen's axis on its way into and out of
 * the scan (on its side to upright, upright to flat: see `YawWitness`). If
 * the player's hands didn't turn it that way enough to tell, aim-right is
 * added after aim-center, for its sign only.
 *
 * A tilt step compares the turn the gyro integrated with the turn gravity
 * shows between two still poses, so a browser that reports rad/s, swaps
 * alpha/beta/gamma or flips a sign is caught and corrected. Pure: feed it
 * samples and times, read `view()` and `result()`.
 */

export type CalibStep = 'rest' | 'pitch' | 'roll' | 'scan' | 'aim-center' | 'aim-right' | 'aim-top' | 'verify'
/** With the camera. */
export const SCAN_STEPS: readonly CalibStep[] = ['rest', 'pitch', 'roll', 'scan', 'aim-center', 'verify']
/** With the camera, when yaw's sign is still unknown at aim-center. */
const SCAN_STEPS_TURN: readonly CalibStep[] = ['rest', 'pitch', 'roll', 'scan', 'aim-center', 'aim-right', 'verify']
/** Without it: aim at the edges. */
export const EDGE_STEPS: readonly CalibStep[] = ['rest', 'pitch', 'roll', 'aim-center', 'aim-right', 'aim-top', 'verify']

/** settle: get into the step's start pose; move: do the motion; hold: keep still while it samples. */
export type CalibPhase = 'settle' | 'move' | 'hold'

export type CalibProblem =
  | 'not-flat' // rest/settle: not lying screen-up
  | 'moving' // rest: something moved it
  | 'further' // tilt: stopped short of the angle
  | 'wrong-edge' // tilt: turned about the wrong edge
  | 'wobble' // tilt: twisted while tilting, or the two tilts disagreed
  | 'not-remote' // aim: not held screen-up like a remote
  | 'wrong-way' // aim: went the opposite way
  | 'too-big' // aim: an edge further away than any TV is
  | ViewProblem // scan: the code was read, but not as a phone in front of the kiosk would see it

export interface CalibView {
  step: CalibStep
  /** 0-based position in this run's steps (SCAN_STEPS or EDGE_STEPS). */
  index: number
  total: number
  phase: CalibPhase
  /** 0–1: how much of the still window is done. */
  progress: number
  problem: CalibProblem | null
  /** Tilt steps: degrees turned from the start pose, and the target. */
  angle?: number
  target?: number
  /** True when the phone has no gyro: only the rest step runs. */
  noGyro?: boolean
  /** After a scan: degrees to the right of the game's straight-ahead (− left) and above its middle. */
  standing?: { side: number; above: number }
}

export const REST_MS = 3000
export const POSE_MS = 800
export const AIM_MS = 1000
/** Tilt steps must turn at least this far, so a few degrees of error are a few percent. */
export const TILT_TARGET_DEG = 60
const REST_FLAT_Z = 0.93 // ~22° from level: a palm, not a spirit level
const SETTLE_FLAT_Z = 0.9
const REST_DRIFT_DEG = 5 // a hand wanders a few degrees over 3 s
const POSE_DRIFT_DEG = 4
const REST_GYRO_STD = 5 // raw units; hand tremor is 1–3 deg/s, a moving hand far more
const REST_ACCEL_STD = 0.06 // of g
const RAW_MOVING = 15 // raw gyro magnitude that clearly is a hand, before units are known
const AXIS_MIN = 0.8 // |cos| between the measured turn axis and the expected one
const DOMINANCE = 2 // the turning channel must integrate at least this much more than the next
const SCALE_MIN = 0.2
const SCALE_MAX = 100
const SCALE_AGREE = 0.25 // pitch and roll scales within 25%
const AIM_STILL_DPS = 8 // deg/s; a hand pointing at a TV trembles at 1–5
const AIM_REMOTE_Z = 0.5
const YAW_MIN = 4
const YAW_MAX = 70
const PITCH_MIN = 2.5
const PITCH_MAX = 50

const CHANNELS: GyroChannel[] = ['alpha', 'beta', 'gamma']
type PerChannel = Record<GyroChannel, number>
const zeros = (): PerChannel => ({ alpha: 0, beta: 0, gamma: 0 })

const DEG = 180 / Math.PI
const norm = (v: Vec3): Vec3 => {
  const n = Math.hypot(v[0], v[1], v[2]) || 1
  return [v[0] / n, v[1] / n, v[2] / n]
}
const dot = (a: Vec3, b: Vec3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
const cross = (a: Vec3, b: Vec3): Vec3 => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]]
const add = (a: Vec3, b: Vec3): Vec3 => [a[0] + b[0], a[1] + b[1], a[2] + b[2]]
const sub = (a: Vec3, b: Vec3): Vec3 => [a[0] - b[0], a[1] - b[1], a[2] - b[2]]
const scale3 = (a: Vec3, k: number): Vec3 => [a[0] * k, a[1] * k, a[2] * k]
const angleDeg = (a: Vec3, b: Vec3) => Math.acos(Math.max(-1, Math.min(1, dot(norm(a), norm(b))))) * DEG
const mean = (xs: number[]) => xs.reduce((a, x) => a + x, 0) / (xs.length || 1)
const std = (xs: number[]) => {
  const m = mean(xs)
  return Math.sqrt(mean(xs.map((x) => (x - m) ** 2)))
}

/** Samples that stayed within `drift` degrees of the first one's gravity direction. */
class StillWindow {
  start: number | null = null
  g0: Vec3 | null = null
  samples: RawMotion[] = []
  reset() {
    this.start = null
    this.g0 = null
    this.samples = []
  }
  /** Adds a sample (or restarts on it when gravity moved); returns the time held. */
  push(raw: RawMotion, g: Vec3, t: number, drift: number): number {
    if (this.g0 && angleDeg(this.g0, g) > drift) this.reset()
    if (this.start === null) {
      this.start = t
      this.g0 = g
    }
    this.samples.push(raw)
    return t - this.start
  }
}

/**
 * Reads yaw's sign from gravity. A turn about the screen's axis (the phone's
 * z, which carries yaw) swings gravity around in the phone's frame unless the
 * phone lies flat: on its way from on its side to upright, or tipped while
 * upright, gravity sweeps across the screen. Over ~100 ms windows, the swing
 * the known pitch and roll turns don't explain is set against the swing the
 * yaw channel predicts; their correlation is the sign. Hand jolts in the
 * accelerometer are noise that averages out; a verdict needs enough turning
 * (`MIN_EVIDENCE`) and a clear correlation.
 */
export class YawWitness {
  static readonly WINDOW_MS = 100
  /** Σ (turn × how much it moves gravity)², rad². About 15° of turn about z, made upright. */
  static readonly MIN_EVIDENCE = 0.02
  /** Smoothing of the accelerometer, per sample: takes the edge off hand jolts. */
  static readonly SMOOTH = 0.4
  private num = 0
  private den = 0
  private g: Vec3 | null = null
  private start: { g: Vec3; t: number } | null = null
  private known: Vec3 = [0, 0, 0]
  private yaw = 0

  /**
   * `g`: up, unit, phone frame. `w`: the known turn rates (about x, about y,
   * deg/s); `yaw`: the yaw channel's rate, deg/s, sign unknown. `t` ms, `dt` s.
   */
  add(raw: Vec3, w: Vec3, yaw: number, t: number, dt: number) {
    const a = YawWitness.SMOOTH
    const g = (this.g = this.g ? norm(add(scale3(this.g, 1 - a), scale3(raw, a))) : raw)
    if (!this.start) {
      this.start = { g, t }
      return
    }
    const k = dt / DEG
    this.known = add(this.known, [w[0] * k, w[1] * k, 0])
    this.yaw += yaw * k
    if (t - this.start.t < YawWitness.WINDOW_MS) return
    // A frame turning by θ sees a fixed vector v move by v × θ.
    const g0 = this.start.g
    const mid = norm(add(g0, g))
    const seen = sub(g, g0)
    const fromKnown = cross(mid, this.known)
    const e = sub(seen, fromKnown)
    const b = cross(mid, [0, 0, this.yaw])
    this.num += dot(e, b)
    this.den += dot(b, b)
    this.start = { g, t }
    this.known = [0, 0, 0]
    this.yaw = 0
  }

  /** +1 / −1 once the evidence is in, else null. */
  sign(): 1 | -1 | null {
    if (this.den < YawWitness.MIN_EVIDENCE) return null
    const s = this.num / this.den
    if (Math.abs(s) < 0.5) return null
    return s > 0 ? 1 : -1
  }
}

interface TiltFit {
  ch: GyroChannel
  sign: 1 | -1
  scale: number
}

export class CalibrationWizard {
  private steps: readonly CalibStep[]
  private index = 0
  private phase: CalibPhase = 'settle'
  private progress = 0
  private problem: CalibProblem | null = null
  private angle: number | undefined
  private win = new StillWindow()
  private lastT: number | null = null

  // rest
  private accelSign: 1 | -1 = 1
  private accelScale = 1
  private bias: PerChannel = zeros()
  private noGyro = false

  // tilts
  private gStart: Vec3 | null = null
  private integ: PerChannel = zeros()
  private pitchFit: TiltFit | null = null
  private rollFit: TiltFit | null = null

  // aim
  private map: [AxisMap, AxisMap, AxisMap] | null = null
  private scale = 1
  private yaw = 0
  private pitch = 0
  private yawRange = 0
  private pitchRange = 0

  // scan
  private viewpoint: Viewpoint | null = null
  private witness: YawWitness | null = null
  private yawKnown = false

  /** `scan`: the phone has a camera to scan the kiosk with (default); false aims at the edges instead. */
  constructor(opts: { scan?: boolean } = {}) {
    this.steps = opts.scan === false ? EDGE_STEPS : SCAN_STEPS
  }

  private done: Calibration | null = null

  get step(): CalibStep {
    return this.steps[this.index]
  }

  view(): CalibView {
    const v: CalibView = {
      step: this.step,
      index: this.index,
      total: this.steps.length,
      phase: this.phase,
      progress: this.progress,
      problem: this.problem,
    }
    if ((this.step === 'pitch' || this.step === 'roll') && this.angle !== undefined) {
      v.angle = Math.round(this.angle)
      v.target = TILT_TARGET_DEG
    }
    if (this.noGyro) v.noGyro = true
    if (this.viewpoint) {
      const d = describeViewpoint(this.viewpoint)
      v.standing = { side: Math.round(d.side) + 0, above: Math.round(d.above) + 0 }
    }
    return v
  }

  /** The finished calibration, once the verify step is reached. */
  result(): Calibration | null {
    return this.done
  }

  /** One raw sample at time `t` (ms). */
  feed(raw: RawMotion, t: number) {
    const dt = this.lastT === null ? 0 : Math.max(0, Math.min(t - this.lastT, 100)) / 1000
    this.lastT = t
    for (const ch of CHANNELS) this.integ[ch] += (raw[ch] - this.bias[ch]) * dt
    if (this.witness && raw.hasGyro) this.watchYaw(raw, t, dt)
    switch (this.step) {
      case 'rest':
        return this.rest(raw, t)
      case 'pitch':
        return this.tilt(raw, t, 0)
      case 'roll':
        return this.tilt(raw, t, 1)
      case 'scan':
        return
      case 'aim-center':
      case 'aim-right':
      case 'aim-top':
        return this.aim(raw, t, dt)
      case 'verify':
        return
    }
  }

  /**
   * The scan step's camera read the kiosk's code: points of the game
   * (`plane`, from the code's text) and where each was seen (`image`, in a
   * frame of `frame` pixels). Returns the problem when that can't be a
   * phone in front of the kiosk.
   */
  scanned(plane: readonly Pt[], image: readonly Pt[], frame: { w: number; h: number }): ViewProblem | null {
    if (this.step !== 'scan') return null
    const vp = viewpointFromScan(plane, image, frame)
    if (typeof vp === 'string') {
      this.problem = vp
      return vp
    }
    this.viewpoint = vp
    this.advance()
    return null
  }

  /** No camera, or the player gave up on the scan: aim at the edges instead. */
  skipScan() {
    if (this.step !== 'scan') return
    this.steps = EDGE_STEPS
    this.viewpoint = null
    this.witness = null // aim-right measures the sign on this path
    this.index = this.steps.indexOf('aim-center')
    this.enter()
  }

  /** Accumulates yaw's sign from gravity until it is known (scan path). */
  private watchYaw(raw: RawMotion, t: number, dt: number) {
    const map = this.map!
    const rate = (i: number) => map[i].sign * this.scale * (raw[map[i].ch] - this.bias[map[i].ch])
    this.witness!.add(this.gravity(raw), [rate(0), rate(2), 0], this.scale * (raw[map[1].ch] - this.bias[map[1].ch]), t, dt)
    const sign = this.witness!.sign()
    if (sign === null) return
    // The witness measured how the yaw channel turns the phone about +z (left is +), so its sign is the map's.
    map[1] = { ch: map[1].ch, sign }
    this.yawKnown = true
    this.witness = null
  }

  // ------------------------------------------------------------------ steps

  private gravity(raw: RawMotion): Vec3 {
    const s = this.accelSign
    return norm([raw.ax * s, raw.ay * s, raw.az * s])
  }

  private rest(raw: RawMotion, t: number) {
    const g = norm([raw.ax, raw.ay, raw.az])
    if (Math.abs(g[2]) < REST_FLAT_Z) return this.fail('not-flat', 'settle')
    if (raw.hasGyro && Math.hypot(raw.alpha, raw.beta, raw.gamma) > RAW_MOVING) return this.fail('moving', 'settle')
    const held = this.win.push(raw, g, t, REST_DRIFT_DEG)
    this.setHold(held / REST_MS)
    if (held < REST_MS) return
    const s = this.win.samples
    this.win.reset()
    const mags = s.map((r) => Math.hypot(r.ax, r.ay, r.az))
    const gyro = s.filter((r) => r.hasGyro)
    if (std(mags) > REST_ACCEL_STD * mean(mags) || (gyro.length && CHANNELS.some((ch) => std(gyro.map((r) => r[ch])) > REST_GYRO_STD))) {
      return this.fail('moving', 'settle')
    }
    this.accelSign = mean(s.map((r) => r.az)) >= 0 ? 1 : -1
    this.accelScale = G / mean(mags)
    if (gyro.length < s.length / 2) {
      // No gyro: accel is all there is to calibrate.
      this.noGyro = true
      this.finish(DEFAULT_GYRO_MAP, 1, [0, 0, 0], { yaw: POINTER_YAW_RANGE, pitch: POINTER_PITCH_RANGE })
      return
    }
    for (const ch of CHANNELS) this.bias[ch] = mean(gyro.map((r) => r[ch]))
    this.advance()
  }

  /** `axis`: 0 = pitch (about x, top edge up), 1 = roll (about y, a side edge down). */
  private tilt(raw: RawMotion, t: number, axis: 0 | 1) {
    const g = this.gravity(raw)
    if (this.phase === 'settle') {
      // Start from flat, screen up, still: rotations about a horizontal axis
      // are the ones gravity can measure.
      if (g[2] < SETTLE_FLAT_Z) {
        this.win.reset()
        this.progress = 0
        return
      }
      const held = this.win.push(raw, g, t, POSE_DRIFT_DEG)
      if (held < POSE_MS / 2) return
      this.gStart = norm(mean3(this.win.samples.map((r) => this.gravity(r))))
      this.win.reset()
      this.integ = zeros()
      this.phase = 'move'
      this.angle = 0
      return
    }
    const gStart = this.gStart!
    this.angle = angleDeg(gStart, g)
    const held = this.win.push(raw, g, t, POSE_DRIFT_DEG)
    if (this.angle < TILT_TARGET_DEG) {
      // Paused short of the target: say so, but keep the turn so far.
      if (held >= POSE_MS && this.angle > 15) this.problem = 'further'
      this.phase = 'move'
      this.progress = 0
      return
    }
    this.problem = null
    this.setHold(held / POSE_MS)
    if (held < POSE_MS) return

    const gEnd = norm(mean3(this.win.samples.map((r) => this.gravity(r))))
    this.win.reset()
    const theta = angleDeg(gStart, gEnd)
    // Gravity turns the opposite way to the phone: the phone's axis is gEnd × gStart.
    const turnAxis = norm(cross(gEnd, gStart))
    if (Math.abs(turnAxis[axis]) < AXIS_MIN) return this.retry('wrong-edge')
    const ranked = [...CHANNELS].sort((a, b) => Math.abs(this.integ[b]) - Math.abs(this.integ[a]))
    const [best, next] = ranked
    const turned = this.integ[best]
    if (!turned || Math.abs(turned) < DOMINANCE * Math.abs(this.integ[next])) return this.retry('wobble')
    const signedTheta = theta * Math.sign(turnAxis[axis])
    const fit: TiltFit = {
      ch: best,
      sign: Math.sign(turned) === Math.sign(signedTheta) ? 1 : -1,
      scale: Math.abs(signedTheta / turned),
    }
    if (!(fit.scale >= SCALE_MIN && fit.scale <= SCALE_MAX)) return this.retry('wobble')
    if (axis === 0) {
      this.pitchFit = fit
      return this.advance()
    }
    const p = this.pitchFit!
    if (fit.ch === p.ch || Math.abs(fit.scale - p.scale) > SCALE_AGREE * p.scale) return this.retry('wobble')
    this.rollFit = fit
    const yawCh = CHANNELS.find((ch) => ch !== p.ch && ch !== fit.ch)!
    // Yaw's sign is unknown until aim-right; start with +1.
    this.map = [
      { ch: p.ch, sign: p.sign },
      { ch: yawCh, sign: 1 },
      { ch: fit.ch, sign: fit.sign },
    ]
    this.scale = (p.scale + fit.scale) / 2
    if (this.steps.includes('scan')) this.witness = new YawWitness()
    this.advance()
  }

  private aim(raw: RawMotion, t: number, dt: number) {
    const map = this.map!
    const rate = (i: number) => map[i].sign * this.scale * (raw[map[i].ch] - this.bias[map[i].ch])
    const pitchRate = rate(0)
    const yawRate = rate(1)
    this.yaw += yawRate * dt
    this.pitch += pitchRate * dt
    const g = this.gravity(raw)
    if (g[2] < AIM_REMOTE_Z) {
      // Still on its edge from the last step: picking it up is the first instruction, no complaint yet.
      if (this.step === 'aim-center' && this.problem === null) {
        this.win.reset()
        this.phase = 'settle'
        this.progress = 0
        return
      }
      return this.fail('not-remote', this.step === 'aim-center' ? 'settle' : 'move')
    }
    const still = Math.hypot(pitchRate, yawRate, rate(2)) <= AIM_STILL_DPS
    if (!still) {
      this.win.reset()
      this.progress = 0
      this.phase = this.step === 'aim-center' ? 'settle' : 'move'
      return
    }
    const held = this.win.push(raw, g, t, 360) // a hand turning about the vertical keeps gravity put; the gyro decides
    const ms = this.step === 'aim-center' ? AIM_MS : POSE_MS

    if (this.step === 'aim-center') {
      this.problem = null
      this.setHold(held / ms)
      if (held < ms) return
      this.yaw = 0
      this.pitch = 0
      this.win.reset()
      if (this.viewpoint) {
        if (this.yawKnown) return this.finishView()
        // Still don't know which way is left: one turn to the right edge says.
        this.steps = SCAN_STEPS_TURN
        this.witness = null
      }
      return this.advance()
    }

    if (this.step === 'aim-right') {
      if (Math.abs(this.yaw) < YAW_MIN || Math.abs(this.yaw) < Math.abs(this.pitch)) {
        this.phase = 'move'
        this.progress = 0
        return
      }
      if (Math.abs(this.yaw) > YAW_MAX) return this.fail('too-big', 'move')
      this.problem = null
      this.setHold(held / ms)
      if (held < ms) return
      // Turning right is negative yaw (right-handed about +z).
      if (this.yaw > 0) map[1] = { ch: map[1].ch, sign: -map[1].sign as 1 | -1 }
      this.yawRange = Math.abs(this.yaw)
      this.win.reset()
      if (this.viewpoint) {
        this.yawKnown = true
        this.witness = null
        return this.finishView()
      }
      return this.advance()
    }

    // aim-top: pitch since the middle of the screen, top edge up is +.
    if (this.pitch <= -PITCH_MIN) return this.fail('wrong-way', 'move')
    if (this.pitch < PITCH_MIN) {
      this.phase = 'move'
      this.progress = 0
      return
    }
    if (this.pitch > PITCH_MAX) return this.fail('too-big', 'move')
    this.problem = null
    this.setHold(held / ms)
    if (held < ms) return
    this.pitchRange = this.pitch
    this.win.reset()
    const b = (i: number) => this.bias[map[i].ch]
    this.finish(map, this.scale, [b(0), b(1), b(2)], { yaw: this.yawRange, pitch: this.pitchRange })
  }

  // ---------------------------------------------------------------- helpers

  private setHold(p: number) {
    this.phase = 'hold'
    this.progress = Math.max(0, Math.min(1, p))
  }

  /** Breaks the current still window; the step goes on. */
  private fail(problem: CalibProblem, phase: CalibPhase) {
    this.win.reset()
    this.problem = problem
    this.phase = phase
    this.progress = 0
  }

  /** A tilt that cannot be used: back to the flat start pose of the same step. */
  private retry(problem: CalibProblem) {
    this.fail(problem, 'settle')
    this.gStart = null
    this.angle = undefined
  }

  private advance() {
    this.index += 1
    this.enter()
  }

  /** Fresh state for the step at `index`. */
  private enter() {
    this.phase = this.step === 'pitch' || this.step === 'roll' || this.step === 'scan' || this.step === 'aim-center' ? 'settle' : 'move'
    this.progress = 0
    this.problem = null
    this.angle = undefined
    this.gStart = null
    this.win.reset()
  }

  /** Scan path: the ranges come from where the player stood. */
  private finishView() {
    const map = this.map!
    const vp = this.viewpoint!
    const b = (i: number) => this.bias[map[i].ch]
    const r = new ViewPointer(vp).ranges()
    this.finish(map, this.scale, [b(0), b(1), b(2)], { yaw: r.yaw, pitch: r.pitch }, vp)
  }

  private finish(map: readonly AxisMap[], scale: number, bias: Vec3, pointer: { yaw: number; pitch: number }, viewpoint?: Viewpoint) {
    this.done = {
      version: CALIBRATION_VERSION,
      accelSign: this.accelSign,
      accelScale: this.accelScale,
      gyroBias: bias,
      gyroMap: map.map((m) => ({ ...m })) as [AxisMap, AxisMap, AxisMap],
      gyroScale: scale,
      pointer,
    }
    if (viewpoint) this.done.viewpoint = viewpoint
    this.index = this.steps.indexOf('verify')
    this.phase = 'hold'
    this.progress = 1
    this.problem = null
    this.angle = undefined
  }
}

function mean3(vs: Vec3[]): Vec3 {
  return [mean(vs.map((v) => v[0])), mean(vs.map((v) => v[1])), mean(vs.map((v) => v[2]))]
}
