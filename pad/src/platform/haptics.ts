/**
 * Haptics. Android: navigator.vibrate. iOS has no vibrate(), but Safari 18+
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

export function cleanPattern(pattern: unknown[]): number[] {
  return pattern
    .filter((n): n is number => typeof n === 'number' && Number.isFinite(n))
    .slice(0, 32)
    .map((n) => Math.max(0, Math.min(n, 5000)))
}

let pending: ReturnType<typeof setTimeout>[] = []

/** Play a `navigator.vibrate`-style pattern (kiosk `haptic` message). */
export function playPattern(pattern: number[]) {
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
  if (canVibrate()) navigator.vibrate(8)
  else if (canSwitchTick()) tick()
}
