import { describe, expect, mock, test } from "bun:test";
import { get } from "svelte/store";

const sends: string[] = [];
let release: (value: unknown) => void = () => {};
let fail: Error | null = null;

mock.module("./bridge", () => ({
	bridge: {
		send: (method: string) => {
			sends.push(method);
			if (fail) return Promise.reject(fail);
			return new Promise((resolve) => {
				release = resolve;
			});
		},
		on: (event: string, fn: (data: unknown) => void) => {
			listeners.set(event, fn);
			return () => listeners.delete(event);
		},
	},
}));

const listeners = new Map<string, (data: unknown) => void>();

const { pad, padAction, cursorColor, remoteIntent } = await import("./pad");

describe("pad events", () => {
	test("each phone event has its own bridge event name", () => {
		const seen: string[] = [];
		const offs = [
			pad.onHome((home) => seen.push(`home:${home.via}`)),
			pad.onRemote((remote) => seen.push(`remote:${remote.action}`)),
			pad.onCursor((cursor) => seen.push(`cursor:${cursor.x},${cursor.y}`)),
			pad.onKeyboard((active) => seen.push(`keyboard:${active}`)),
		];
		listeners.get("pad.home")?.({ player_id: "p1", via: "button" });
		listeners.get("pad.remote")?.({ player_id: "p1", action: "open_yui" });
		listeners.get("pad.cursor")?.({ x: 4, y: 5, visible: true });
		listeners.get("pad.keyboard")?.({ active: true });
		listeners.get("pad.keyboard")?.(null);
		expect(seen).toEqual(["home:button", "remote:open_yui", "cursor:4,5", "keyboard:true", "keyboard:false"]);
		for (const off of offs) off();
		expect(listeners.size).toBe(0);
	});

	test("remote buttons open, close and go back", () => {
		expect(remoteIntent("open_yui", false)).toBe("open");
		expect(remoteIntent("open_yui", true)).toBe("close");
		expect(remoteIntent("back", true)).toBe("close");
		expect(remoteIntent("back", false)).toBe("history-back");
		expect(remoteIntent("home", true)).toBe("close");
		expect(remoteIntent("home", false)).toBe("none");
	});

	test("cursor colour follows the player swatch", () => {
		expect(cursorColor("teal")).toBe("#0f9f8f");
		expect(cursorColor(undefined)).toBe("var(--yui-blue)");
		expect(cursorColor("plaid")).toBe("var(--yui-blue)");
	});
});

describe("pad.run", () => {
	test("a second tap while one is in flight shares the first call", async () => {
		sends.length = 0;
		const first = pad.run("start");
		const second = pad.run("start");
		expect(get(padAction).busy).toBe(true);
		release({ session: { code: "KQXM", join_url: "https://yui.example/j" }, players: [] });
		const [a, b] = await Promise.all([first, second]);
		expect(sends).toEqual(["pad.session.start"]);
		expect(a).toBe(b);
		expect(a?.session?.code).toBe("KQXM");
		expect(get(padAction)).toEqual({ busy: false, error: "" });
	});

	test("a failed start resolves null and keeps the error for the page", async () => {
		fail = new Error("connect this kiosk to an account before starting a play session");
		const result = await pad.run("start");
		fail = null;
		expect(result).toBeNull();
		expect(get(padAction)).toEqual({
			busy: false,
			error: "connect this kiosk to an account before starting a play session",
		});
	});
});
