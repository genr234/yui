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
		on: () => () => {},
	},
}));

const { pad, padAction } = await import("./pad");

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
