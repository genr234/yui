import { describe, expect, mock, test } from "bun:test";
import { get } from "svelte/store";

const sends: string[] = [];
const sentParams: unknown[] = [];
let release: (value: unknown) => void = () => {};
let fail: Error | null = null;

mock.module("./bridge", () => ({
	bridge: {
		send: (method: string, params?: unknown) => {
			sends.push(method);
			sentParams.push(params);
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

const { pad, padAction, cursorColor, remoteIntent, linkQuality, avatarKind } = await import("./pad");

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

describe("session admin", () => {
	test("pairing as stereo sends the channel with pad.speaker", () => {
		sends.length = 0;
		sentParams.length = 0;
		void pad.makeSpeaker("p2", "right");
		void pad.makeSpeaker("p3");
		expect(sends).toEqual(["pad.speaker", "pad.speaker"]);
		expect(sentParams).toEqual([{ player_id: "p2", channel: "right" }, { player_id: "p3" }]);
	});

	test("lobby, kick, host and speaker call their bridge commands", async () => {
		sends.length = 0;
		void pad.kick("p2");
		void pad.makeHost("p2");
		void pad.makeSpeaker("p2");
		const lobby = pad.setLobby(true);
		release({ session: { code: "KQXM", join_url: "https://yui.example/j", lobby_open: true }, players: [] });
		expect(sends).toEqual(["pad.kick", "pad.host", "pad.speaker", "pad.lobby"]);
		expect((await lobby)?.session?.lobby_open).toBe(true);
	});

	test("a failed kick resolves null and shows its error without marking busy", async () => {
		fail = new Error("player not found");
		const result = await pad.kick("p9");
		fail = null;
		expect(result).toBeNull();
		expect(get(padAction)).toEqual({ busy: false, error: "player not found" });
	});
});

describe("linkQuality", () => {
	test("buckets the round trip", () => {
		expect(linkQuality(undefined)).toBeNull();
		expect(linkQuality(12)).toBe("good");
		expect(linkQuality(80)).toBe("ok");
		expect(linkQuality(400)).toBe("slow");
	});
});

describe("avatarKind", () => {
	test("jpeg and webp data urls are images", () => {
		expect(avatarKind("data:image/jpeg;base64,/9j/4AAQ")).toEqual({ kind: "image", src: "data:image/jpeg;base64,/9j/4AAQ" });
		expect(avatarKind("data:image/webp;base64,UklGRg==")).toEqual({ kind: "image", src: "data:image/webp;base64,UklGRg==" });
	});

	test("an emoji is text", () => {
		expect(avatarKind("🦊")).toEqual({ kind: "text", text: "🦊" });
		expect(avatarKind("<img src=x onerror=alert(1)>")?.kind).not.toBe("image");
	});

	test("other urls, empty and oversized values are no avatar", () => {
		expect(avatarKind("data:image/svg+xml;base64,PHN2Zz4=")).toBeNull();
		expect(avatarKind("data:image/png;base64,iVBORw0KGgo=")).toBeNull();
		expect(avatarKind("javascript:alert(1)")).toBeNull();
		expect(avatarKind("https://example.com/me.jpg")).toBeNull();
		expect(avatarKind(" data:image/jpeg;base64,/9j/")).toBeNull();
		expect(avatarKind("x".repeat(40))).toBeNull();
		expect(avatarKind("")).toBeNull();
		expect(avatarKind("  ")).toBeNull();
		expect(avatarKind(null)).toBeNull();
		expect(avatarKind(undefined)).toBeNull();
		expect(avatarKind(42)).toBeNull();
	});
});
