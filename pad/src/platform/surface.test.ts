import { beforeEach, describe, expect, test } from 'bun:test'
import { get } from 'svelte/store'
import { applySurface, attachSurfaceTrack, resetSurface, surfaceState, surfaceStream, videoSupported } from './surface'

beforeEach(() => resetSurface())

describe('surface state', () => {
  test('on then off', () => {
    applySurface({ id: 'second-screen', state: 'on', width: 854, height: 480 })
    expect(get(surfaceState)).toEqual({ id: 'second-screen', state: 'on', width: 854, height: 480 })
    applySurface({ id: 'second-screen', state: 'off' })
    expect(get(surfaceState)).toEqual({ id: 'second-screen', state: 'off', width: 0, height: 0 })
    resetSurface()
    expect(get(surfaceState)).toBeNull()
  })
})

describe('attachSurfaceTrack', () => {
  const stream = { id: 's' } as unknown as MediaStream
  test('keeps the video stream and zeroes the playout hints where they exist', () => {
    const receiver = { jitterBufferTarget: null as number | null, playoutDelayHint: 0.5 }
    attachSurfaceTrack({ track: { kind: 'video' }, streams: [stream], receiver })
    expect(get(surfaceStream)).toBe(stream)
    expect(receiver.jitterBufferTarget).toBe(0)
    expect(receiver.playoutDelayHint).toBe(0)
  })
  test('does not add hints a browser lacks; ignores audio', () => {
    const receiver: Record<string, unknown> = {}
    attachSurfaceTrack({ track: { kind: 'video' }, streams: [stream], receiver })
    expect(Object.keys(receiver)).toEqual([])
    resetSurface()
    attachSurfaceTrack({ track: { kind: 'audio' }, streams: [stream] })
    expect(get(surfaceStream)).toBeNull()
  })
})

describe('videoSupported (hello caps.video)', () => {
  const caps = (...mime: string[]) => ({ getCapabilities: () => ({ codecs: mime.map((mimeType) => ({ mimeType })) }) })
  test('needs WebRTC at all', () => expect(videoSupported({})).toBe(false))
  test('true when H264 is listed (any case)', () => {
    expect(videoSupported({ RTCPeerConnection: {}, RTCRtpReceiver: caps('video/VP8', 'video/H264') })).toBe(true)
  })
  test('false when H264 is missing', () => {
    expect(videoSupported({ RTCPeerConnection: {}, RTCRtpReceiver: caps('video/VP8') })).toBe(false)
  })
  test('plain true when the capability API is missing', () => {
    expect(videoSupported({ RTCPeerConnection: {} })).toBe(true)
  })
})
