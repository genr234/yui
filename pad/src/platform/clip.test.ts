import { describe, expect, test } from 'bun:test'
import type { ControlMessage } from '@yui/protocol'
import {
  CLIP_IDLE_TIMEOUT_MS,
  CLIP_MAKE_TIMEOUT_MS,
  CLIP_MARK_TIMEOUT_MS,
  CLIP_MAX_CHUNKS,
  ClipReceiver,
  clipFileName,
  defaultClipLength,
  type ClipReadyInfo,
  type ClipVideo,
} from './clip'

function rig() {
  const ready: ClipReadyInfo[] = []
  const done: ClipVideo[] = []
  const fails: string[] = []
  const progress: number[] = []
  const timers = new Map<number, { fn: () => void; ms: number }>()
  let next = 1
  const rx = new ClipReceiver(
    { ready: (i) => ready.push(i), done: (c) => done.push(c), fail: (t) => fails.push(t), progress: (f) => progress.push(f) },
    {
      setTimeout: (fn, ms) => {
        timers.set(next, { fn, ms })
        return next++
      },
      clearTimeout: (h) => void timers.delete(h as number),
    },
  )
  const fire = () => {
    const [[id, t]] = [...timers]
    timers.delete(id)
    t.fn()
  }
  return { rx, ready, done, fails, progress, timers, fire }
}

const b64 = (bytes: number[]) => btoa(String.fromCharCode(...bytes))
const readyMsg = (o: Record<string, unknown> = {}) => ({ t: 'clip_ready', id: 'c1', secs: 30, camera: true, ...o }) as ControlMessage
const result = (o: Record<string, unknown> = {}) =>
  ({ t: 'clip_result', id: 'c1', ok: true, mime: 'video/mp4', size: 6, chunks: 2, w: 1280, h: 720, secs: 20, ...o }) as ControlMessage
const chunk = (seq: number, bytes: number[], id = 'c1') => ({ t: 'clip_chunk', id, seq, data: b64(bytes) }) as ControlMessage

function toReady() {
  const r = rig()
  r.rx.begin()
  r.rx.handle(readyMsg())
  return r
}

describe('ClipReceiver', () => {
  test('mark, make and receive a clip', async () => {
    const { rx, ready, done, fails, progress, timers } = rig()
    expect(rx.begin()).toBe(true)
    expect(rx.begin()).toBe(false)
    expect(rx.stage).toBe('marking')
    expect([...timers.values()][0].ms).toBe(CLIP_MARK_TIMEOUT_MS)
    expect(rx.handle(readyMsg())).toBe(true)
    expect(rx.stage).toBe('ready')
    expect(rx.busy).toBe(false)
    expect(ready).toEqual([{ id: 'c1', secs: 30, camera: true }])
    expect(timers.size).toBe(0)

    const make = rx.make({ secs: 20, camera: true, layout: 'tall' })
    expect(make).toEqual({ t: 'clip_make', id: 'c1', secs: 20, camera: true, layout: 'tall' })
    expect(rx.stage).toBe('making')
    expect([...timers.values()][0].ms).toBe(CLIP_MAKE_TIMEOUT_MS)

    rx.handle(result())
    expect(rx.stage).toBe('receiving')
    expect([...timers.values()][0].ms).toBe(CLIP_IDLE_TIMEOUT_MS)
    rx.handle(chunk(0, [0, 0, 0, 0x18]))
    rx.handle(chunk(1, [0x66, 0x74]))
    expect(fails).toEqual([])
    expect(done).toHaveLength(1)
    expect(done[0].blob.type).toBe('video/mp4')
    expect(done[0].w).toBe(1280)
    expect(done[0].secs).toBe(20)
    expect([...new Uint8Array(await done[0].blob.arrayBuffer())]).toEqual([0, 0, 0, 0x18, 0x66, 0x74])
    expect(progress).toEqual([0, 0.5, 1])
    expect(rx.stage).toBe('idle')
    expect(timers.size).toBe(0)
  })

  test('camera is never asked for when the kiosk has none', () => {
    const { rx } = rig()
    rx.begin()
    rx.handle(readyMsg({ camera: false }))
    expect(rx.make({ secs: 10, camera: true, layout: 'wide' })?.camera).toBe(false)
  })

  test('make without a pinned moment does nothing', () => {
    const { rx } = rig()
    expect(rx.make({ secs: 10, camera: false, layout: 'wide' })).toBeNull()
  })

  test('a refusal while marking fails with the kiosk text', () => {
    const { rx, fails } = rig()
    rx.begin()
    rx.handle({ t: 'clip_result', id: 'e', ok: false, error: 'Wait a moment' })
    expect(fails).toEqual(['Wait a moment'])
    expect(rx.stage).toBe('idle')
    expect(rx.begin()).toBe(true)
  })

  test('a refusal while making fails, by id or as "e"', () => {
    for (const id of ['c1', 'e']) {
      const { rx, fails } = toReady()
      rx.make({ secs: 10, camera: false, layout: 'wide' })
      rx.handle({ t: 'clip_result', id, ok: false, error: 'That moment is gone, clip again' })
      expect(fails).toEqual(['That moment is gone, clip again'])
    }
  })

  test('messages for another clip are ignored', () => {
    const { rx, fails, done } = toReady()
    rx.make({ secs: 10, camera: false, layout: 'wide' })
    rx.handle(result({ id: 'other' }))
    expect(rx.stage).toBe('making')
    rx.handle(result())
    rx.handle(chunk(0, [1, 2, 3, 4], 'other'))
    expect(rx.stage).toBe('receiving')
    expect(fails).toEqual([])
    expect(done).toEqual([])
  })

  test('out-of-order, oversize and bad chunks fail', () => {
    for (const bad of [chunk(1, [1, 2, 3]), chunk(0, [1, 2, 3, 4, 5, 6, 7]), { t: 'clip_chunk', id: 'c1', seq: 0, data: '***' } as ControlMessage]) {
      const { rx, fails } = toReady()
      rx.make({ secs: 10, camera: false, layout: 'wide' })
      rx.handle(result())
      rx.handle(bad)
      expect(fails).toEqual(['The clip got scrambled'])
      expect(rx.stage).toBe('idle')
    }
  })

  test('a short total fails', () => {
    const { rx, fails } = toReady()
    rx.make({ secs: 10, camera: false, layout: 'wide' })
    rx.handle(result())
    rx.handle(chunk(0, [1, 2]))
    rx.handle(chunk(1, [3]))
    expect(fails).toEqual(['The clip got scrambled'])
  })

  test('too many chunks or bytes is refused', () => {
    for (const o of [{ chunks: CLIP_MAX_CHUNKS + 1 }, { size: 49 * 1024 * 1024 }, { chunks: 0 }]) {
      const { rx, fails } = toReady()
      rx.make({ secs: 10, camera: false, layout: 'wide' })
      rx.handle(result(o))
      expect(fails).toEqual(['The clip was too large'])
    }
  })

  test('timeouts at each stage', () => {
    let r = rig()
    r.rx.begin()
    r.fire()
    expect(r.fails).toEqual(['The screen took too long to answer'])

    r = toReady()
    r.rx.make({ secs: 10, camera: false, layout: 'wide' })
    r.fire()
    expect(r.fails).toEqual(['Making the clip took too long'])

    r = toReady()
    r.rx.make({ secs: 10, camera: false, layout: 'wide' })
    r.rx.handle(result({ chunks: 3, size: 3 }))
    r.rx.handle(chunk(0, [1]))
    expect(r.timers.size).toBe(1) // the idle timer was re-armed, not stacked
    r.fire()
    expect(r.fails).toEqual(['The clip stopped arriving'])
    expect(r.rx.stage).toBe('idle')
  })

  test('cancel drops everything and clip messages are still swallowed', () => {
    const { rx, timers, fails, ready } = rig()
    rx.begin()
    rx.cancel()
    expect(timers.size).toBe(0)
    expect(rx.handle(readyMsg())).toBe(true)
    expect(ready).toEqual([])
    expect(fails).toEqual([])
    expect(rx.handle({ t: 'snap' } as ControlMessage)).toBe(false)
  })
})

describe('helpers', () => {
  test('defaultClipLength', () => {
    expect(defaultClipLength(30)).toBe(20)
    expect(defaultClipLength(20)).toBe(20)
    expect(defaultClipLength(15)).toBe(10)
    expect(defaultClipLength(6)).toBe(6)
  })
  test('clipFileName', () => {
    expect(clipFileName(new Date(2026, 10, 28, 9, 5, 7))).toBe('yui-clip-20261128-090507.mp4')
  })
})
