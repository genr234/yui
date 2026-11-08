import type { Node } from '@yui/protocol'

export const demoTree: Node = {
  type: 'column',
  props: { gap: 16 },
  children: [
    { type: 'text', props: { text: 'Layout demo', size: 'large' } },
    { type: 'text', props: { text: 'Hold a button; press and release events are logged below.' } },
    {
      type: 'row',
      children: [
        { type: 'button', props: { id: 'minus', label: '-', variant: 'secondary' } },
        { type: 'button', props: { id: 'plus', label: '+', variant: 'secondary' } },
      ],
    },
    { type: 'spacer', props: { flex: 1 } },
    {
      type: 'grid',
      props: { columns: 3 },
      children: [
        { type: 'button', props: { id: 'one', label: '1' } },
        { type: 'button', props: { id: 'two', label: '2' } },
        { type: 'button', props: { id: 'a', label: 'A' } },
        { type: 'button', props: { id: 'b', label: 'B' } },
        { type: 'button', props: { id: 'z', label: 'Z' } },
        { type: 'button', props: { id: 'c', label: 'C' } },
      ],
    },
    { type: 'button', props: { id: 'home', label: 'Home', variant: 'danger' } },
  ],
}
