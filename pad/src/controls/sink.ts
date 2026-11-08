import type { EventKind, TextOp } from '@yui/protocol'
import type { InputModel } from '../input/model'

/** Where controls send their output: control-channel messages and the input model. */
export interface ControlSink {
  event(control: string, kind: EventKind, value?: unknown): void
  text(op: TextOp, text?: string): void
  input: InputModel
  /** Short local press feedback (vibration / iOS switch tick). */
  feedback(): void
  now(): number
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(handle: unknown): void
}

/** Bit numbers (contracts §3). */
export const BIT = {
  South: 0,
  East: 1,
  West: 2,
  North: 3,
  L1: 4,
  R1: 5,
  L2: 6,
  R2: 7,
  Select: 8,
  Start: 9,
  Home: 10,
  L3: 11,
  R3: 12,
  Up: 13,
  Down: 14,
  Left: 15,
  Right: 16,
  Recenter: 17,
} as const

export type ButtonName = keyof typeof BIT

/** A layout may put a button on 0–31 except Home (10) and Recenter (17), which the pad owns. */
export function validBit(bit: unknown): number | null {
  if (typeof bit !== 'number' || !Number.isInteger(bit)) return null
  if (bit < 0 || bit > 31 || bit === BIT.Home || bit === BIT.Recenter) return null
  return bit
}
