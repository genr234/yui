import { describe, expect, test } from "bun:test";
import quiz from "../../../../apps/party-quiz/app.yui.js";

/** Party Quiz keeps a linked player's best score in their profile and never blocks guests. */
function setup(profileFor: (id: string) => any) {
	const handlers = new Map<string, (data: any) => void>();
	let layout: ((p: any) => any) | undefined;
	const players = [
		{ id: "linked", name: "Ada", host: true },
		{ id: "guest", name: "Bo", host: false },
	];
	const ui = new Proxy({}, { get: () => (a: any, b: any) => ({ a, b }) }) as any;
	const ctx: any = {
		controllers: {
			players,
			ui,
			layout: (fn: any) => (layout = fn),
			on: (event: string, fn: any) => handlers.set(event, fn),
			haptic: async () => {},
			joinCard: () => null,
		},
		profiles: { for: profileFor },
		library: { publish: async () => {}, played: async () => {} },
		launch: { item: null },
		ui: new Proxy({}, { get: () => () => null }),
		log: { warn() {} },
		state: (initial: any) => initial,
	};
	quiz.mount(ctx);
	const tap = (id: string, control: string, value?: unknown) =>
		handlers.get("event")!({ player: players.find((p) => p.id === id), control, kind: "tap", value });
	return { tap, layout: () => layout! , players };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

function playThrough(env: ReturnType<typeof setup>) {
	env.tap("linked", "start");
	for (let round = 0; round < 5; round++) {
		env.tap("guest", "answer", 0);
		env.tap("linked", "reveal");
		env.tap("linked", "next");
	}
}

describe("party quiz profiles", () => {
	test("asks a linked player once, saves their best, and skips guests", async () => {
		const calls: string[] = [];
		let stored: any = { data: { best: 2, other: true }, version: 4 };
		const profile = {
			request: async () => (calls.push("request"), { granted: true }),
			get: async () => (calls.push("get"), stored),
			set: async (data: any, version: number) => {
				calls.push(`set ${JSON.stringify(data)} @${version}`);
				stored = { data, version: version + 1 };
				return { version: version + 1 };
			},
		};
		const env = setup((id) => (id === "linked" ? profile : null));
		playThrough(env);
		await flush();
		await flush();
		expect(calls).toEqual(["request", "get", 'set {"best":2,"other":true} @4']);
		// A second finish does not ask again.
		calls.length = 0;
		env.tap("linked", "restart");
		playThrough(env);
		await flush();
		await flush();
		expect(calls.includes("request")).toBe(false);
	});

	test("a declined request saves nothing and the round still ends", async () => {
		const calls: string[] = [];
		const profile = {
			request: async () => (calls.push("request"), { granted: false }),
			get: async () => (calls.push("get"), { data: null, version: 0 }),
			set: async () => (calls.push("set"), { version: 1 }),
		};
		const env = setup((id) => (id === "linked" ? profile : null));
		playThrough(env);
		await flush();
		await flush();
		expect(calls).toEqual(["request"]);
	});

	test("a version conflict re-reads and merges", async () => {
		let version = 1;
		const sets: any[] = [];
		const profile = {
			request: async () => ({ granted: true }),
			get: async () => ({ data: { best: 1 }, version: version }),
			set: async (data: any, v: number) => {
				if (sets.length === 0) {
					sets.push("conflict");
					version = 2;
					throw Object.assign(new Error("conflict"), { code: "version_conflict", version: 2 });
				}
				sets.push([data, v]);
				return { version: v + 1 };
			},
		};
		const env = setup((id) => (id === "linked" ? profile : null));
		playThrough(env);
		for (let i = 0; i < 6; i++) await flush();
		expect(sets).toEqual(["conflict", [{ best: 1 }, 2]]);
	});
});
