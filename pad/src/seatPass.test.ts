import { describe, expect, test } from 'bun:test'
import type { SeatMate } from '@yui/protocol'
import { emptySeat, resultText, seatExpire, seatReduce, secondsLeft, spectatorsOf } from './seatPass'

const ana: SeatMate = { id: '1', name: 'Ana', color: 'blue' }
const dee: SeatMate = { id: '2', name: 'Dee', color: 'red' }

describe('seatReduce', () => {
  test('an offer opens the recipient card for expires_in seconds', () => {
    const s = seatReduce(emptySeat, { t: 'seat_offered', from: ana, slot: 1, expires_in: 20 }, 1000)
    expect(s.incoming).toEqual({ from: ana, slot: 1, until: 21000 })
  })

  test('pending then a final result drives the giver', () => {
    let s = seatReduce(emptySeat, { t: 'seat_result', result: 'pending', role: 'giver', slot: 1, other: dee, expires_in: 20 }, 0)
    expect(s.outgoing?.to).toBe(dee)
    s = seatReduce(s, { t: 'seat_result', result: 'declined', role: 'giver', slot: 1, other: dee }, 5000)
    expect(s.outgoing).toBeNull()
    expect(s.result?.result).toBe('declined')
    expect(resultText(s.result!)).toBe('Dee said no thanks.')
  })

  test('a final result closes the recipient card without a giver message', () => {
    let s = seatReduce(emptySeat, { t: 'seat_offered', from: ana, slot: 1, expires_in: 20 }, 0)
    s = seatReduce(s, { t: 'seat_result', result: 'expired', role: 'recipient', slot: 1, other: ana }, 20000)
    expect(s).toEqual(emptySeat)
  })

  test('ignores unrelated messages', () => {
    expect(seatReduce(emptySeat, { t: 'seat_cancel' }, 0)).toBe(emptySeat)
  })
})

describe('seatExpire', () => {
  test('drops lapsed offers and tells the giver', () => {
    let s = seatReduce(emptySeat, { t: 'seat_result', result: 'pending', role: 'giver', slot: 1, other: dee, expires_in: 20 }, 0)
    expect(seatExpire(s, 19000)).toBe(s)
    s = seatExpire(s, 20000)
    expect(s.outgoing).toBeNull()
    expect(s.result?.result).toBe('expired')
    const r = seatExpire(seatReduce(emptySeat, { t: 'seat_offered', from: ana, slot: 1, expires_in: 5 }, 0), 5000)
    expect(r.incoming).toBeNull()
  })
})

describe('helpers', () => {
  test('spectatorsOf keeps only seatless players', () => {
    const roster = [
      { id: '2', name: 'Dee', color: 'red', slot: null, pokes: true },
      { id: '3', name: 'Bo', color: 'green', slot: 2, pokes: true },
    ]
    expect(spectatorsOf(roster).map((p) => p.id)).toEqual(['2'])
  })
  test('secondsLeft rounds up and floors at 0', () => {
    expect(secondsLeft(10_500, 10_000)).toBe(1)
    expect(secondsLeft(10_000, 11_000)).toBe(0)
  })
})
