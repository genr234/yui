import type { Node } from '@yui/protocol'

/*
 * The button guide: what the controls of the current (expanded) layout do.
 * Apps may name actions with `hint` on a node (presets pass `props.actions`
 * through as hints); without one, preset buttons get a position-based note
 * and keymap-style buttons are described by their own label.
 */

export interface GuideEntry {
  id: string
  kind: 'button' | 'dpad' | 'stick' | 'trackpad' | 'motion'
  /** Short text for the key cap (a button label), or '' to show only the description. */
  cap: string
  /** What it does. */
  text: string
}

const POSITION: Record<string, string> = {
  South: 'Bottom face button',
  East: 'Right face button',
  West: 'Left face button',
  North: 'Top face button',
  L1: 'Left shoulder',
  R1: 'Right shoulder',
  L2: 'Left trigger',
  R2: 'Right trigger',
  Select: 'Select',
  Start: 'Start',
}

const FIXED: Partial<Record<GuideEntry['kind'], string>> = {
  dpad: 'Direction pad',
  stick: 'Left stick',
  trackpad: 'Touchpad',
  motion: 'Tilt and point the phone',
}

function hint(n: Node): string {
  const h = n.props?.hint
  return typeof h === 'string' ? h.trim() : ''
}

export function guideEntries(root: Node | null): GuideEntry[] {
  const out: GuideEntry[] = []
  const seen = new Set<string>()
  const walk = (n: Node, depth: number) => {
    if (depth > 32) return
    const p = n.props ?? {}
    const id = typeof p.id === 'string' ? p.id : ''
    if (n.type === 'button') {
      const label = String(p.label ?? p.text ?? '').trim()
      const position = POSITION[id.split('.').pop() ?? '']
      if ((label || hint(n)) && !seen.has(id || label)) {
        seen.add(id || label)
        const short = label.length > 0 && label.length <= 4
        out.push({ id: id || label, kind: 'button', cap: short ? label : '', text: hint(n) || (short ? (position ?? '') : label) })
      }
    } else if (n.type === 'dpad' || n.type === 'stick' || n.type === 'trackpad' || n.type === 'motion') {
      if (!seen.has(id || n.type)) {
        seen.add(id || n.type)
        // A motion node with no hint and no pointer is just a sensor: nothing to explain.
        if (n.type !== 'motion' || hint(n) || p.pointer === true) {
          out.push({ id: id || n.type, kind: n.type, cap: '', text: hint(n) || FIXED[n.type] || '' })
        }
      }
    }
    for (const c of n.children ?? []) walk(c, depth + 1)
  }
  if (root) walk(root, 0)
  // Sticks and pads first, then buttons, each in layout order.
  return [...out.filter((e) => e.kind !== 'button'), ...out.filter((e) => e.kind === 'button')]
}

/** The guide only makes sense when the layout has buttons to explain. */
export const hasButtons = (entries: GuideEntry[]) => entries.some((e) => e.kind === 'button')
