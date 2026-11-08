export type MotionPermission = 'granted' | 'denied' | 'unsupported'

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

export function vibrate(pattern: number | number[]): boolean {
  return canVibrate() ? navigator.vibrate(pattern) : false
}
