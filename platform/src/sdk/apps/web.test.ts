import { describe, expect, test } from "bun:test";
import { base64ToBytes, bytesToBase64, manifestRuntimes, payloadBytes, toBytes, transferables } from "./web";
import { webField } from "./web";
import { validateSimpleApp } from "./validate";

const base = { schema: "yui.simple-js.v0", id: "dev.test.app", name: "Test", version: "1.0.0", mount() {} };

describe("web manifest", () => {
	test("reads web as a path or an object, ignoring web props inside mount()", () => {
		expect(webField(`export default { id: "a.b.c", web: "./web/", mount(ctx) {} }`)).toBe("./web/");
		expect(webField(`export default { id: "a.b.c", web: { runtimes: ["emu", "x"] }, mount(ctx) {} }`)).toEqual({ runtimes: ["emu", "x"] });
		expect(webField(`export default { id: "a.b.c", mount(ctx) { return ctx.ui.embed({ web: "index.html" }) } }`)).toBeUndefined();
	});

	test("validates runtimes and needs assets:runtime for them", () => {
		expect(() => validateSimpleApp({ ...base, web: { runtimes: ["emu"] } })).toThrow("assets:runtime");
		expect(() => validateSimpleApp({ ...base, permissions: ["assets:runtime"], web: { runtimes: ["../x"] } })).toThrow("runtime names");
		expect(() => validateSimpleApp({ ...base, permissions: ["assets:runtime", "saves"], web: { runtimes: ["emu"] } })).not.toThrow();
		expect(manifestRuntimes({ ...base, web: { runtimes: ["emu"] } } as any)).toEqual(["emu"]);
		expect(manifestRuntimes({ ...base, web: "./web/" } as any)).toEqual([]);
	});
});

describe("payloads", () => {
	test("sizes and transferables", () => {
		const buffer = new ArrayBuffer(1024);
		const message = { state: new Uint8Array(buffer), name: "ab", list: [1, 2] };
		expect(payloadBytes(message)).toBeGreaterThanOrEqual(1024 + 4 + 16);
		expect(transferables(message)).toEqual([buffer]);
		// A view over part of a buffer is copied, not transferred.
		expect(transferables({ part: new Uint8Array(buffer, 0, 10) })).toEqual([]);
	});

	test("base64 round-trips large binary data", () => {
		const bytes = new Uint8Array(200_000);
		for (let i = 0; i < bytes.length; i++) bytes[i] = (i * 31) & 0xff;
		expect(base64ToBytes(bytesToBase64(bytes))).toEqual(bytes);
		expect(toBytes("hi")).toEqual(new Uint8Array([104, 105]));
		expect(() => toBytes(42)).toThrow("bytes");
	});
});
