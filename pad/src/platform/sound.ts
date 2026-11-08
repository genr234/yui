/**
 * Clips the kiosk asks the phone to play (`sound {url, volume}`). Only URLs on
 * this server or data: URIs play (the CSP allows nothing else anyway). Audio
 * elements are cached per URL so repeated clips start instantly.
 */
const cache = new Map<string, HTMLAudioElement>()
let master = 1

export function setMasterVolume(v: number) {
  master = Math.max(0, Math.min(1, v))
}

export function soundAllowed(url: string, origin = location.origin): boolean {
  if (url.startsWith('data:audio/')) return true
  try {
    return new URL(url, origin).origin === origin
  } catch {
    return false
  }
}

export function playSound(url: string, volume = 1) {
  if (!soundAllowed(url)) return
  let a = cache.get(url)
  if (!a) {
    a = new Audio(url)
    a.preload = 'auto'
    if (cache.size > 32) cache.clear()
    cache.set(url, a)
  }
  a.volume = Math.max(0, Math.min(1, (Number.isFinite(volume) ? volume : 1) * master))
  try {
    a.currentTime = 0
  } catch {
    /* not loaded yet */
  }
  void a.play().catch(() => {})
}

/** iOS only lets audio start after a gesture: call from the first tap. */
export function unlockAudio() {
  for (const a of cache.values()) {
    a.muted = true
    void a.play().then(() => {
      a.pause()
      a.muted = false
    }).catch(() => {
      a.muted = false
    })
  }
}
