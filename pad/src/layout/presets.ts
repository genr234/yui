import type { Node } from '@yui/protocol'
import { BIT, type ButtonName } from '../controls/sink'

/*
 * Layout presets (contracts §3 "preset"). The kiosk sends `{type:"preset",
 * props:{name, labels, hide}}`; the pad expands it here because only the pad
 * knows the phone's orientation. Every press goes into the input frame bits.
 *
 * Pad-internal props used by the expansion (also honoured if an app sends
 * them): button `shape` ("round" | "pill") and `size` ("small" | "large"),
 * d-pad `rotate` (90 = screen-up reports Right, for a remote held sideways)
 * and d-pad `dirs` (directions left after `hide`).
 */

export interface PresetContext {
  landscape: boolean
  /** False when the phone has no usable gyro: motion-remote aims by touch instead. */
  motionReady: boolean
}

export const DEFAULT_LABELS: Record<ButtonName, string> = {
  South: 'A',
  East: 'B',
  West: 'X',
  North: 'Y',
  L1: 'L1',
  R1: 'R1',
  L2: 'L2',
  R2: 'R2',
  Select: 'Select',
  Start: 'Start',
  Home: 'Home',
  L3: 'L3',
  R3: 'R3',
  Up: 'Up',
  Down: 'Down',
  Left: 'Left',
  Right: 'Right',
  Recenter: 'Recenter',
}

type Opts = { labels: Record<string, string>; hide: Set<string>; base: string }

function opts(props: Record<string, unknown>): Opts {
  const labels: Record<string, string> = {}
  if (props.labels && typeof props.labels === 'object' && !Array.isArray(props.labels)) {
    for (const [k, v] of Object.entries(props.labels as Record<string, unknown>)) {
      if (typeof v === 'string' || typeof v === 'number') labels[k] = String(v)
    }
  }
  const hide = new Set(Array.isArray(props.hide) ? props.hide.filter((x): x is string => typeof x === 'string') : [])
  const base = typeof props.id === 'string' && props.id ? props.id : 'preset'
  return { labels, hide, base }
}

const row = (children: (Node | null)[], props: Record<string, unknown> = {}): Node => ({
  type: 'row',
  props,
  children: children.filter((c): c is Node => c !== null),
})
const column = (children: (Node | null)[], props: Record<string, unknown> = {}): Node => ({
  type: 'column',
  props,
  children: children.filter((c): c is Node => c !== null),
})
const spacer = (flex = 1): Node => ({ type: 'spacer', props: { flex } })

function btn(o: Opts, name: ButtonName, extra: Record<string, unknown> = {}): Node | null {
  if (o.hide.has(name)) return null
  return {
    type: 'button',
    props: {
      id: `${o.base}.${name}`,
      label: o.labels[name] ?? DEFAULT_LABELS[name],
      bit: BIT[name],
      variant: 'secondary',
      ...extra,
    },
  }
}

function dpad(o: Opts, extra: Record<string, unknown> = {}): Node | null {
  const dirs = (['Up', 'Down', 'Left', 'Right'] as const).filter((d) => !o.hide.has(d)).map((d) => d.toLowerCase())
  if (!dirs.length) return null
  return { type: 'dpad', props: { id: `${o.base}.dpad`, mode: 'bits', dirs, ...extra } }
}

/** Four face buttons as a diamond (North top, South bottom). Nulls keep their cell empty. */
function face(o: Opts, size?: string): Node | null {
  const cell = (name: ButtonName) => btn(o, name, { shape: 'round', variant: 'primary', size }) ?? spacer()
  const any = (['North', 'West', 'East', 'South'] as const).some((n) => !o.hide.has(n))
  if (!any) return null
  return {
    type: 'grid',
    props: { columns: 3, gap: 6, square: true },
    children: [spacer(), cell('North'), spacer(), cell('West'), spacer(), cell('East'), spacer(), cell('South'), spacer()],
  }
}

function stick(o: Opts): Node {
  return { type: 'stick', props: { id: `${o.base}.stick`, side: 'left', click_bit: o.hide.has('L3') ? undefined : BIT.L3 } }
}

const small = { shape: 'pill', size: 'small' }

function gamepad(o: Opts, ctx: PresetContext): Node {
  const shouldersL = row([btn(o, 'L2', small), btn(o, 'L1', small)], { gap: 8 })
  const shouldersR = row([btn(o, 'R1', small), btn(o, 'R2', small)], { gap: 8 })
  const menu = row([btn(o, 'Select', small), btn(o, 'Start', small)], { gap: 8 })
  if (ctx.landscape) {
    return row(
      [
        column([shouldersL, row([stick(o), dpad(o)], { flex: 1, gap: 12 })], { flex: 3, gap: 10 }),
        column([spacer(), menu, spacer()], { flex: 1 }),
        column([shouldersR, row([face(o)], { flex: 1 })], { flex: 2, gap: 10 }),
      ],
      { flex: 1, gap: 16 },
    )
  }
  return column(
    [
      row([shouldersL, spacer(0.4), shouldersR], { gap: 8 }),
      menu,
      spacer(0.5),
      row([stick(o), face(o)], { flex: 1, gap: 12 }),
      row([dpad(o), spacer()], { flex: 1, gap: 12 }),
    ],
    { flex: 1, gap: 12 },
  )
}

function motionRemote(o: Opts, ctx: PresetContext): Node {
  if (ctx.landscape) {
    // Sideways: the remote's top is the screen's left edge. The d-pad reports
    // directions in the remote's own frame (rotate 90), like the motion axes.
    return row(
      [
        { type: 'motion', props: { id: `${o.base}.motion`, pointer: false } },
        column([spacer(), dpad(o, { rotate: 90 }), spacer()], { flex: 1 }),
        spacer(0.3),
        row([btn(o, 'West', { shape: 'round', size: 'large' }), btn(o, 'North', { shape: 'round', size: 'large' })], {
          flex: 1,
          gap: 20,
        }),
      ],
      { flex: 1, gap: 16 },
    )
  }
  const aim: Node = ctx.motionReady
    ? { type: 'motion', props: { id: `${o.base}.motion`, pointer: true } }
    : {
        type: 'column',
        props: { gap: 8 },
        children: [
          { type: 'motion', props: { id: `${o.base}.motion`, pointer: false } },
          { type: 'trackpad', props: { id: `${o.base}.aim`, mode: 'absolute', label: 'Touch to aim' } },
        ],
      }
  return column(
    [
      aim,
      row([dpad(o)], { flex: 1 }),
      row([btn(o, 'South', { shape: 'round', size: 'large', variant: 'primary' }), btn(o, 'R2', { size: 'large' })], { gap: 12 }),
      row([btn(o, 'Select', small), btn(o, 'Start', small)], { gap: 40 }),
      row([btn(o, 'West', { shape: 'round' }), btn(o, 'North', { shape: 'round' })], { gap: 24 }),
    ],
    { flex: 1, gap: 14 },
  )
}

export function expandPreset(props: Record<string, unknown>, ctx: PresetContext): Node | null {
  const o = opts(props)
  switch (props.name) {
    case 'gamepad':
      return gamepad(o, ctx)
    case 'motion-remote':
      return motionRemote(o, ctx)
    default:
      return null
  }
}

/** Replace every preset node with its expansion (unknown preset names render nothing). */
export function expandTree(node: Node, ctx: PresetContext): Node {
  if (node.type === 'preset') return expandPreset(node.props ?? {}, ctx) ?? { type: 'spacer', props: { flex: 0 } }
  if (!node.children?.length) return node
  return { ...node, children: node.children.map((c) => expandTree(c, ctx)) }
}

export interface TreeInfo {
  /** A `motion` node is present: stream motion and show the permission gate. */
  motion: boolean
  /** Some motion node asks for the pointer. */
  pointer: boolean
}

export function scanTree(node: Node | null): TreeInfo {
  const info: TreeInfo = { motion: false, pointer: false }
  const walk = (n: Node, depth: number) => {
    if (depth > 32) return
    if (n.type === 'motion') {
      info.motion = true
      if (n.props?.pointer === true) info.pointer = true
    }
    for (const c of n.children ?? []) walk(c, depth + 1)
  }
  if (node) walk(node, 0)
  return info
}

/** Does the raw (unexpanded) tree need motion in any orientation? Used for the permission gate. */
export function wantsMotion(node: Node | null): boolean {
  if (!node) return false
  const portrait = scanTree(expandTree(node, { landscape: false, motionReady: true }))
  const landscape = scanTree(expandTree(node, { landscape: true, motionReady: true }))
  return portrait.motion || landscape.motion
}
