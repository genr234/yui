import { describe, expect, test } from "bun:test";
import { nativeWindowToWire } from "./native-window";

describe("native window option", () => {
	test("absent means no window key on the wire", () => {
		expect(nativeWindowToWire(undefined)).toBeUndefined();
	});

	test("ratio strings and numbers become a number", () => {
		expect(nativeWindowToWire({ aspect: "16:9" })?.aspect).toBeCloseTo(16 / 9);
		expect(nativeWindowToWire({ aspect: "4:3", align: "center" })?.aspect).toBeCloseTo(4 / 3);
		expect(nativeWindowToWire({ aspect: 1.5 })).toEqual({ aspect: 1.5, align: "center" });
	});

	test("bad values are rejected", () => {
		expect(() => nativeWindowToWire({ aspect: "wide" as any })).toThrow();
		expect(() => nativeWindowToWire({ aspect: 0 })).toThrow();
		expect(() => nativeWindowToWire({ aspect: "16:0" })).toThrow();
		expect(() => nativeWindowToWire({ aspect: 2, align: "left" as any })).toThrow();
	});
});
