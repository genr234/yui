import { expect, test } from 'bun:test'
import { guideEntries, hasButtons } from './guide'
import { expandTree } from './presets'

const ctx = { landscape: false, motionReady: true }

test('preset actions become hints and show in the guide', () => {
  const tree = expandTree(
    { type: 'preset', props: { name: 'gamepad', actions: { South: 'Accelerate', East: 'Drift', Dpad: 'Steer' }, hide: ['L2', 'R2'] } },
    ctx,
  )
  const g = guideEntries(tree)
  expect(g.find((e) => e.id === 'preset.South')).toMatchObject({ cap: 'A', text: 'Accelerate' })
  expect(g.find((e) => e.id === 'preset.East')?.text).toBe('Drift')
  expect(g.find((e) => e.kind === 'dpad')?.text).toBe('Steer')
  expect(g.find((e) => e.id === 'preset.West')?.text).toBe('Left face button')
  expect(g.some((e) => e.id === 'preset.L2')).toBe(false)
  expect(hasButtons(g)).toBe(true)
})

test('keymap-style buttons are described by their label; hint wins', () => {
  const g = guideEntries({
    type: 'column',
    children: [
      { type: 'button', props: { id: 'a', label: 'Move forward' } },
      { type: 'button', props: { id: 'b', label: 'X', hint: 'Cast spell' } },
    ],
  })
  expect(g).toEqual([
    { id: 'a', kind: 'button', cap: '', text: 'Move forward' },
    { id: 'b', kind: 'button', cap: 'X', text: 'Cast spell' },
  ])
})

test('no buttons, no guide', () => {
  expect(hasButtons(guideEntries({ type: 'column', children: [{ type: 'trackpad', props: { id: 't' } }] }))).toBe(false)
  expect(hasButtons(guideEntries(null))).toBe(false)
})
