import { describe, expect, test } from "bun:test";
import { isKeymapKey, validateControllersManifest, validateSimpleApp } from "./validate";

const base = {
	schema: "yui.simple-js.v0",
	id: "dev.example.game",
	name: "Game",
	version: "1.0.0",
	permissions: ["controllers"],
	mount() {},
};

const arrows = { up: "ArrowUp", down: "ArrowDown", left: "ArrowLeft", right: "ArrowRight" };

describe("controllers manifest", () => {
	test("the shipped web games and the sample app validate", async () => {
		for (const path of ["subway-surfers", "fruit-ninja", "party-quiz"]) {
			const mod = await import(`../../../../apps/${path}/app.yui.js`);
			expect(() => validateSimpleApp(mod.default)).not.toThrow();
			expect(mod.default.controllers).toBeDefined();
		}
	});

	test("accepts each control type", () => {
		expect(() =>
			validateControllersManifest({
				keymap: {
					players: "all",
					controls: [
						{ type: "swipe", keys: arrows, tap: "Space", label: "Swipe" },
						{ type: "dpad", keys: { up: "w", down: "s" } },
						{ type: "button", label: "Jump", key: "Space" },
						{ type: "button", label: "Fire", mouse: "left" },
						{ type: "trackpad", mode: "absolute", mouse: "drag" },
					],
				},
			}),
		).not.toThrow();
		expect(() => validateControllersManifest({})).not.toThrow();
		expect(() => validateControllersManifest({ keymap: { controls: [{ type: "trackpad" }] } })).not.toThrow();
	});

	test.each([
		["not an object", "keymap"],
		["unknown field", { gamepad: true }],
		["keymap not an object", { keymap: [] }],
		["keymap unknown field", { keymap: { controls: [{ type: "dpad", keys: arrows }], extra: 1 } }],
		["bad players", { keymap: { players: "everyone", controls: [{ type: "dpad", keys: arrows }] } }],
		["no controls", { keymap: { controls: [] } }],
		["too many controls", { keymap: { controls: Array.from({ length: 9 }, () => ({ type: "button", label: "x", key: "a" })) } }],
		["unknown type", { keymap: { controls: [{ type: "joystick" }] } }],
		["unknown key", { keymap: { controls: [{ type: "dpad", keys: { up: "F13" } }] } }],
		["unknown direction", { keymap: { controls: [{ type: "swipe", keys: { north: "ArrowUp" } }] } }],
		["empty keys", { keymap: { controls: [{ type: "swipe", keys: {} }] } }],
		["two swipes", { keymap: { controls: [{ type: "swipe", keys: arrows }, { type: "swipe", keys: arrows }] } }],
		["two trackpads", { keymap: { controls: [{ type: "trackpad" }, { type: "trackpad", mode: "absolute", mouse: "drag" }] } }],
		["button without label", { keymap: { controls: [{ type: "button", key: "a" }] } }],
		["button with key and mouse", { keymap: { controls: [{ type: "button", label: "x", key: "a", mouse: "left" }] } }],
		["button without output", { keymap: { controls: [{ type: "button", label: "x" }] } }],
		["absolute without drag", { keymap: { controls: [{ type: "trackpad", mode: "absolute" }] } }],
		["bad trackpad mode", { keymap: { controls: [{ type: "trackpad", mode: "sideways" }] } }],
		["long label", { keymap: { controls: [{ type: "button", label: "x".repeat(41), key: "a" }] } }],
		["bad tap key", { keymap: { controls: [{ type: "swipe", keys: arrows, tap: "Hyper" }] } }],
	])("rejects %s", (_name, value) => {
		expect(() => validateControllersManifest(value)).toThrow(/YUI_INVALID_APP: controllers/);
	});

	test("controllers needs the controllers permission", () => {
		expect(() => validateSimpleApp({ ...base, controllers: {} })).not.toThrow();
		expect(() => validateSimpleApp({ ...base, permissions: [], controllers: {} })).toThrow(/controllers permission/);
		expect(() => validateSimpleApp({ ...base, controllers: { keymap: { controls: [] } } })).toThrow();
	});

	test("key names match the controller's CDP adapter", () => {
		for (const key of ["Enter", "Space", "ArrowUp", "PageDown", "a", "Z", "7"]) expect(isKeymapKey(key)).toBe(true);
		for (const key of ["F13", "", "ab", "Shift", 3]) expect(isKeymapKey(key)).toBe(false);
	});
});
