import { describe, expect, test } from 'bun:test'
import { parseControl } from '@yui/protocol'
import { InputStats, MAX_DEAD_AT, layoutName } from './stats'

describe('input stats', () => {
  test('a window holds counts and rounded dead touches, and take() starts a new one', () => {
    let t = 1000
    const s = new InputStats(() => t)
    s.setContext('gamepad:portrait', true)
    expect(s.take()).toBeNull()
    s.count('touches')
    s.count('touches')
    s.dead(0.123, 1.7)
    t += 30_000
    const msg = s.take()
    expect(msg).toEqual({
      t: 'input_stats',
      window_ms: 30_000,
      layout: 'gamepad:portrait',
      assist: true,
      counts: { touches: 2, dead: 1 },
      dead_at: [[0.12, 1]],
    })
    // What the pad sends is what the kiosk accepts.
    expect(() => parseControl(JSON.stringify(msg))).not.toThrow()
    expect(s.take()).toBeNull()
  })

  test('changing layout or assist closes the window', () => {
    const s = new InputStats(() => 0)
    expect(s.setContext('gamepad:portrait', true)).toBeNull()
    s.count('press')
    const done = s.setContext('gamepad:portrait', false)
    expect(done?.assist).toBe(true)
    expect(done?.counts).toEqual({ press: 1 })
    expect(s.setContext('gamepad:portrait', false)).toBeNull()
    expect(s.empty).toBe(true)
  })

  test('dead positions are capped', () => {
    const s = new InputStats(() => 0)
    for (let i = 0; i < MAX_DEAD_AT + 10; i++) s.dead(0.5, 0.5)
    const msg = s.take()
    expect(msg?.dead_at?.length).toBe(MAX_DEAD_AT)
    expect(msg?.counts.dead).toBe(MAX_DEAD_AT + 10)
  })

  test('layout names', () => {
    expect(layoutName(null, false)).toBe('none:portrait')
    expect(layoutName({ type: 'preset', props: { name: 'gamepad' } }, true)).toBe('gamepad:landscape')
    expect(layoutName({ type: 'column', children: [{ type: 'text' }, { type: 'preset', props: { name: 'motion-remote' } }] }, false)).toBe(
      'motion-remote:portrait',
    )
    expect(layoutName({ type: 'column', children: [{ type: 'button' }] }, false)).toBe('custom:portrait')
  })
})
