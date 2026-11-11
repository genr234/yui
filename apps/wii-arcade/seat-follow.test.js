import { describe, expect, test } from "bun:test";
import app from "./app.yui.js";

// Drives the real app script with a stand-in platform: enough of ctx to mount
// it, then puts it mid-game and replays roster updates as the platform sends them.
function rig() {
	const handlers = {};
	const attached = {}; // player id -> slot, as the controller keeps it
	const calls = [];
	const players = [];
	let state;
	const loose = new Proxy(function () {}, { get: () => loose, apply: () => loose });
	const ctx = {
		state: (initial) => (state = initial),
		controllers: {
			get players() {
				return players;
			},
			ui: loose,
			on: (event, fn) => (handlers[event] = fn),
			layout: () => {},
			joinCard: () => null,
		},
		ui: loose,
		log: { warn: () => {} },
		launch: { item: null },
		assets: Object.assign(async () => [], { onChange: () => {} }),
		library: { list: async () => [], publish: async () => {} },
		toast: async () => {},
		wakeLock: null,
		native: { on: () => {}, status: async () => ({}), launch: async () => ({}), stop: async () => {} },
		virtualPads: {
			attach: async (player, { slot }) => {
				calls.push(["attach", player.id, slot]);
				for (const [id, held] of Object.entries(attached)) if (id !== player.id && held === slot) delete attached[id];
				attached[player.id] = slot;
				return { address: `127.0.0.1:26760/${slot}` };
			},
			detach: async (player) => {
				const id = typeof player === "string" ? player : player.id;
				calls.push(["detach", id]);
				delete attached[id];
			},
			systemPad: async () => ({ address: "127.0.0.1:26760/4" }),
			press: async () => {},
		},
	};
	app.mount(ctx);
	return { handlers, attached, calls, players, state: () => state };
}

const person = (id, slot, extra = {}) => ({ id, slot, name: id, color: "blue", host: false, connected: true, ...extra });
const settle = () => new Promise((resolve) => setTimeout(resolve, 30));

/** Four seated players holding Wii Remotes 0-3, plus a spectator, mid-game. */
async function midGame(spectators = [person("e", null)]) {
	const r = rig();
	r.players.push(person("a", 1, { host: true }), person("b", 2), person("c", 3), person("d", 4), ...spectators);
	Object.assign(r.state(), { phase: "playing", slots: { a: 0, b: 1, c: 2, d: 3 } });
	for (const [id, slot] of Object.entries(r.state().slots)) r.attached[id] = slot;
	r.handlers.players(); // baseline: who sits where
	await settle();
	r.calls.length = 0;
	return r;
}

function setSeat(r, id, slot) {
	r.players.find((p) => p.id === id).slot = slot;
}

describe("Wii Arcade follows a passed Yui seat", () => {
	test("the new seat holder drives the giver's remote", async () => {
		const r = await midGame();
		// Rails announces the recipient first, then the giver.
		setSeat(r, "e", 2);
		r.handlers.players();
		setSeat(r, "b", null);
		r.handlers.players();
		await settle();

		expect(r.state().slots).toEqual({ a: 0, e: 1, c: 2, d: 3 });
		expect(r.attached).toEqual({ a: 0, e: 1, c: 2, d: 3 });
		expect(r.calls).toEqual([["attach", "e", 1]]);
	});

	test("the giver watches afterwards, even with a free remote", async () => {
		const r = await midGame();
		await r.state(); // keep a reference alive
		delete r.state().slots.d;
		delete r.attached.d;
		r.players.splice(3, 1); // d left: remote 3 is free
		r.handlers.players();
		r.calls.length = 0;

		setSeat(r, "b", null);
		setSeat(r, "e", 2);
		r.handlers.players();
		await settle();
		r.handlers.players();
		await settle();

		expect(r.state().slots.e).toBe(1);
		expect(r.state().slots.b).toBeUndefined();
		expect(r.calls.filter(([kind, id]) => kind === "attach" && id === "b")).toEqual([]);
	});

	test("a roster update with no seat change leaves the remotes alone", async () => {
		const r = await midGame([]);
		r.handlers.players();
		await settle();
		expect(r.calls).toEqual([]);
		expect(r.state().slots).toEqual({ a: 0, b: 1, c: 2, d: 3 });
	});

	test("outside a game nothing is attached", async () => {
		const r = rig();
		r.players.push(person("a", 1, { host: true }), person("e", null));
		r.handlers.players();
		setSeat(r, "e", 1);
		setSeat(r, "a", null);
		r.handlers.players();
		await settle();
		expect(r.calls).toEqual([]);
	});
});
