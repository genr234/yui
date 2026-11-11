// Pass your seat: the phone side of the seat_* messages (contracts §3).
// Pure state so it can be tested without a browser; Pad.svelte owns the timers.
import type { ControlMessage, Peer, SeatMate, SeatOffered, SeatResult } from '@yui/protocol'

/** Nothing is waiting on this phone. */
export interface SeatState {
  /** An offer to this phone (the recipient's card), until `until` (ms clock). */
  incoming: { from: SeatMate; slot: number; until: number } | null
  /** An offer this phone made (the giver's "Waiting for Dee…"), until `until`. */
  outgoing: { to: SeatMate; slot: number; until: number } | null
  /** The last final result, shown once to the giver (the recipient just sees their seat change). */
  result: { result: SeatResult['result']; role: 'giver' | 'recipient'; other?: SeatMate; reason?: string } | null
}

export const emptySeat: SeatState = { incoming: null, outgoing: null, result: null }

/** Applies one kiosk message. `now` is a ms clock; returns the same object when nothing changed. */
export function seatReduce(state: SeatState, msg: ControlMessage, now: number): SeatState {
  if (msg.t === 'seat_offered') return offered(state, msg, now)
  if (msg.t !== 'seat_result') return state
  if (msg.result === 'pending') {
    if (msg.role !== 'giver' || !msg.other) return state
    return { ...state, result: null, outgoing: { to: msg.other, slot: msg.slot, until: now + (msg.expires_in ?? 20) * 1000 } }
  }
  // A final result closes whichever side this phone was on.
  if (msg.role === 'recipient') {
    // Accepting a seat shows no giver-style result: the seat label changes by itself.
    return { ...state, incoming: null, result: msg.result === 'accepted' ? null : state.result }
  }
  return { ...state, outgoing: null, result: { result: msg.result, role: 'giver', other: msg.other, reason: msg.reason } }
}

function offered(state: SeatState, msg: SeatOffered, now: number): SeatState {
  return { ...state, incoming: { from: msg.from, slot: msg.slot, until: now + msg.expires_in * 1000 } }
}

/** Drops an offer whose time ran out (the kiosk also says so; this covers a lost message). */
export function seatExpire(state: SeatState, now: number): SeatState {
  const incoming = state.incoming && state.incoming.until <= now ? null : state.incoming
  const lapsed = state.outgoing && state.outgoing.until <= now
  if (incoming === state.incoming && !lapsed) return state
  return {
    ...state,
    incoming,
    outgoing: lapsed ? null : state.outgoing,
    result: lapsed ? { result: 'expired', role: 'giver', other: state.outgoing?.to } : state.result,
  }
}

/** Other players with no seat (the kiosk only lists ready ones). */
export function spectatorsOf(players: Peer[]): Peer[] {
  return players.filter((p) => p.slot === null || p.slot === undefined)
}

/** Whole seconds left, never negative. */
export function secondsLeft(until: number, now: number): number {
  return Math.max(0, Math.ceil((until - now) / 1000))
}

/** One line for the giver once an offer ended. Empty when there is nothing to say. */
export function resultText(r: NonNullable<SeatState['result']>): string {
  const who = r.other?.name || 'They'
  switch (r.result) {
    case 'accepted':
      return `${who} took your seat.`
    case 'declined':
      return `${who} said no thanks.`
    case 'expired':
      return `${who} didn't answer in time.`
    case 'cancelled':
      return r.reason === 'left_or_changed' ? 'The offer was called off.' : 'Offer cancelled.'
    case 'failed':
      return r.reason === 'busy' ? `${who} already has an offer waiting.` : "The seat couldn't be passed. Try again."
    default:
      return ''
  }
}
