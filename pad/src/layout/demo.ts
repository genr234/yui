import type { Node } from '@yui/protocol'

/** Dev-only preview tree (`?demo=layout`, `?demo=gamepad`, `?demo=remote`): every control type once. */
export const demoTree: Node = {
  type: 'column',
  props: { gap: 14 },
  children: [
    { type: 'text', props: { text: 'Layout demo', size: 'large' } },
    {
      type: 'row',
      children: [
        { type: 'button', props: { id: 'minus', label: '−', variant: 'secondary' } },
        { type: 'button', props: { id: 'plus', label: '+', variant: 'secondary' } },
        { type: 'button', props: { id: 'jump', label: 'Jump', bit: 0 } },
      ],
    },
    {
      type: 'choice',
      props: {
        id: 'team',
        options: [
          { id: 'red', label: 'Red', color: '#d84a4a' },
          { id: 'blue', label: 'Blue', color: '#2f76dc' },
        ],
      },
    },
    { type: 'keyboard', props: { id: 'kb', mode: 'field', placeholder: 'Say something', submit_label: 'Send' } },
    { type: 'keyboard', props: { id: 'live' } },
    { type: 'trackpad', props: { id: 'tp' } },
    {
      type: 'row',
      props: { flex: 1 },
      children: [
        { type: 'stick', props: { id: 'ls', side: 'left', click_bit: 11 } },
        { type: 'dpad', props: { id: 'dp' } },
      ],
    },
    {
      type: 'list',
      props: {
        id: 'games',
        columns: 3,
        items: [
          { id: 'g1', title: 'First game', subtitle: '2 players' },
          { id: 'g2', title: 'Second game' },
          { id: 'g3', title: 'Third game' },
        ],
      },
    },
  ],
}

export const demoGamepad: Node = { type: 'preset', props: { name: 'gamepad' } }

/** The kiosk's shell remote (controller/internal/pad/shell Remote(true)). */
export const demoShell: Node = {
  type: 'column',
  props: { gap: 12 },
  children: [
    { type: 'trackpad', props: { id: 'trackpad', mode: 'relative', label: 'Drag to move, tap to click, two fingers to scroll' } },
    { type: 'keyboard', props: { id: 'keyboard', mode: 'live', placeholder: 'Type on the screen' } },
    {
      type: 'row',
      props: { gap: 12 },
      children: [
        { type: 'button', props: { id: 'back', label: 'Back', variant: 'secondary' } },
        { type: 'button', props: { id: 'home', label: 'Home', variant: 'secondary' } },
        { type: 'button', props: { id: 'open_yui', label: 'Open Yui', variant: 'primary' } },
      ],
    },
    { type: 'button', props: { id: 'keyboard_toggle', label: 'Hide keyboard', variant: 'ghost' } },
  ],
}

export const demoRemote: Node = {
  type: 'preset',
  props: { name: 'motion-remote', labels: { South: 'A', R2: 'B', West: '1', North: '2', Select: '−', Start: '+' } },
}
