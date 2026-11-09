import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { avatarImage, decodeInput, encodeInput, hexToBytes, inputToHex, parseControl, seqNewer, INPUT_SIZE } from "./index";

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

describe("system sensitive", () => {
  // Not a shared fixture yet: the Go mirror adds "sensitive" separately.
  test("kiosk can flag a sensitive surface", () => {
    const m = parseControl('{"t":"system","action":"sensitive","value":true}');
    expect(m).toEqual({ t: "system", action: "sensitive", value: true });
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
