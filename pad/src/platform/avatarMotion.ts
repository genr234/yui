// Lets the phone's tilt and shakes move every avatar's googly eyes. It only
// listens: on iOS motion events start once the player has allowed motion for
// a game, and it never asks by itself.
import { setAvatarGravity } from '@yui/avatar'
import { STANDARD_GRAVITY, loadCalibration, motionNeedsPermission, motionSupported } from './motion'

/** Shakes can't push harder than this many g. */
const MAX_G = 2.5

/**
 * Which way is down on the screen, in g (+x right, +y down), from the
 * accelerometer's x and y (m/s², gravity included).
 *
 * `sign` is +1 when the browser reports +9.8 on the axis pointing up
 * (Android) and -1 when it reports -9.8 (iOS). `angle` is
 * screen.orientation.angle.
 */
export function screenGravity(ax: number, ay: number, angle: number, sign: 1 | -1): [number, number] {
  const clamp = (n: number) => (Number.isFinite(n) ? Math.min(MAX_G, Math.max(-MAX_G, n)) + 0 : 0)
  // Gravity's direction along the device's own axes (x right, y towards the top edge).
  const dx = clamp((-ax * sign) / STANDARD_GRAVITY)
  const dy = clamp((-ay * sign) / STANDARD_GRAVITY)
  switch ((((Math.round(angle / 90) * 90) % 360) + 360) % 360) {
    case 90: // top edge to the left
      return [-dy + 0, -dx + 0]
    case 180:
      return [-dx + 0, dy]
    case 270: // top edge to the right
      return [dy, dx]
    default:
      return [dx, -dy + 0]
  }
}

/** Starts feeding the phone's motion to the avatars. Returns a function that stops it. */
export function followDeviceGravity(): () => void {
  if (!motionSupported()) return () => {}
  const sign = loadCalibration()?.accelSign ?? (motionNeedsPermission() ? -1 : 1)
  const onMotion = (e: DeviceMotionEvent) => {
    const a = e.accelerationIncludingGravity
    if (!a || a.x === null || a.y === null) return
    const angle = screen.orientation?.angle ?? Number((window as unknown as { orientation?: number }).orientation ?? 0)
    const [x, y] = screenGravity(a.x, a.y, angle, sign)
    setAvatarGravity(x, y)
  }
  window.addEventListener('devicemotion', onMotion)
  return () => {
    window.removeEventListener('devicemotion', onMotion)
    setAvatarGravity(0, 1)
  }
}
