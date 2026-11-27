import { describe, expect, test } from "bun:test";
import { blocksController } from "./csp";

describe("blocksController", () => {
	const hosts = ["127.0.0.1:7071", "127.0.0.1:7070", null];
	test("matches the controller's bridge and asset hosts", () => {
		expect(blocksController("ws://127.0.0.1:7071/ws", hosts)).toBe(true);
		expect(blocksController("http://127.0.0.1:7070/assets/logo.png", hosts)).toBe(true);
	});
	test("ignores the page's own blocks", () => {
		expect(blocksController("https://ads.example.com/x.js", hosts)).toBe(false);
		expect(blocksController("inline", hosts)).toBe(false);
		expect(blocksController("", hosts)).toBe(false);
	});
});
