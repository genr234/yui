import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  HAPTIC_NAMES,
  avatarImage,
  decodeInput,
  decodeTouch,
  encodeInput,
  encodeTouch,
  hexToBytes,
  inputFrameType,
  inputToHex,
  parseControl,
  seqNewer,
  INPUT_SIZE,
  TOUCH_SIZE,
} from "./index";

const fx = join(import.meta.dir, "..", "fixtures");
const load = (dir: string) =>
  readdirSync(join(fx, dir))
    .filter((f) => f.endsWith(".json"))
    .sort()
    .map((f) => ({ file: f, raw: readFileSync(join(fx, dir, f), "utf8") }));

describe("input fixtures", () => {
  const cases = load("input").map((c) => ({ file: c.file, ...JSON.parse(c.raw) }));
  test("at least 4 cases", () => expect(cases.length).toBeGreaterThanOrEqual(4));
  for (const c of cases) {
    test(`${c.name}: encode`, () => {
      const buf = encodeInput(c.fields);
      expect(buf.byteLength).toBe(INPUT_SIZE);
      expect(inputToHex(buf)).toBe(c.hex);
    });
    test(`${c.name}: decode`, () => {
      expect(decodeInput(hexToBytes(c.hex))).toEqual(c.fields);
    });
  }
  test("decode rejects wrong length/version", () => {
    expect(() => decodeInput(new ArrayBuffer(47))).toThrow();
    const b = new Uint8Array(hexToBytes(cases[0].hex));
    b[0] = 2;
    expect(() => decodeInput(b.buffer)).toThrow();
  });
});

describe("touch fixtures", () => {
  const cases = load("touch").map((c) => ({ file: c.file, ...JSON.parse(c.raw) }));
  test("at least 3 cases", () => expect(cases.length).toBeGreaterThanOrEqual(3));
  for (const c of cases) {
    test(`${c.name}: encode`, () => {
      const buf = encodeTouch(c.fields);
      expect(buf.byteLength).toBe(TOUCH_SIZE);
      expect(inputToHex(buf)).toBe(c.hex);
    });
    test(`${c.name}: decode`, () => {
      expect(decodeTouch(hexToBytes(c.hex))).toEqual(c.fields);
      expect(inputFrameType(hexToBytes(c.hex))).toBe(2);
    });
  }
  test("decode rejects wrong length, version and type", () => {
    expect(() => decodeTouch(new ArrayBuffer(19))).toThrow();
    const b = new Uint8Array(hexToBytes(cases[0].hex));
    b[0] = 2;
    expect(() => decodeTouch(b)).toThrow();
    b[0] = 1;
    b[1] = 1;
    expect(() => decodeTouch(b)).toThrow();
  });
  test("decodes from a view with an offset", () => {
    const inner = new Uint8Array(hexToBytes(cases[0].hex));
    const padded = new Uint8Array(inner.length + 3);
    padded.set(inner, 3);
    expect(decodeTouch(padded.subarray(3))).toEqual(cases[0].fields);
  });
});

describe("inputFrameType", () => {
  test("state frames are 1, bad frames throw", () => {
    expect(inputFrameType(encodeInput({ seq: 0, tMs: 0, buttons: 0, lx: 0, ly: 0, rx: 0, ry: 0, accel: [0, 0, 0], gyro: [0, 0, 0], px: 0, py: 0 }))).toBe(1);
    expect(() => inputFrameType(new ArrayBuffer(0))).toThrow();
    expect(() => inputFrameType(new Uint8Array([2, 1]))).toThrow();
  });
});

describe("surface and caps.video", () => {
  test("hello caps.video is optional but must be a boolean", () => {
    const hello = (video: unknown) =>
      JSON.stringify({ t: "hello", v: 1, caps: { motion: true, haptics: true, wakeLock: true, video }, screen: { w: 1, h: 1, orientation: "landscape" } });
    expect(parseControl(hello(true)).t).toBe("hello");
    expect(parseControl(hello(undefined)).t).toBe("hello");
    expect(() => parseControl(hello("yes"))).toThrow();
  });
  test("surface id length and on-size rules", () => {
    expect(() => parseControl(JSON.stringify({ t: "surface", id: "a".repeat(33), state: "off" }))).toThrow();
    expect(() => parseControl('{"t":"surface","id":"a","state":"on","width":0,"height":480}')).toThrow();
    expect(parseControl('{"t":"surface","id":"a","state":"off"}')).toEqual({ t: "surface", id: "a", state: "off" });
  });
});

describe("seqNewer", () => {
  test("basic", () => {
    expect(seqNewer(2, 1)).toBe(true);
    expect(seqNewer(1, 2)).toBe(false);
    expect(seqNewer(5, 5)).toBe(false);
  });
  test("wraparound", () => {
    expect(seqNewer(0, 65535)).toBe(true);
    expect(seqNewer(65535, 0)).toBe(false);
    expect(seqNewer(10, 65530)).toBe(true);
  });
  test("half-range is not newer", () => {
    expect(seqNewer(0x8000, 0)).toBe(false);
    expect(seqNewer(0x7fff, 0)).toBe(true);
  });
});

describe("control fixtures", () => {
  for (const c of load("control")) {
    test(`valid ${c.file}`, () => {
      const m = parseControl(c.raw);
      expect(m.t).toBe(JSON.parse(c.raw).t);
    });
  }
  for (const c of load("control/invalid")) {
    test(`invalid ${c.file}`, () => {
      expect(() => parseControl(c.raw)).toThrow();
    });
  }
});

describe("haptic", () => {
  const ok = (o: object) => parseControl(JSON.stringify({ t: "haptic", ...o }));
  const bad = (o: object) => expect(() => ok(o)).toThrow();
  test("legacy bare pattern still parses", () => expect(ok({ pattern: [30, 20, 30] })).toEqual({ t: "haptic", pattern: [30, 20, 30] }));
  test("every vocabulary name parses, with intensity", () => {
    for (const name of HAPTIC_NAMES) expect(ok({ name, intensity: 0.5 })).toMatchObject({ name });
  });
  test("limits", () => {
    ok({ pattern: Array(16).fill(250) });
    bad({ pattern: Array(17).fill(1) });
    bad({ pattern: [] });
    bad({ pattern: [1001] });
    bad({ pattern: [-1] });
    bad({ pattern: [1000, 1000, 1000, 1000, 1] }); // over 4 s
  });
  test("exactly one of name / pattern, known names, intensity range", () => {
    bad({});
    bad({ name: "tap", pattern: [10] });
    bad({ name: "zap" });
    bad({ name: "tap", intensity: 1.5 });
    bad({ name: "tap", intensity: -0.1 });
  });
});

describe("system sensitive", () => {
  // Not a shared fixture yet: the Go mirror adds "sensitive" separately.
  test("kiosk can flag a sensitive surface", () => {
    const m = parseControl('{"t":"system","action":"sensitive","value":true}');
    expect(m).toEqual({ t: "system", action: "sensitive", value: true });
  });
});

describe("system be_speaker channel", () => {
  test("be_speaker takes an optional left or right channel", () => {
    expect(parseControl('{"t":"system","action":"be_speaker"}')).toEqual({ t: "system", action: "be_speaker" });
    expect(parseControl('{"t":"system","action":"be_speaker","channel":"right"}')).toEqual({
      t: "system",
      action: "be_speaker",
      channel: "right",
    });
    expect(() => parseControl('{"t":"system","action":"be_speaker","channel":"middle"}')).toThrow();
  });
});

describe("profile messages (plan 10)", () => {
  test("request and reply parse", () => {
    expect(parseControl('{"t":"profile_request","app_id":"a.b","app_name":"Game"}')).toEqual({ t: "profile_request", app_id: "a.b", app_name: "Game" });
    expect(parseControl('{"t":"profile_reply","app_id":"a.b","granted":false}')).toEqual({ t: "profile_reply", app_id: "a.b", granted: false });
  });
  test("rejects bad shapes", () => {
    expect(() => parseControl('{"t":"profile_request","app_id":"a.b"}')).toThrow();
    expect(() => parseControl('{"t":"profile_reply","app_id":5,"granted":true}')).toThrow();
  });
});

describe("avatarImage", () => {
  test("only jpeg and webp base64 data urls are images", () => {
    expect(avatarImage("data:image/jpeg;base64,/9j/4AAQ")).toBe("data:image/jpeg;base64,/9j/4AAQ");
    expect(avatarImage("data:image/webp;base64,UklGRg==")).toBe("data:image/webp;base64,UklGRg==");
    expect(avatarImage("data:image/svg+xml;base64,PHN2Zy8+")).toBeNull();
    expect(avatarImage("javascript:alert(1)")).toBeNull();
    expect(avatarImage(" data:image/jpeg;base64,/9j/")).toBeNull();
    expect(avatarImage("🦊")).toBeNull();
    expect(avatarImage(null)).toBeNull();
    expect(avatarImage(undefined)).toBeNull();
  });
});
