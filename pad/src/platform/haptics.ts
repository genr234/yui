/**
 * Haptics. Android: navigator.vibrate (kiosk `haptic` messages carry a
 * vocabulary name or an on/off ms pattern). iOS has no vibrate(), but Safari 18+
 * plays the system "switch" tick when an `<input type="checkbox" switch>` is
 * toggled, including through a click on its label. We keep one hidden switch
 * and click its label once per pulse of the pattern.
 */

let label: HTMLLabelElement | null = null

function iosSwitch(): HTMLLabelElement | null {
  if (typeof document === 'undefined') return null
  if (label?.isConnected) return label
  const input = document.createElement('input')
  input.type = 'checkbox'
  input.setAttribute('switch', '')
  input.id = 'yui-haptic-switch'
  input.tabIndex = -1
  input.setAttribute('aria-hidden', 'true')
  label = document.createElement('label')
  label.htmlFor = input.id
  label.setAttribute('aria-hidden', 'true')
  label.className = 'haptic-switch'
  label.appendChild(input)
  document.body.appendChild(label)
  return label
}

export function canVibrate(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function'
}

/** iOS 18+ (switch inputs exist) without vibrate(). */
export function canSwitchTick(): boolean {
  if (typeof document === 'undefined' || canVibrate()) return false
  const ua = navigator.userAgent
  return /iPhone|iPad|iPod/.test(ua) || (ua.includes('Mac') && navigator.maxTouchPoints > 1)
}

export function hapticsSupported(): boolean {
  return canVibrate() || canSwitchTick()
}

function tick() {
  iosSwitch()?.click()
}

/**
 * Tick times (ms) for a vibrate-style pattern [on, off, on, …] on iOS: a
 * switch tick is one short click, so a long pulse becomes a tick every 80 ms.
 */
export function tickTimes(pattern: number[]): number[] {
  const out: number[] = []
  let t = 0
  pattern.forEach((ms, i) => {
    if (i % 2 === 0 && ms > 0) for (let k = 0; k < Math.max(1, Math.floor(ms / 80)); k++) out.push(t + k * 80)
    t += ms
  })
  return out
}

/** Same limits as the protocol: 16 steps, each <= 1000 ms, <= 4 s in all. */
export function cleanPattern(pattern: unknown[]): number[] {
  const out: number[] = []
  let total = 0
  for (const n of pattern) {
    if (typeof n !== 'number' || !Number.isFinite(n)) continue
    if (out.length >= 16) break
    const ms = Math.max(0, Math.min(n, 1000, 4000 - total))
    out.push(ms)
    total += ms
  }
  return out
}

/**
 * The pattern vocabulary, as `navigator.vibrate` on/off steps. Names resolve
 * here, on the phone, so timings can be tuned per platform without touching
 * the kiosk or any game.
 */
export const HAPTIC_PATTERNS: Record<string, number[]> = {
  tap: [12],
  success: [30, 50, 60],
  fail: [70, 40, 70, 40, 160],
  hit: [45],
  heavy: [160],
  heartbeat: [40, 90, 70, 450, 40, 90, 70],
  countdown: [50],
  win: [60, 50, 60, 50, 60, 50, 220],
}

/** Shorten the pulses (even steps) for a weaker feel; 0 is silent (null). */
export function scaleIntensity(pattern: number[], intensity: number): number[] | null {
  if (!(intensity > 0)) return null
  if (intensity >= 1) return pattern
  return pattern.map((ms, i) => (i % 2 === 0 && ms > 0 ? Math.max(5, Math.round(ms * Math.max(0.2, intensity))) : ms))
}

/** What a kiosk `haptic` message should play, or null for nothing (unknown name, bad input, intensity 0). */
export function resolveHaptic(msg: { name?: unknown; pattern?: unknown; intensity?: unknown }): number[] | null {
  let steps: number[] | null = null
  if (typeof msg.name === 'string') {
    const named = Object.prototype.hasOwnProperty.call(HAPTIC_PATTERNS, msg.name) ? HAPTIC_PATTERNS[msg.name] : null
    steps = named ? [...named] : null
  } else if (Array.isArray(msg.pattern)) {
    steps = cleanPattern(msg.pattern)
  }
  if (!steps?.length) return null
  const intensity = typeof msg.intensity === 'number' && Number.isFinite(msg.intensity) ? Math.min(1, msg.intensity) : 1
  return scaleIntensity(steps, intensity)
}

const VIBRATION_KEY = 'yui_pad_vibration'

/** The Home sheet's Vibration switch; on unless this phone turned it off. */
export function vibrationEnabled(): boolean {
  try {
    return localStorage.getItem(VIBRATION_KEY) !== 'off'
  } catch {
    return true
  }
}

export function setVibrationEnabled(on: boolean) {
  try {
    if (on) localStorage.removeItem(VIBRATION_KEY)
    else localStorage.setItem(VIBRATION_KEY, 'off')
  } catch {
    // private mode: the switch just won't stick
  }
  if (!on) stopHaptics()
}

let pending: ReturnType<typeof setTimeout>[] = []

function stopHaptics() {
  for (const t of pending) clearTimeout(t)
  pending = []
  if (canVibrate()) navigator.vibrate(0)
}

/** Play a kiosk `haptic` message (name or pattern, optional intensity). */
export function playHaptic(msg: { name?: unknown; pattern?: unknown; intensity?: unknown }) {
  const steps = resolveHaptic(msg)
  if (steps) playPattern(steps)
}

/** Play a `navigator.vibrate`-style pattern. Silent when Vibration is off or unsupported. */
export function playPattern(pattern: number[]) {
  if (!vibrationEnabled()) return
  const clean = cleanPattern(pattern)
  if (!clean.length) return
  if (canVibrate()) {
    navigator.vibrate(clean)
    return
  }
  if (!canSwitchTick()) return
  for (const t of pending) clearTimeout(t)
  pending = tickTimes(clean).map((s) => setTimeout(tick, s))
}

/** Light local press feedback. */
export function pressTick() {
  if (!vibrationEnabled()) return
  if (canVibrate()) navigator.vibrate(8)
  else if (canSwitchTick()) tick()
}
