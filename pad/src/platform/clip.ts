import { CLIP_CHUNK_BYTES, CLIP_MAX_BYTES, type ClipLayout, type ControlMessage } from '@yui/protocol'

/*
 * Clip the last moments, the phone's side (contracts §3 "clip"). Two steps:
 *
 *   pad `clip`  -> kiosk pins "now" and answers `clip_ready` {id, secs, camera}
 *   pad `clip_make` {id, secs, camera, layout}
 *               -> kiosk makes the video (can take a while), answers
 *                  `clip_result` and, when ok, that many `clip_chunk`s.
 *
 * The chunks are paced by the kiosk, so while receiving the timeout is on
 * inactivity (reset by every chunk), not on the whole transfer.
 */

export const CLIP_MARK_TIMEOUT_MS = 10_000
export const CLIP_MAKE_TIMEOUT_MS = 90_000
export const CLIP_IDLE_TIMEOUT_MS = 30_000
export const CLIP_MAX_CHUNKS = Math.ceil(CLIP_MAX_BYTES / CLIP_CHUNK_BYTES)

export type ClipStage = 'idle' | 'marking' | 'ready' | 'making' | 'receiving'

export interface ClipReadyInfo {
  id: string
  secs: number
  camera: boolean
}

export interface ClipVideo {
  blob: Blob
  w?: number
  h?: number
  secs?: number
}

export interface ClipHandlers {
  /** The kiosk pinned the moment; show the options. */
  ready(info: ClipReadyInfo): void
  /** Bytes are arriving: 0..1. */
  progress?(fraction: number): void
  /** The video arrived. */
  done(clip: ClipVideo): void
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

const SCRAMBLED = 'The clip got scrambled'

export class ClipReceiver {
  private stageNow: ClipStage = 'idle'
  private timer: unknown = null
  private readyInfo: ClipReadyInfo | null = null
  private id = ''
  private expect = 0
  private size = 0
  private w: number | undefined
  private h: number | undefined
  private secs: number | undefined
  private parts: Uint8Array[] = []
  private got = 0
  private bytes = 0

  constructor(
    private handlers: ClipHandlers,
    private timers: Timers = realTimers,
  ) {}

  get stage(): ClipStage {
    return this.stageNow
  }

  /** True while a step is waiting on the kiosk (marking, making, receiving). */
  get busy() {
    return this.stageNow === 'marking' || this.stageNow === 'making' || this.stageNow === 'receiving'
  }

  /** The pinned moment while the options are shown. */
  get pinned(): ClipReadyInfo | null {
    return this.readyInfo
  }

  /** Call right after sending `clip`. Returns false when a clip is already under way. */
  begin(): boolean {
    if (this.stageNow !== 'idle') return false
    this.reset()
    this.stageNow = 'marking'
    this.arm(CLIP_MARK_TIMEOUT_MS, 'The screen took too long to answer')
    return true
  }

  /**
   * Call right after sending `clip_make` for the pinned moment. Returns the
   * message to send, or null when nothing is pinned.
   */
  make(opts: { secs: number; camera: boolean; layout: ClipLayout }): { t: 'clip_make'; id: string; secs: number; camera: boolean; layout: ClipLayout } | null {
    if (this.stageNow !== 'ready' || !this.readyInfo) return null
    const id = this.readyInfo.id
    this.stageNow = 'making'
    this.id = id
    this.arm(CLIP_MAKE_TIMEOUT_MS, 'Making the clip took too long')
    return { t: 'clip_make', id, secs: opts.secs, camera: opts.camera && this.readyInfo.camera, layout: opts.layout }
  }

  /** Forget the clip (the player closed the sheet, or the session ended). */
  cancel() {
    this.stageNow = 'idle'
    this.reset()
  }

  /** Feed every control message; returns true when it was a clip message. */
  handle(msg: ControlMessage): boolean {
    switch (msg.t) {
      case 'clip_ready': {
        if (this.stageNow !== 'marking') return true
        this.clearTimer()
        const secs = Math.max(0, Math.floor(msg.secs))
        this.readyInfo = { id: msg.id, secs, camera: msg.camera === true }
        this.stageNow = 'ready'
        this.handlers.ready(this.readyInfo)
        return true
      }
      case 'clip_result': {
        // A refusal can answer `clip` (marking) or `clip_make` (making).
        if (this.stageNow === 'marking') {
          if (!msg.ok) this.finish(msg.error || "Couldn't make the clip")
          return true
        }
        if (this.stageNow !== 'making') return true
        // A refusal of the make may come back as id "e" (contracts §3).
        if (!msg.ok && (msg.id === this.id || msg.id === 'e')) {
          this.finish(msg.error || "Couldn't make the clip")
          return true
        }
        if (msg.id !== this.id || !msg.ok) return true
        const chunks = msg.chunks ?? 0
        const size = msg.size ?? 0
        if (chunks < 1 || chunks > CLIP_MAX_CHUNKS || size < 1 || size > CLIP_MAX_BYTES) {
          this.finish('The clip was too large')
          return true
        }
        this.expect = chunks
        this.size = size
        this.w = msg.w
        this.h = msg.h
        this.secs = msg.secs
        this.stageNow = 'receiving'
        this.arm(CLIP_IDLE_TIMEOUT_MS, 'The clip stopped arriving')
        this.handlers.progress?.(0)
        return true
      }
      case 'clip_chunk': {
        if (this.stageNow !== 'receiving' || msg.id !== this.id) return true
        if (msg.seq !== this.got) return this.finish(SCRAMBLED), true
        const part = decode(msg.data)
        if (!part) return this.finish(SCRAMBLED), true
        this.bytes += part.length
        if (this.bytes > this.size) return this.finish(SCRAMBLED), true
        this.parts.push(part)
        this.got++
        if (this.got < this.expect) {
          this.arm(CLIP_IDLE_TIMEOUT_MS, 'The clip stopped arriving')
          this.handlers.progress?.(this.got / this.expect)
          return true
        }
        if (this.bytes !== this.size) return this.finish(SCRAMBLED), true
        const clip: ClipVideo = { blob: new Blob(this.parts as BlobPart[], { type: 'video/mp4' }), w: this.w, h: this.h, secs: this.secs }
        this.stageNow = 'idle'
        this.reset()
        this.handlers.progress?.(1)
        this.handlers.done(clip)
        return true
      }
      default:
        return false
    }
  }

  private arm(ms: number, text: string) {
    this.clearTimer()
    this.timer = this.timers.setTimeout(() => {
      this.timer = null
      this.finish(text)
    }, ms)
  }

  private clearTimer() {
    if (this.timer !== null) this.timers.clearTimeout(this.timer)
    this.timer = null
  }

  private finish(error: string) {
    this.stageNow = 'idle'
    this.reset()
    this.handlers.fail(error)
  }

  private reset() {
    this.clearTimer()
    this.readyInfo = null
    this.parts = []
    this.got = this.bytes = this.expect = this.size = 0
    this.w = this.h = this.secs = undefined
    this.id = ''
  }
}

/** Length choices the sheet offers, and the default for `secs` seconds available. */
export const CLIP_LENGTHS = [10, 20, 30] as const

export function defaultClipLength(available: number): number {
  const ok = CLIP_LENGTHS.filter((n) => n <= available)
  if (ok.includes(20)) return 20
  if (ok.length) return ok[ok.length - 1]
  return Math.max(1, Math.floor(available))
}

export function clipFileName(now = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `yui-clip-${now.getFullYear()}${p(now.getMonth() + 1)}${p(now.getDate())}-${p(now.getHours())}${p(now.getMinutes())}${p(now.getSeconds())}.mp4`
}
