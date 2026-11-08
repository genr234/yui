import { describe, expect, test } from 'bun:test'
import type { Node } from '@yui/protocol'
import { Buttons } from '@yui/protocol'
import { expandPreset, expandTree, scanTree, wantsMotion } from './presets'

function collect(n: Node | null, out: Node[] = []): Node[] {
  if (!n) return out
  out.push(n)
  for (const c of n.children ?? []) collect(c, out)
  return out
}
const buttons = (n: Node | null) =>
  Object.fromEntries(
    collect(n)
      .filter((x) => x.type === 'button')
      .map((x) => [x.props!.label as string, x.props!.bit as number]),
  )
const types = (n: Node | null) => collect(n).map((x) => x.type)
const ids = (n: Node | null) => collect(n).map((x) => x.props?.id).filter(Boolean)

const portrait = { landscape: false, motionReady: true }
const landscape = { landscape: true, motionReady: true }

describe('gamepad preset', () => {
  test('stick, d-pad, face, shoulders, Select/Start with bits', () => {
    for (const ctx of [portrait, landscape]) {
      const n = expandPreset({ name: 'gamepad' }, ctx)
      const b = buttons(n)
      expect(b).toEqual({ A: 0, B: 1, X: 2, Y: 3, L1: 4, R1: 5, L2: 6, R2: 7, Select: 8, Start: 9 })
      expect(types(n)).toContain('stick')
      expect(types(n)).toContain('dpad')
      const stick = collect(n).find((x) => x.type === 'stick')!
      expect(stick.props).toMatchObject({ side: 'left', click_bit: 11 })
    }
  })

  test('labels and hide', () => {
    const n = expandPreset({ name: 'gamepad', labels: { South: 'Jump' }, hide: ['L2', 'R2', 'Up', 'Down', 'Left', 'Right'] }, portrait)
    const b = buttons(n)
    expect(b.Jump).toBe(0)
    expect('L2' in b || 'R2' in b).toBe(false)
    expect(types(n)).not.toContain('dpad')
  })

  test('ids are unique', () => {
    const all = ids(expandPreset({ name: 'gamepad', id: 'p1' }, portrait))
    expect(new Set(all).size).toBe(all.length)
    expect(all.every((i) => String(i).startsWith('p1.'))).toBe(true)
  })
})

describe('motion-remote preset', () => {
  const labels = { South: 'A', R2: 'B', West: '1', North: '2', Select: '−', Start: '+' }

  test('upright: pointer motion + A, B, d-pad, 1, 2, −, +', () => {
    const n = expandPreset({ name: 'motion-remote', labels }, portrait)
    expect(buttons(n)).toEqual({ A: 0, B: 7, '1': 2, '2': 3, '−': 8, '+': 9 })
    const motion = collect(n).filter((x) => x.type === 'motion')
    expect(motion.length).toBe(1)
    expect(motion[0].props!.pointer).toBe(true)
    const dpad = collect(n).find((x) => x.type === 'dpad')!
    expect(dpad.props!.rotate).toBeUndefined()
  })

  test('sideways: d-pad (remote frame) + 1 + 2, motion without pointer', () => {
    const n = expandPreset({ name: 'motion-remote', labels }, landscape)
    expect(buttons(n)).toEqual({ '1': 2, '2': 3 })
    expect(collect(n).find((x) => x.type === 'dpad')!.props!.rotate).toBe(90)
    expect(scanTree(n)).toEqual({ motion: true, pointer: false })
  })

  test('no gyro: touch aim (absolute trackpad) replaces the pointer', () => {
    const n = expandPreset({ name: 'motion-remote', labels }, { landscape: false, motionReady: false })
    const tp = collect(n).find((x) => x.type === 'trackpad')!
    expect(tp.props!.mode).toBe('absolute')
    expect(scanTree(n).pointer).toBe(false)
  })

  test('Home and Recenter never appear as layout bits', () => {
    for (const ctx of [portrait, landscape]) {
      const bits = Object.values(buttons(expandPreset({ name: 'motion-remote' }, ctx)))
      expect(bits).not.toContain(10)
      expect(bits).not.toContain(17)
      expect(bits.every((b) => (1 << b) & 0x3ffff & ~Buttons.Home)).toBe(true)
    }
  })
})

describe('tree helpers', () => {
  test('expandTree replaces nested presets, unknown presets vanish', () => {
    const tree: Node = {
      type: 'column',
      children: [
        { type: 'text', props: { text: 'hi' } },
        { type: 'preset', props: { name: 'gamepad' } },
        { type: 'preset', props: { name: 'nope' } },
      ],
    }
    const out = expandTree(tree, portrait)
    expect(types(out)).not.toContain('preset')
    expect(types(out)).toContain('stick')
  })

  test('wantsMotion sees motion in either orientation', () => {
    expect(wantsMotion({ type: 'preset', props: { name: 'motion-remote' } })).toBe(true)
    expect(wantsMotion({ type: 'preset', props: { name: 'gamepad' } })).toBe(false)
    expect(wantsMotion({ type: 'row', children: [{ type: 'motion', props: { id: 'm', pointer: true } }] })).toBe(true)
    expect(wantsMotion(null)).toBe(false)
  })
})
