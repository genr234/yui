import { writable } from 'svelte/store'

/**
 * A private video view the kiosk sends to this phone (a `surface` node shows
 * it). Every peer carries an H.264 receive track from its first offer; the
 * kiosk only writes packets into the track of the phone that is shown the
 * surface, and tells it so with a `surface` control message.
 */
export interface SurfaceView {
  id: string
  state: 'on' | 'off'
  width: number
  height: number
}

export const surfaceStream = writable<MediaStream | null>(null)
export const surfaceState = writable<SurfaceView | null>(null)

/** Applies a kiosk `surface` message. */
export function applySurface(msg: { id: string; state: 'on' | 'off'; width?: number; height?: number }) {
  surfaceState.set({ id: msg.id, state: msg.state, width: msg.width ?? 0, height: msg.height ?? 0 })
}

export function resetSurface() {
  surfaceStream.set(null)
  surfaceState.set(null)
}

/** The parts of an RTCRtpReceiver this file touches (some are not in every browser). */
interface ReceiverLike {
  jitterBufferTarget?: number | null
  playoutDelayHint?: number | null
}

/**
 * The video track of a peer: keep its stream for the `surface` node and ask
 * the browser for the least buffering it allows (where it exists).
 */
export function attachSurfaceTrack(ev: { track: { kind: string }; streams: readonly MediaStream[]; receiver?: ReceiverLike }) {
  if (ev.track.kind !== 'video') return
  const rx = ev.receiver
  if (rx) {
    try {
      if ('jitterBufferTarget' in rx) rx.jitterBufferTarget = 0
      if ('playoutDelayHint' in rx) rx.playoutDelayHint = 0
    } catch {
      /* the browser refused the hint */
    }
  }
  surfaceStream.set(ev.streams[0] ?? new MediaStream([ev.track as MediaStreamTrack]))
}

/**
 * Can this phone show a surface? It needs a receiver that lists H.264. Where
 * the capability API is missing but WebRTC exists, assume yes.
 */
export function videoSupported(
  g: {
    RTCPeerConnection?: unknown
    RTCRtpReceiver?: { getCapabilities?: (kind: string) => { codecs: { mimeType: string }[] } | null }
  } = globalThis as never,
): boolean {
  if (!g.RTCPeerConnection) return false
  const get = g.RTCRtpReceiver?.getCapabilities
  if (typeof get !== 'function') return true
  try {
    const caps = get.call(g.RTCRtpReceiver, 'video')
    return !!caps?.codecs.some((c) => c.mimeType.toLowerCase() === 'video/h264')
  } catch {
    return false
  }
}
