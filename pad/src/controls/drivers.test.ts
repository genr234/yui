import { describe, expect, test } from 'bun:test'
import { Buttons, decodeInput } from '@yui/protocol'
import {
  ButtonDriver,
  CLICK_MS,
  DpadDriver,
  LiveKeyboard,
  SENTINEL,
  StickDriver,
  TrackpadDriver,
  diffText,
  dpadDirs,
  stickAxes,
} from './drivers'
import { validBit } from './sink'
import { safeImage } from './context'
import { InputLoop } from '../input/loop'
import { recordingSink } from '../testing'

describe('button', () => {
  test('without bit: press, release, tap on a short press', () => {
    const r = recordingSink()
    const b = new ButtonDriver(r.sink, 'ok')
    b.down(1)
    r.clock.advance(100)
    b.up(1)
    expect(r.sent).toEqual([
      { t: 'event', control: 'ok', kind: 'press' },
      { t: 'event', control: 'ok', kind: 'release' },
      { t: 'event', control: 'ok', kind: 'tap' },
    ])
    expect(r.feedbackCount()).toBe(1)
  })

  test('long press has no tap; cancel has no tap', () => {
    const r = recordingSink()
    const b = new ButtonDriver(r.sink, 'ok')
    b.down(1)
    r.clock.advance(800)
    b.up(1)
    b.down(2)
    b.up(2, true)
    expect(r.sent.map((s) => (s.t === 'event' ? s.kind : ''))).toEqual(['press', 'release', 'press', 'release'])
  })

  test('two fingers on one button are one press', () => {
    const r = recordingSink()
    const b = new ButtonDriver(r.sink, 'ok')
    b.down(1)
    b.down(2)
    b.up(1)
    expect(r.sent.length).toBe(1)
    b.up(2)
    expect(r.sent.length).toBe(3)
  })

  test('with bit: held in the frame, no events', () => {
    const r = recordingSink()
    const frames: ArrayBuffer[] = []
    new InputLoop(r.input, (f) => frames.push(f), r.clock)
    const b = new ButtonDriver(r.sink, 'jump', 1 << 0)
    b.down(1)
    r.clock.advance(50)
    b.up(1)
    expect(r.sent).toEqual([])
    const d = frames.map((f) => decodeInput(f))
    expect(d[0].buttons).toBe(Buttons.South)
    expect(d.at(-1)!.buttons).toBe(0)
  })

  test('valid bits exclude Home and Recenter', () => {
    expect(validBit(0)).toBe(0)
    expect(validBit(31)).toBe(31)
    expect(validBit(10)).toBeNull()
    expect(validBit(17)).toBeNull()
    expect(validBit(32)).toBeNull()
    expect(validBit(1.5)).toBeNull()
    expect(validBit('3')).toBeNull()
  })

  test('stick + button at once (multi-touch) both land in one frame', () => {
    const r = recordingSink()
    const frames: ArrayBuffer[] = []
    new InputLoop(r.input, (f) => frames.push(f), r.clock)
    const stick = new StickDriver(r.sink, 'ls', 'left', null, 50)
    const a = new ButtonDriver(r.sink, 'a', Buttons.South)
    stick.down(1, 100, 100)
    stick.move(1, 150, 100)
    a.down(2)
    r.clock.advance(20)
    const d = decodeInput(frames.at(-1)!)
    expect(d.buttons).toBe(Buttons.South)
    expect(d.lx).toBe(32767)
  })
})

describe('dpad', () => {
  test('8-way sectors and dead zone (screen coords, +y down)', () => {
    expect(dpadDirs(0, 0, true)).toEqual([])
    expect(dpadDirs(0, -1, true)).toEqual(['up'])
    expect(dpadDirs(1, 1, true)).toEqual(['down', 'right'])
    expect(dpadDirs(-1, -1, true)).toEqual(['up', 'left'])
    expect(dpadDirs(-1, -0.9, false)).toEqual(['left'])
  })

  test('bits mode holds bits 13–16 and releases on lift', () => {
    const r = recordingSink()
    const d = new DpadDriver(r.sink, 'dp')
    d.down(1, 0.9, -0.9)
    expect(r.input.buttons()).toBe(Buttons.Up | Buttons.Right)
    d.move(1, 0, 0.9)
    expect(r.input.buttons()).toBe(Buttons.Down)
    d.up(1)
    expect(r.input.buttons()).toBe(0)
    expect(r.sent).toEqual([])
  })

  test('events mode: 4-way press/release with direction values', () => {
    const r = recordingSink()
    const d = new DpadDriver(r.sink, 'menu', 'events')
    d.down(1, 0, -1)
    d.move(1, 1, 0)
    d.up(1)
    expect(r.sent).toEqual([
      { t: 'event', control: 'menu', kind: 'press', value: 'up' },
      { t: 'event', control: 'menu', kind: 'release', value: 'up' },
      { t: 'event', control: 'menu', kind: 'press', value: 'right' },
      { t: 'event', control: 'menu', kind: 'release', value: 'right' },
    ])
  })

  test('rotate 90 reports the sideways remote frame, dirs filter', () => {
    const r = recordingSink()
    const d = new DpadDriver(r.sink, 'dp', 'bits', ['up', 'down', 'left', 'right'], 90)
    d.down(1, 0, -1) // screen up
    expect(r.input.buttons()).toBe(Buttons.Right)
    d.move(1, -1, 0) // screen left
    expect(r.input.buttons()).toBe(Buttons.Up)
    const r2 = recordingSink()
    const d2 = new DpadDriver(r2.sink, 'dp', 'bits', ['left', 'right'])
    d2.down(1, 0, -1)
    expect(r2.input.buttons()).toBe(0)
  })
})

describe('stick', () => {
  test('axes: dead zone, clamp, +y up', () => {
    expect(stickAxes(2, 2, 60)).toEqual([0, 0])
    const [x, y] = stickAxes(0, -120, 60)
    expect(x).toBeCloseTo(0)
    expect(y).toBeCloseTo(1)
    const [dx] = stickAxes(30, 0, 60)
    expect(dx).toBeGreaterThan(0.4)
    expect(dx).toBeLessThan(0.5)
  })

  test('floating centre, side, centre on release', () => {
    const r = recordingSink()
    const s = new StickDriver(r.sink, 'rs', 'right', null, 50)
    s.down(7, 200, 200)
    s.move(7, 200, 150)
    let f = r.input.frame()
    expect([f.rx, f.ry]).toEqual([0, 32767])
    expect(f.lx).toBe(0)
    s.move(8, 0, 0) // another finger is ignored
    s.up(7)
    f = r.input.frame()
    expect([f.rx, f.ry]).toEqual([0, 0])
    expect(r.input.touching()).toBe(false)
  })

  test('click_bit: a quick tap pulses the bit', () => {
    const r = recordingSink()
    const s = new StickDriver(r.sink, 'ls', 'left', 11, 50)
    s.down(1, 10, 10)
    r.clock.advance(80)
    s.up(1)
    expect(r.input.buttons()).toBe(Buttons.L3)
    r.clock.advance(CLICK_MS + 1)
    expect(r.input.buttons()).toBe(0)
    // a drag is not a click
    s.down(2, 10, 10)
    s.move(2, 60, 10)
    s.up(2)
    expect(r.input.buttons()).toBe(0)
  })
})

describe('trackpad', () => {
  test('relative: change events in whole px, batched to one per frame', () => {
    const r = recordingSink()
    const t = new TrackpadDriver(r.sink, 'tp')
    t.down(1, 0, 0)
    r.clock.advance(100)
    t.move(1, 3.6, 1) // first move flushes at once
    t.move(1, 6, 2) // within 16 ms: batched
    t.move(1, 20, 4)
    expect(r.sent).toEqual([{ t: 'event', control: 'tp', kind: 'change', value: { dx: 3, dy: 1, fingers: 1 } }])
    r.clock.advance(20)
    expect(r.sent[1]).toEqual({ t: 'event', control: 'tp', kind: 'change', value: { dx: 17, dy: 3, fingers: 1 } })
    t.up(1)
    expect(r.sent.some((s) => s.t === 'event' && s.kind === 'tap')).toBe(false)
  })

  test('relative: two-finger scroll averages the fingers', () => {
    const r = recordingSink()
    const t = new TrackpadDriver(r.sink, 'tp')
    t.down(1, 0, 0)
    t.down(2, 50, 0)
    r.clock.advance(50)
    t.move(1, 0, 20)
    t.move(2, 50, 20)
    r.clock.advance(20)
    const changes = r.sent.filter((s) => s.t === 'event' && s.kind === 'change')
    const total = changes.reduce((a, s) => a + ((s as any).value.dy as number), 0)
    expect(total).toBe(20)
    expect(changes.every((s) => (s as any).value.fingers === 2)).toBe(true)
  })

  test('relative: taps report finger count', () => {
    const r = recordingSink()
    const t = new TrackpadDriver(r.sink, 'tp')
    t.down(1, 0, 0)
    t.down(2, 30, 0)
    r.clock.advance(80)
    t.up(1)
    t.up(2)
    expect(r.sent).toEqual([{ t: 'event', control: 'tp', kind: 'tap', value: { fingers: 2 } }])
  })

  test('absolute: pointer in the frame while touched, press/release', () => {
    const r = recordingSink()
    const t = new TrackpadDriver(r.sink, 'aim', 'absolute')
    t.down(1, 0, 0, -1, -1)
    expect([r.input.frame().px, r.input.frame().py]).toEqual([-32767, -32767])
    t.move(1, 5, 5, 0.5, 0)
    expect([r.input.frame().px, r.input.frame().py]).toEqual([16384, 0])
    t.up(1)
    expect([r.input.frame().px, r.input.frame().py]).toEqual([0, 0])
    expect(r.sent.map((s) => (s.t === 'event' ? s.kind : ''))).toEqual(['press', 'release'])
  })
})

describe('keyboard', () => {
  test('diffText: backspaces then insert', () => {
    expect(diffText('helo', 'hello')).toEqual([{ op: 'backspace' }, { op: 'insert', text: 'lo' }])
    expect(diffText('abc', 'ab')).toEqual([{ op: 'backspace' }])
    expect(diffText('', '👍')).toEqual([{ op: 'insert', text: '👍' }])
    expect(diffText('a👍', 'a')).toEqual([{ op: 'backspace' }])
  })

  test('live: typing, autocorrect, backspace on empty, enter', () => {
    const r = recordingSink()
    const k = new LiveKeyboard(r.sink)
    let v = k.input(SENTINEL + 'h')
    v = k.input(v + 'i')
    v = k.input(SENTINEL + 'Hi ') // autocorrect rewrote the word
    v = k.enter()
    expect(v).toBe(SENTINEL)
    k.input('') // Backspace deleted the sentinel
    expect(r.sent).toEqual([
      { t: 'text', op: 'insert', text: 'h' },
      { t: 'text', op: 'insert', text: 'i' },
      { t: 'text', op: 'backspace' },
      { t: 'text', op: 'backspace' },
      { t: 'text', op: 'insert', text: 'Hi ' },
      { t: 'text', op: 'enter' },
      { t: 'text', op: 'backspace' },
    ])
  })
})

describe('images', () => {
  test('only data: images and this server', () => {
    const origin = 'https://yui.example'
    expect(safeImage('data:image/png;base64,AAAA', origin)).toBe('data:image/png;base64,AAAA')
    expect(safeImage('/covers/RMCE01.png', origin)).toBe('https://yui.example/covers/RMCE01.png')
    expect(safeImage('https://evil.example/x.png', origin)).toBeNull()
    expect(safeImage('javascript:alert(1)', origin)).toBeNull()
    expect(safeImage(42, origin)).toBeNull()
  })
})
