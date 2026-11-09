import { describe, expect, mock, test } from "bun:test";

mock.module("../bridge", () => ({
	bridge: { send: () => Promise.resolve(null), on: () => () => {} },
}));

const { handleSandboxApiCall, sandboxApiSource } = await import("./sandbox-apis");

describe("ctx.assets across the sandbox boundary", () => {
	test("the sandbox ctx gets an assets function that lists through rpc", () => {
		expect(sandboxApiSource).toContain('rpc("assets", "list", [kind])');
		expect(sandboxApiSource).toContain('yuiSubscribe("assets", "changed", handler)');
	});

	test("assets.list calls the host ctx with the kind", async () => {
		const calls: string[] = [];
		const ctx: any = { assets: Object.assign(async (kind: string) => (calls.push(kind), [{ id: "1" }]), { onChange: () => () => {} }) };
		const result = await handleSandboxApiCall(ctx, "assets", "list", ["game"], () => {});
		expect(calls).toEqual(["game"]);
		expect(result).toEqual([{ id: "1" }]);
	});

	test("assets.subscribe pushes changed events and rejects unknown events", async () => {
		const pushes: unknown[][] = [];
		let fire: () => void = () => {};
		const ctx: any = {
			assets: Object.assign(async () => [], {
				onChange: (handler: () => void) => {
					fire = handler;
					return () => {};
				},
			}),
		};
		await handleSandboxApiCall(ctx, "assets", "subscribe", ["changed"], (...args) => pushes.push(args));
		fire();
		expect(pushes).toEqual([["assets", "changed", null]]);
		await expect(handleSandboxApiCall(ctx, "assets", "subscribe", ["nope"], () => {})).rejects.toThrow();
	});
});
