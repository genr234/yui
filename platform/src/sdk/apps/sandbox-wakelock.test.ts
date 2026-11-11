import { expect, test } from "bun:test";
import { handleSandboxApiCall, sandboxApiSource } from "./sandbox-apis";
import type { YuiContext } from "./types";

test("sandbox wake locks are held by the host ctx and released by token", async () => {
	const released: string[] = [];
	let n = 0;
	const ctx = {
		wakeLock: {
			request: async (reason?: string) => {
				const name = `lock${++n}:${reason}`;
				let done = false;
				return {
					reason: reason ?? "",
					get released() {
						return done;
					},
					release: async () => {
						done = true;
						released.push(name);
					},
				};
			},
		},
	} as unknown as YuiContext;
	const push = () => {};

	const held = (await handleSandboxApiCall(ctx, "wakeLock", "request", ["cutscene"], push)) as { id: string; reason: string };
	expect(held.reason).toBe("cutscene");
	await handleSandboxApiCall(ctx, "wakeLock", "release", [held.id], push);
	await handleSandboxApiCall(ctx, "wakeLock", "release", [held.id], push);
	await handleSandboxApiCall(ctx, "wakeLock", "release", ["unknown"], push);
	expect(released).toEqual(["lock1:cutscene"]);
	expect(sandboxApiSource).toContain("ctx.wakeLock = {");
});
