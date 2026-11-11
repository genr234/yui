import type { ControlMessage } from '@yui/protocol'

/*
 * Snap the screen, the phone's side (contracts §3 "snap"). The pad sends
 * {t:'snap'}; the kiosk answers with one `snap_result` and, when ok, that many
 * `snap_chunk` messages (base64 slices of a JPEG, in order). One snap is in
 * flight at a time, bounded in size, and gives up after `SNAP_TIMEOUT_MS`.
 */

export const SNAP_TIMEOUT_MS = 20000
export const SNAP_MAX_BYTES = 2 * 1024 * 1024
export const SNAP_MAX_CHUNKS = 128

export interface SnapShot {
  blob: Blob
  w?: number
  h?: number
}

export interface SnapHandlers {
  /** The picture arrived. */
  done(shot: SnapShot): void
  /** The kiosk said no, the transfer broke, or it timed out. `text` is for the player. */
  fail(text: string): void
}

interface Timers {
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(h: unknown): void
}

const realTimers: Timers = { setTimeout: (fn, ms) => setTimeout(fn, ms), clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>) }

function decode(b64: string): Uint8Array | null {
  try {
    const bin = atob(b64)
    const out = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
    return out
  } catch {
    return null
  }
}

export class SnapReceiver {
  private waiting = false
  private timer: unknown = null
  private id = ''
  private expect = 0
  private size = 0
  private w: number | undefined
  private h: number | undefined
  private parts: Uint8Array[] = []
  private got = 0
  private bytes = 0
  private announced = false

  constructor(
    private handlers: SnapHandlers,
    private timers: Timers = realTimers,
  ) {}

  /** True from `begin()` until the picture or an error arrives. */
  get busy() {
    return this.waiting
  }

  /** Call right after sending `snap`. Returns false when one is already in flight. */
  begin(): boolean {
    if (this.waiting) return false
    this.reset()
    this.waiting = true
    this.timer = this.timers.setTimeout(() => this.finish('The screen took too long to answer'), SNAP_TIMEOUT_MS)
    return true
  }

  /** Forget the transfer (the session ended). */
  cancel() {
    this.waiting = false
    this.reset()
  }

  /** Feed every control message; returns true when it was a snap message. */
  handle(msg: ControlMessage): boolean {
    if (msg.t === 'snap_result') {
      if (!this.waiting) return true
      if (!msg.ok) {
        this.finish(msg.error || "Couldn't grab the screen")
        return true
      }
      const chunks = msg.chunks ?? 0
      const size = msg.size ?? 0
      if (chunks < 1 || chunks > SNAP_MAX_CHUNKS || size < 1 || size > SNAP_MAX_BYTES) {
        this.finish('The snapshot was too large')
        return true
      }
      this.id = msg.id
      this.expect = chunks
      this.size = size
      this.w = msg.w
      this.h = msg.h
      this.announced = true
      return true
    }
    if (msg.t === 'snap_chunk') {
      if (!this.waiting || !this.announced || msg.id !== this.id) return true
      if (msg.seq !== this.got) return this.finish('The snapshot got scrambled'), true
      const part = decode(msg.data)
      if (!part) return this.finish('The snapshot got scrambled'), true
      this.bytes += part.length
      if (this.bytes > this.size) return this.finish('The snapshot got scrambled'), true
      this.parts.push(part)
      this.got++
      if (this.got === this.expect) {
        if (this.bytes !== this.size) {
          this.finish('The snapshot got scrambled')
          return true
        }
        const blob = new Blob(this.parts as BlobPart[], { type: 'image/jpeg' })
        const shot = { blob, w: this.w, h: this.h }
        this.waiting = false
        this.reset()
        this.handlers.done(shot)
      }
      return true
    }
    return false
  }

  private finish(error: string) {
    this.waiting = false
    this.reset()
    this.handlers.fail(error)
  }

  private reset() {
    if (this.timer !== null) this.timers.clearTimeout(this.timer)
    this.timer = null
    this.parts = []
    this.got = this.bytes = this.expect = this.size = 0
    this.announced = false
    this.id = ''
  }
}

/** A Blob as a data: URL (the pad's CSP allows data: images, not blob: ones). */
export function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result))
    r.onerror = () => reject(r.error)
    r.readAsDataURL(blob)
  })
}

export function snapFileName(now = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `yui-snap-${now.getFullYear()}${p(now.getMonth() + 1)}${p(now.getDate())}-${p(now.getHours())}${p(now.getMinutes())}${p(now.getSeconds())}.jpg`
}

/** True when this browser can share the file through the system share sheet. */
export function canShareFile(file: File): boolean {
  const nav = navigator as Navigator & { canShare?: (d: ShareData) => boolean }
  return typeof nav.share === 'function' && typeof nav.canShare === 'function' && nav.canShare({ files: [file] })
}
