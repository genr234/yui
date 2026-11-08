import { describe, expect, test } from 'bun:test'
import type { ControlMessage, Hello, Player } from '@yui/protocol'
import { CABLE_PROTOCOLS, CableClient, type SocketLike } from './cable'
import { FALLBACK_MS, PadSession, RENEGOTIATE_MS, type ChannelLike, type ConnState, type PeerLike } from './session'
import { FakeClock } from '../testing'

const IDENT = JSON.stringify({ channel: 'PlaySessionChannel' })

class FakeSocket implements SocketLike {
  readyState = 0
  sent: any[] = []
  closed = false
  onopen: ((ev: unknown) => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onclose: ((ev: unknown) => void) | null = null
  onerror: ((ev: unknown) => void) | null = null
  constructor(
    public url: string,
    public protocols: string[],
  ) {}
  send(data: string) {
    this.sent.push(JSON.parse(data))
  }
  close() {
    this.closed = true
    this.readyState = 3
  }
  // server side
  open() {
    this.readyState = 1
    this.onopen?.({})
    this.frame({ type: 'welcome' })
  }
  frame(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) })
  }
  confirm() {
    this.frame({ type: 'confirm_subscription', identifier: IDENT })
  }
  broadcast(message: unknown) {
    this.frame({ identifier: IDENT, message })
  }
  drop() {
    this.readyState = 3
    this.onclose?.({})
  }
  /** Signals the pad performed, decoded. */
  signals() {
    return this.sent.filter((c) => c.command === 'message').map((c) => JSON.parse(c.data))
  }
}

class FakeChannel implements ChannelLike {
  readyState = 'connecting'
  binaryType = 'blob'
  sent: (string | ArrayBuffer)[] = []
  onopen: ((ev: unknown) => void) | null = null
  onclose: ((ev: unknown) => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  constructor(public label: string) {}
  send(d: string | ArrayBuffer) {
    this.sent.push(d)
  }
  close() {
    this.readyState = 'closed'
  }
  open() {
    this.readyState = 'open'
    this.onopen?.({})
  }
  json() {
    return this.sent.filter((s): s is string => typeof s === 'string').map((s) => JSON.parse(s))
  }
}

class FakePeer implements PeerLike {
  ondatachannel: ((ev: { channel: ChannelLike }) => void) | null = null
  onicecandidate: ((ev: { candidate: { toJSON(): unknown } | null }) => void) | null = null
  oniceconnectionstatechange: ((ev: unknown) => void) | null = null
  onconnectionstatechange: ((ev: unknown) => void) | null = null
  iceConnectionState = 'new'
  connectionState = 'new'
  remote: any = null
  local: any = null
  candidates: unknown[] = []
  closed = false
  constructor(public config: unknown) {}
  async setRemoteDescription(d: any) {
    this.remote = d
  }
  async createAnswer() {
    return { type: 'answer', sdp: 'v=0 answer' }
  }
  async setLocalDescription(d: any) {
    this.local = d
  }
  async addIceCandidate(c: unknown) {
    this.candidates.push(c)
  }
  close() {
    this.closed = true
  }
  channel(label: string) {
    const ch = new FakeChannel(label)
    this.ondatachannel?.({ channel: ch })
    return ch
  }
  fail() {
    this.iceConnectionState = 'failed'
    this.oniceconnectionstatechange?.({})
  }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

const hello: Hello = {
  t: 'hello',
  v: 1,
  caps: { motion: true, haptics: true, wakeLock: false },
  screen: { w: 390, h: 844, orientation: 'portrait' },
}
const player: Player = { id: '7', slot: 2, name: 'Ana', color: 'red', host: false, connected: true }

function setup() {
  const clock = new FakeClock()
  const sockets: FakeSocket[] = []
  const peers: FakePeer[] = []
  const states: ConnState[] = []
  const controls: ControlMessage[] = []
  const players: Player[] = []
  const inputs: boolean[] = []
  let iceCalls = 0
  const session = new PadSession(
    {
      cable: (h) =>
        new CableClient(
          'wss://yui.example/cable',
          { channel: 'PlaySessionChannel' },
          h,
          (url, protocols) => {
            const s = new FakeSocket(url, protocols)
            sockets.push(s)
            return s
          },
          clock,
        ),
      peer: (config) => {
        const p = new FakePeer(config)
        peers.push(p)
        return p
      },
      ice: async () => {
        iceCalls++
        return [{ urls: ['stun:stun.example:3478'] }]
      },
      hello: () => hello,
      timers: clock,
    },
    {
      state: (s) => states.push(s),
      control: (m) => controls.push(m),
      player: (p) => players.push(p),
      input: (r) => inputs.push(r),
    },
  )
  const sock = () => sockets.at(-1)!
  const connect = () => {
    sock().open()
    sock().confirm()
  }
  const offer = async () => {
    sock().broadcast({ type: 'offer', data: { type: 'offer', sdp: 'v=0 offer' } })
    await flush()
    await flush()
  }
  return { clock, session, sockets, sock, peers, states, controls, players, inputs, connect, offer, iceCalls: () => iceCalls }
}

describe('CableClient', () => {
  test('subscribes after welcome with the actioncable protocols', () => {
    const s = setup()
    s.session.start()
    expect(s.sock().url).toBe('wss://yui.example/cable')
    expect(s.sock().protocols).toEqual(CABLE_PROTOCOLS)
    s.sock().open()
    expect(s.sock().sent[0]).toEqual({ command: 'subscribe', identifier: IDENT })
  })

  test('reconnects with backoff after a drop and re-sends the hello signal', () => {
    const s = setup()
    s.session.start()
    s.connect()
    expect(s.sock().signals()).toEqual([{ action: 'signal', type: 'hello', data: {} }])
    s.sock().drop()
    expect(s.states.at(-1)).toBe('reconnecting')
    expect(s.sockets.length).toBe(1)
    s.clock.advance(600)
    expect(s.sockets.length).toBe(2)
    s.connect()
    expect(s.sock().signals()).toEqual([{ action: 'signal', type: 'hello', data: {} }])
    expect(s.states.at(-1)).toBe('connecting')
  })

  test('a socket with no server pings is treated as dead', () => {
    const s = setup()
    s.session.start()
    s.connect()
    s.clock.advance(9000)
    expect(s.sockets[0].closed).toBe(true)
    expect(s.sockets.length).toBeGreaterThan(1)
  })

  test('ignores other identifiers and malformed frames', () => {
    const s = setup()
    s.session.start()
    s.connect()
    s.sock().frame({ identifier: '{"channel":"Other"}', message: { type: 'kicked' } })
    s.sock().onmessage?.({ data: 'not json' })
    expect(s.states.includes('kicked')).toBe(false)
  })
})

describe('PadSession signaling', () => {
  test('answers the kiosk offer and trickles ICE both ways', async () => {
    const s = setup()
    s.session.start()
    s.connect()
    // ICE before the offer is applied is queued
    s.sock().broadcast({ type: 'offer', data: { type: 'offer', sdp: 'v=0 offer' } })
    s.sock().broadcast({ type: 'ice', data: { candidate: 'candidate:1 1 udp 1 1.2.3.4 5 typ host', sdpMid: '0' } })
    await flush()
    await flush()
    const pc = s.peers[0]
    expect(pc.config).toEqual({ iceServers: [{ urls: ['stun:stun.example:3478'] }] })
    expect(pc.remote).toEqual({ type: 'offer', sdp: 'v=0 offer' })
    expect(pc.candidates.length).toBe(1)
    const answer = s.sock().signals().find((x) => x.type === 'answer')
    expect(answer).toEqual({ action: 'signal', type: 'answer', data: { type: 'answer', sdp: 'v=0 answer' } })

    pc.onicecandidate?.({ candidate: { toJSON: () => ({ candidate: 'candidate:2', sdpMid: '0', sdpMLineIndex: 0 }) } })
    pc.onicecandidate?.({ candidate: null })
    const ice = s.sock().signals().filter((x) => x.type === 'ice')
    expect(ice).toEqual([{ action: 'signal', type: 'ice', data: { candidate: 'candidate:2', sdpMid: '0', sdpMLineIndex: 0 } }])

    s.sock().broadcast({ type: 'ice', data: { candidate: 'candidate:3' } })
    await flush()
    expect(pc.candidates.length).toBe(2)
    s.sock().broadcast({ type: 'ice', data: { candidate: '' } }) // end-of-candidates
    await flush()
    expect(pc.candidates.length).toBe(2)
  })

  test('control opens → hello; ping → pong; other messages are passed on', async () => {
    const s = setup()
    s.session.start()
    s.connect()
    await s.offer()
    const pc = s.peers[0]
    const control = pc.channel('control')
    const input = pc.channel('input')
    control.open()
    input.open()
    expect(input.binaryType).toBe('arraybuffer')
    expect(s.states.at(-1)).toBe('online')
    expect(control.json()[0]).toEqual(hello)
    expect(s.inputs).toEqual([true])

    control.onmessage?.({ data: JSON.stringify({ t: 'ping', id: 4, ts: 99 }) })
    expect(control.json()[1]).toEqual({ t: 'pong', id: 4, ts: 99 })

    const welcome = { t: 'welcome', v: 1, player, kiosk_name: 'Lobby' }
    control.onmessage?.({ data: JSON.stringify(welcome) })
    control.onmessage?.({ data: JSON.stringify({ t: 'layout', version: 3, tree: { type: 'column' } }) })
    control.onmessage?.({ data: JSON.stringify({ t: 'system', action: 'sensitive', value: true }) })
    control.onmessage?.({ data: '{"t":"bogus"}' })
    expect(s.controls.map((c) => c.t)).toEqual(['welcome', 'layout', 'system'])

    expect(s.session.send({ t: 'event', control: 'a', kind: 'tap' })).toBe(true)
    expect(control.json().at(-1)).toEqual({ t: 'event', control: 'a', kind: 'tap' })
    const frame = new ArrayBuffer(48)
    expect(s.session.sendInput(frame)).toBe(true)
    expect(input.sent).toEqual([frame])
  })

  test('a new offer replaces the old peer (renegotiation)', async () => {
    const s = setup()
    s.session.start()
    s.connect()
    await s.offer()
    s.peers[0].channel('control').open()
    await s.offer()
    expect(s.peers.length).toBe(2)
    expect(s.peers[0].closed).toBe(true)
    expect(s.iceCalls()).toBe(1) // ICE config is cached
  })

  test('player_updated from the cable updates the player', () => {
    const s = setup()
    s.session.start()
    s.connect()
    s.sock().broadcast({ type: 'player_updated', player: { ...player, slot: 1 } })
    expect(s.players[0].slot).toBe(1)
  })

  test('kicked: clear end state, cable closed, no auto-rejoin', () => {
    const s = setup()
    s.session.start()
    s.connect()
    s.sock().broadcast({ type: 'kicked' })
    expect(s.states.at(-1)).toBe('kicked')
    expect(s.sock().closed).toBe(true)
    s.clock.advance(60_000)
    expect(s.sockets.length).toBe(1)
    expect(s.session.send({ t: 'system', action: 'home' })).toBe(false)
  })

  test('session_ended and disconnect reconnect:false end the session', () => {
    const a = setup()
    a.session.start()
    a.connect()
    a.sock().broadcast({ type: 'session_ended' })
    expect(a.states.at(-1)).toBe('ended')

    const b = setup()
    b.session.start()
    b.connect()
    b.sock().frame({ type: 'disconnect', reason: 'unauthorized', reconnect: false })
    expect(b.states.at(-1)).toBe('closed')
    b.clock.advance(60_000)
    expect(b.sockets.length).toBe(1)
  })

  test('rejected subscription ends the session', () => {
    const s = setup()
    s.session.start()
    s.sock().open()
    s.sock().frame({ type: 'reject_subscription', identifier: IDENT })
    expect(s.states.at(-1)).toBe('closed')
  })
})

describe('PadSession fallback', () => {
  test('ICE failure → control over the cable, hello sent there, periodic renegotiation', async () => {
    const s = setup()
    s.session.start()
    s.connect()
    await s.offer()
    s.peers[0].fail()
    expect(s.states.at(-1)).toBe('fallback')
    const ctl = s.sock().signals().filter((x) => x.type === 'control')
    expect(ctl).toEqual([{ action: 'signal', type: 'control', data: hello }])

    expect(s.session.send({ t: 'event', control: 'a', kind: 'press' })).toBe(true)
    expect(s.sock().signals().at(-1)).toEqual({
      action: 'signal',
      type: 'control',
      data: { t: 'event', control: 'a', kind: 'press' },
    })
    // Input frames never go over the cable.
    expect(s.session.sendInput(new ArrayBuffer(48))).toBe(false)

    // Kiosk control objects arrive as {type:"control", data}; pings are answered on the cable.
    s.sock().broadcast({ type: 'control', data: { t: 'ping', id: 9, ts: 1 } })
    expect(s.sock().signals().at(-1)).toEqual({ action: 'signal', type: 'control', data: { t: 'pong', id: 9, ts: 1 } })
    s.sock().broadcast({ type: 'control', data: { t: 'haptic', pattern: [20] } })
    expect(s.controls.at(-1)).toEqual({ t: 'haptic', pattern: [20] })

    const hellos = () => s.sock().signals().filter((x) => x.type === 'hello').length
    const before = hellos()
    for (let t = 0; t <= RENEGOTIATE_MS; t += 1000) {
      s.clock.advance(1000)
      s.sock().frame({ type: 'ping', message: t }) // the server pings every 3 s
    }
    expect(s.sockets.length).toBe(1)
    expect(hellos()).toBe(before + 1)
  })

  test('control channel that never opens → fallback; opening later leaves it', async () => {
    const s = setup()
    s.session.start()
    s.connect()
    await s.offer()
    const control = s.peers[0].channel('control')
    s.clock.advance(FALLBACK_MS + 1)
    expect(s.states.at(-1)).toBe('fallback')
    control.open()
    expect(s.states.at(-1)).toBe('online')
    s.session.send({ t: 'event', control: 'x', kind: 'tap' })
    expect(control.json().at(-1)).toEqual({ t: 'event', control: 'x', kind: 'tap' })
  })

  test('a cable drop while the data channel works keeps the pad online', async () => {
    const s = setup()
    s.session.start()
    s.connect()
    await s.offer()
    s.peers[0].channel('control').open()
    s.sock().drop()
    expect(s.states.at(-1)).toBe('online')
  })
})
