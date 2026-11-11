import { describe, expect, test } from 'bun:test'
import type { ControlMessage } from '@yui/protocol'
import { SNAP_MAX_CHUNKS, SNAP_TIMEOUT_MS, SnapReceiver, snapFileName, type SnapShot } from './snap'

function rig() {
  const done: SnapShot[] = []
  const fails: string[] = []
  const timers = new Map<number, { fn: () => void; ms: number }>()
  let next = 1
  const rx = new SnapReceiver(
    { done: (s) => done.push(s), fail: (t) => fails.push(t) },
    {
      setTimeout: (fn, ms) => {
        timers.set(next, { fn, ms })
        return next++
      },
      clearTimeout: (h) => void timers.delete(h as number),
    },
  )
  return { rx, done, fails, timers }
}

const b64 = (bytes: number[]) => btoa(String.fromCharCode(...bytes))
const result = (o: Record<string, unknown> = {}) => ({ t: 'snap_result', id: 's1', ok: true, mime: 'image/jpeg', size: 6, chunks: 2, w: 10, h: 20, ...o }) as ControlMessage
const chunk = (seq: number, bytes: number[], id = 's1') => ({ t: 'snap_chunk', id, seq, data: b64(bytes) }) as ControlMessage

describe('SnapReceiver', () => {
  test('reassembles ordered chunks into a JPEG blob', async () => {
    const { rx, done, fails, timers } = rig()
    expect(rx.begin()).toBe(true)
    expect(rx.begin()).toBe(false) // one in flight
    rx.handle(result())
    rx.handle(chunk(0, [0xff, 0xd8, 1, 2]))
    expect(rx.busy).toBe(true)
    rx.handle(chunk(1, [3, 4]))
    expect(rx.busy).toBe(false)
    expect(fails).toEqual([])
    expect(done).toHaveLength(1)
    expect(done[0].blob.type).toBe('image/jpeg')
    expect(done[0].w).toBe(10)
    expect([...new Uint8Array(await done[0].blob.arrayBuffer())]).toEqual([0xff, 0xd8, 1, 2, 3, 4])
    expect(timers.size).toBe(0) // timeout cleared
  })

  test('an error result fails with the kiosk text', () => {
    const { rx, fails } = rig()
    rx.begin()
    rx.handle({ t: 'snap_result', id: 'e', ok: false, error: 'Wait a moment' })
    expect(fails).toEqual(['Wait a moment'])
    expect(rx.busy).toBe(false)
    expect(rx.begin()).toBe(true)
  })

  test('out-of-order, oversize and bad chunks fail', () => {
    for (const bad of [chunk(1, [1, 2, 3]), chunk(0, [1, 2, 3, 4, 5, 6, 7]), { t: 'snap_chunk', id: 's1', seq: 0, data: '***' } as ControlMessage]) {
      const { rx, done, fails } = rig()
      rx.begin()
      rx.handle(result())
      rx.handle(bad)
      expect(fails).toHaveLength(1)
      expect(done).toHaveLength(0)
    }
  })

  test('a short transfer is rejected', () => {
    const { rx, done, fails } = rig()
    rx.begin()
    rx.handle(result({ size: 10 }))
    rx.handle(chunk(0, [1, 2, 3]))
    rx.handle(chunk(1, [4, 5, 6]))
    expect(done).toHaveLength(0)
    expect(fails).toHaveLength(1)
  })

  test('announces over the limit are refused', () => {
    const { rx, fails } = rig()
    rx.begin()
    rx.handle(result({ chunks: SNAP_MAX_CHUNKS + 1 }))
    expect(fails).toHaveLength(1)
  })

  test('chunks for another id or without a begin are ignored', () => {
    const { rx, done, fails } = rig()
    rx.handle(result())
    rx.handle(chunk(0, [1]))
    rx.begin()
    rx.handle(result())
    rx.handle(chunk(0, [1, 2, 3], 'other'))
    expect(done.length + fails.length).toBe(0)
    expect(rx.busy).toBe(true)
  })

  test('times out', () => {
    const { rx, fails, timers } = rig()
    rx.begin()
    const [t] = [...timers.values()]
    expect(t.ms).toBe(SNAP_TIMEOUT_MS)
    t.fn()
    expect(fails).toHaveLength(1)
    expect(rx.busy).toBe(false)
  })

  test('only snap messages are claimed', () => {
    const { rx } = rig()
    expect(rx.handle({ t: 'snap' })).toBe(false)
    expect(rx.handle({ t: 'idle', ends_in: 0 })).toBe(false)
    expect(rx.handle(result())).toBe(true)
  })
})

test('snapFileName', () => {
  expect(snapFileName(new Date(2026, 10, 11, 9, 5, 3))).toBe('yui-snap-20261111-090503.jpg')
})
