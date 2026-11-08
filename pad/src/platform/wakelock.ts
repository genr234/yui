type Sentinel = { release(): Promise<void> }

let sentinel: Sentinel | null = null
let wanted = false

export const wakeLockSupported = () => typeof navigator !== 'undefined' && 'wakeLock' in navigator

async function acquire() {
  if (!wanted || sentinel || !wakeLockSupported()) return
  try {
    sentinel = await (navigator as any).wakeLock.request('screen')
    ;(sentinel as any).addEventListener?.('release', () => {
      sentinel = null
    })
  } catch {
    sentinel = null
  }
}

// The browser drops the lock when the tab is hidden; take it again when we come back.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') void acquire()
  })
}

/** Keep the screen on. Safe to call repeatedly; no-op where unsupported. */
export async function keepAwake(): Promise<void> {
  wanted = true
  await acquire()
}

export async function allowSleep(): Promise<void> {
  wanted = false
  const s = sentinel
  sentinel = null
  try {
    await s?.release()
  } catch {
    /* already released */
  }
}
