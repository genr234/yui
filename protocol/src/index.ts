// Yui Pad wire protocol: types, input codec and control message validation.
// Mirrors controller/internal/pad/protocol (Go). Keep both in sync via fixtures/.

export const PROTOCOL_VERSION = 1;

export type Color = "blue" | "red" | "green" | "yellow" | "purple" | "orange" | "pink" | "teal";
export const COLORS: readonly Color[] = ["blue", "red", "green", "yellow", "purple", "orange", "pink", "teal"];

export interface Player {
  id: string;
  slot: number | null;
  name: string;
  color: Color | string;
  host: boolean;
  connected: boolean;
  /** This phone plays the kiosk's audio (contracts §2). Absent on older kiosks. */
  speaker?: boolean;
}

export interface IceServer {
  urls: string[];
  username?: string;
  credential?: string;
}

export interface IceConfig {
  ice_servers: IceServer[];
  ttl: number;
}

export interface Node {
  type: string;
  props?: Record<string, unknown>;
  children?: Node[];
}

export type EventKind = "press" | "release" | "tap" | "change" | "submit" | "swipe";
export type TextOp = "insert" | "backspace" | "enter";
// "sensitive" is kiosk→pad only: value true while the kiosk shows a sensitive surface (D9).
export type SystemAction = "home" | "resume" | "quit" | "save_state" | "load_state" | "be_speaker" | "volume" | "sensitive";

export interface Hello {
  t: "hello";
  v: number;
  caps: { motion: boolean; haptics: boolean; wakeLock: boolean };
  screen: { w: number; h: number; orientation: string };
}
export interface Welcome {
  t: "welcome";
  v: number;
  player: Player;
  kiosk_name: string;
}
export interface Layout {
  t: "layout";
  version: number;
  tree: Node;
}
export interface PadEvent {
  t: "event";
  control: string;
  kind: EventKind;
  value?: unknown;
}
export interface TextMsg {
  t: "text";
  op: TextOp;
  text?: string;
}
export interface Haptic {
  t: "haptic";
  pattern: number[];
}
export interface Sound {
  t: "sound";
  url: string;
  volume?: number;
}
export interface PlayerMsg {
  t: "player";
  player: Player;
}
export interface System {
  t: "system";
  action: SystemAction;
  value?: unknown;
}
export interface Ping {
  t: "ping";
  id: number | string;
  ts: number;
}
export interface Pong {
  t: "pong";
  id: number | string;
  ts: number;
}

export type ControlMessage =
  | Hello
  | Welcome
  | Layout
  | PadEvent
  | TextMsg
  | Haptic
  | Sound
  | PlayerMsg
  | System
  | Ping
  | Pong;

// ---------------------------------------------------------------- input codec

export const INPUT_SIZE = 48;
export const INPUT_VERSION = 1;
export const INPUT_TYPE_STATE = 1;

// Generic gamepad buttons, named by position so any app or runtime can map
// them (Xbox A = South, PlayStation Cross = South, Nintendo B = South).
// Device-shaped layouts (e.g. a Wiimote preset) map their own labels onto
// these bits; the platform never knows about a specific console.
export const Buttons = {
  South: 1 << 0,
  East: 1 << 1,
  West: 1 << 2,
  North: 1 << 3,
  L1: 1 << 4,
  R1: 1 << 5,
  L2: 1 << 6,
  R2: 1 << 7,
  Select: 1 << 8,
  Start: 1 << 9,
  Home: 1 << 10, // reserved: always opens the phone's system sheet (D8)
  L3: 1 << 11,
  R3: 1 << 12,
  Up: 1 << 13,
  Down: 1 << 14,
  Left: 1 << 15,
  Right: 1 << 16,
  Recenter: 1 << 17, // system: re-zero motion / pointer
} as const;

export const ALL_BUTTONS = 0x3ffff;

export type Vec3 = [number, number, number];

export interface InputState {
  seq: number; // u16
  tMs: number; // u32
  buttons: number; // u32
  lx: number; // i16
  ly: number;
  rx: number;
  ry: number;
  accel: Vec3; // g
  gyro: Vec3; // deg/s
  px: number; // i16
  py: number;
}

export function encodeInput(s: InputState): ArrayBuffer {
  const buf = new ArrayBuffer(INPUT_SIZE);
  const v = new DataView(buf);
  v.setUint8(0, INPUT_VERSION);
  v.setUint8(1, INPUT_TYPE_STATE);
  v.setUint16(2, s.seq & 0xffff, true);
  v.setUint32(4, s.tMs >>> 0, true);
  v.setUint32(8, s.buttons >>> 0, true);
  v.setInt16(12, s.lx, true);
  v.setInt16(14, s.ly, true);
  v.setInt16(16, s.rx, true);
  v.setInt16(18, s.ry, true);
  for (let i = 0; i < 3; i++) {
    v.setFloat32(20 + i * 4, s.accel[i], true);
    v.setFloat32(32 + i * 4, s.gyro[i], true);
  }
  v.setInt16(44, s.px, true);
  v.setInt16(46, s.py, true);
  return buf;
}

export function decodeInput(buf: ArrayBuffer | ArrayBufferView): InputState {
  const bytes =
    buf instanceof ArrayBuffer
      ? new Uint8Array(buf)
      : new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength);
  if (bytes.byteLength !== INPUT_SIZE) {
    throw new Error(`input: expected ${INPUT_SIZE} bytes, got ${bytes.byteLength}`);
  }
  const v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (v.getUint8(0) !== INPUT_VERSION) throw new Error(`input: unsupported version ${v.getUint8(0)}`);
  if (v.getUint8(1) !== INPUT_TYPE_STATE) throw new Error(`input: unsupported type ${v.getUint8(1)}`);
  return {
    seq: v.getUint16(2, true),
    tMs: v.getUint32(4, true),
    buttons: v.getUint32(8, true),
    lx: v.getInt16(12, true),
    ly: v.getInt16(14, true),
    rx: v.getInt16(16, true),
    ry: v.getInt16(18, true),
    accel: [v.getFloat32(20, true), v.getFloat32(24, true), v.getFloat32(28, true)],
    gyro: [v.getFloat32(32, true), v.getFloat32(36, true), v.getFloat32(40, true)],
    px: v.getInt16(44, true),
    py: v.getInt16(46, true),
  };
}

/** True when `a` is newer than `b` using 16-bit serial-number arithmetic (RFC 1982). */
export function seqNewer(a: number, b: number): boolean {
  const d = (a - b) & 0xffff;
  return d !== 0 && d < 0x8000;
}

export function inputToHex(buf: ArrayBuffer): string {
  return Array.from(new Uint8Array(buf), (b) => b.toString(16).padStart(2, "0")).join("");
}

export function hexToBytes(hex: string): ArrayBuffer {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out.buffer;
}

// ------------------------------------------------------------- control parsing

type Obj = Record<string, unknown>;

const isObj = (x: unknown): x is Obj => typeof x === "object" && x !== null && !Array.isArray(x);
const isStr = (x: unknown): x is string => typeof x === "string";
const isNum = (x: unknown): x is number => typeof x === "number" && Number.isFinite(x);
const isBool = (x: unknown): x is boolean => typeof x === "boolean";

function fail(msg: string): never {
  throw new Error(`control: ${msg}`);
}

function checkPlayer(p: unknown, where: string): void {
  if (!isObj(p)) fail(`${where}: player must be an object`);
  if (!isStr(p.id)) fail(`${where}: player.id must be a string`);
  if (!(p.slot === null || isNum(p.slot))) fail(`${where}: player.slot must be number or null`);
  if (!isStr(p.name)) fail(`${where}: player.name must be a string`);
  if (!isStr(p.color)) fail(`${where}: player.color must be a string`);
  if (!isBool(p.host)) fail(`${where}: player.host must be a boolean`);
  if (!isBool(p.connected)) fail(`${where}: player.connected must be a boolean`);
}

function checkNode(n: unknown, depth = 0): void {
  if (depth > 32) fail("layout: tree too deep");
  if (!isObj(n)) fail("layout: node must be an object");
  if (!isStr(n.type) || n.type === "") fail("layout: node.type must be a non-empty string");
  if (n.props !== undefined && !isObj(n.props)) fail("layout: node.props must be an object");
  if (n.children !== undefined) {
    if (!Array.isArray(n.children)) fail("layout: node.children must be an array");
    for (const c of n.children) checkNode(c, depth + 1);
  }
}

const EVENT_KINDS = ["press", "release", "tap", "change", "submit", "swipe"];
const TEXT_OPS = ["insert", "backspace", "enter"];
const SYSTEM_ACTIONS = ["home", "resume", "quit", "save_state", "load_state", "be_speaker", "volume", "sensitive"];

/** Parse and validate a control-channel JSON frame. Throws on invalid input. */
export function parseControl(json: string): ControlMessage {
  let m: unknown;
  try {
    m = JSON.parse(json);
  } catch {
    fail("invalid JSON");
  }
  if (!isObj(m)) fail("message must be an object");
  const t = m.t;
  if (!isStr(t)) fail("missing t");
  switch (t) {
    case "hello": {
      if (!isNum(m.v)) fail("hello: v must be a number");
      const caps = m.caps;
      const screen = m.screen;
      if (!isObj(caps) || !isBool(caps.motion) || !isBool(caps.haptics) || !isBool(caps.wakeLock)) fail("hello: bad caps");
      if (!isObj(screen) || !isNum(screen.w) || !isNum(screen.h) || !isStr(screen.orientation)) fail("hello: bad screen");
      break;
    }
    case "welcome":
      if (!isNum(m.v)) fail("welcome: v must be a number");
      checkPlayer(m.player, "welcome");
      if (!isStr(m.kiosk_name)) fail("welcome: kiosk_name must be a string");
      break;
    case "layout":
      if (!isNum(m.version)) fail("layout: version must be a number");
      checkNode(m.tree);
      break;
    case "event":
      if (!isStr(m.control) || m.control === "") fail("event: control must be a non-empty string");
      if (!isStr(m.kind) || !EVENT_KINDS.includes(m.kind)) fail("event: bad kind");
      break;
    case "text":
      if (!isStr(m.op) || !TEXT_OPS.includes(m.op)) fail("text: bad op");
      if (m.text !== undefined && !isStr(m.text)) fail("text: text must be a string");
      break;
    case "haptic":
      if (!Array.isArray(m.pattern) || !m.pattern.every(isNum)) fail("haptic: pattern must be number[]");
      break;
    case "sound":
      if (!isStr(m.url) || m.url === "") fail("sound: url must be a non-empty string");
      if (m.volume !== undefined && !isNum(m.volume)) fail("sound: volume must be a number");
      break;
    case "player":
      checkPlayer(m.player, "player");
      break;
    case "system":
      if (!isStr(m.action) || !SYSTEM_ACTIONS.includes(m.action)) fail("system: bad action");
      break;
    case "ping":
    case "pong":
      if (!(isNum(m.id) || isStr(m.id))) fail(`${t}: id must be number or string`);
      if (!isNum(m.ts)) fail(`${t}: ts must be a number`);
      break;
    default:
      fail(`unknown t "${t}"`);
  }
  return m as unknown as ControlMessage;
}
