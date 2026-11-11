import { expect, test } from 'bun:test'
import { EMOTES, parseControl } from '@yui/protocol'
import { EMOTE_LABELS, pokedText } from './social'

test('every emote has a label', () => {
  for (const id of EMOTES) expect(EMOTE_LABELS[id]).toBeTruthy()
  expect(EMOTES.length).toBe(16)
})

test('pokedText names the poker when known', () => {
  const peers = [{ id: '8', name: 'Bo', color: 'red', slot: 2, pokes: true }]
  expect(pokedText(peers, '8')).toBe('Bo poked you')
  expect(pokedText(peers, '9')).toBe('Someone poked you')
})

test('the messages the pad sends parse', () => {
  expect(parseControl(JSON.stringify({ t: 'react', emote: 'heart' })).t).toBe('react')
  expect(parseControl(JSON.stringify({ t: 'poke', to: '8' })).t).toBe('poke')
  expect(parseControl(JSON.stringify({ t: 'system', action: 'pokes', value: false })).t).toBe('system')
  expect(() => parseControl(JSON.stringify({ t: 'react', emote: 'nope' }))).toThrow()
})
