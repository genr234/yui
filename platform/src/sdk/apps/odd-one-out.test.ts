import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import game from "../../../../apps/odd-one-out/app.yui.js";
import { validateSimpleApp } from "./validate";

/** Drives Odd One Out with fake phones: no timers, no network, real game logic. */

type Timer = { fn: () => void; ms: number };
const real = { setTimeout: globalThis.setTimeout, clearTimeout: globalThis.clearTimeout, setInterval: globalThis.setInterval, clearInterval: globalThis.clearInterval };
let timers = new Map<number, Timer>();
let nextTimer = 1;
beforeEach(() => {
	timers = new Map();
	(globalThis as any).setTimeout = (fn: () => void, ms: number) => (timers.set(nextTimer, { fn, ms }), nextTimer++);
	(globalThis as any).clearTimeout = (id: number) => void timers.delete(id);
	(globalThis as any).setInterval = () => nextTimer++; // the countdown tick is never needed here
	(globalThis as any).clearInterval = () => {};
});
afterEach(() => Object.assign(globalThis, real));

/** Runs the pending timer with this delay, as if it elapsed. */
function elapse(ms: number) {
	for (const [id, t] of timers) {
		if (t.ms !== ms) continue;
		timers.delete(id);
		t.fn();
		return;
	}
	throw new Error(`no ${ms} ms timer is pending`);
}
const TURN = 30_000;
const VOTE = 45_000;
const GUESS = 30_000;

const node = (type: string) => (props?: any, children?: any) => ({ type, props: props ?? {}, children });
const padUi: any = {
	text: (text: string, props?: any) => ({ type: "text", props: { ...(props ?? {}), text } }),
	button: node("button"),
	choice: node("choice"),
	sketch: node("sketch"),
	column: node("column"),
};

function walk(value: any, visit: (n: any) => void) {
	if (Array.isArray(value)) return value.forEach((v) => walk(v, visit));
	if (!value || typeof value !== "object") return;
	if (typeof value.type === "string") visit(value);
	walk(value.children, visit);
	walk(value.args, visit);
}
const nodes = (tree: any, type: string) => {
	const out: any[] = [];
	walk(tree, (n) => n.type === type && out.push(n));
	return out;
};
const texts = (tree: any) => {
	const out: string[] = [];
	const collect = (v: any) => {
		if (typeof v === "string") out.push(v);
		else if (Array.isArray(v)) v.forEach(collect);
		else if (v && typeof v === "object") {
			if (typeof v.props?.text === "string") out.push(v.props.text);
			collect(v.children);
			collect(v.args);
		}
	};
	collect(tree);
	return out;
};

const COLORS = ["#e63946", "#1d7de0", "#2a9d4a", "#f0a90a", "#8e44c9"];

function setup(count = 4) {
	const handlers = new Map<string, (data: any) => void>();
	const buzzes: Array<[string, string]> = [];
	let layout: ((p: any) => any) | undefined;
	const players: any[] = Array.from({ length: count }, (_, i) => ({ id: `p${i + 1}`, name: ["Ada", "Bo", "Cy", "Di", "Ed"][i], color: COLORS[i], host: i === 0, connected: true }));
	const ctx: any = {
		controllers: {
			players,
			ui: padUi,
			layout: (fn: any) => (layout = fn),
			on: (event: string, fn: any) => handlers.set(event, fn),
			haptic: async (who: string, name: string) => void buzzes.push([who, name]),
			joinCard: () => null,
		},
		library: { publish: async () => {}, played: async () => {} },
		ui: new Proxy({}, { get: (_t, type: string) => (...args: any[]) => ({ type, args }) }),
		state: (initial: any) => initial,
	};
	const view = game.mount(ctx) as () => any;
	const who = (id: string) => players.find((p) => p.id === id);
	let secret = { odd: "", word: "" };
	const readOdd = () => players.find((p) => texts(env.layoutOf(p.id)).includes("Sei l'intruso. Mimetizzati."))!.id as string;
	const readWord = () => {
		const other = players.find((p) => !texts(env.layoutOf(p.id)).includes("Sei l'intruso. Mimetizzati."))!;
		return texts(env.layoutOf(other.id))[1];
	};
	const env = {
		players,
		buzzes,
		view,
		layoutOf: (id: string) => layout!(who(id)),
		tap: (id: string, control: string, value?: unknown) => {
			handlers.get("event")!({ player: who(id), control, kind: "tap", value });
			// Remember who the odd one out is and the word while the round starts: later screens do not say.
			if (["start", "next", "restart"].includes(control) && env.drawer()) secret = { odd: readOdd(), word: readWord() };
		},
		sketch: (id: string, control: string, kind: string, value: unknown) => {
			handlers.get("event")!({ player: who(id), control, kind, value });
			// The kiosk redraws at most every 60 ms; let that window pass.
			while ([...timers.values()].some((t) => t.ms === 60)) elapse(60);
		},
		leave: (id: string) => {
			const i = players.findIndex((p) => p.id === id);
			const [gone] = players.splice(i, 1);
			if (gone.host && players[0]) players[0].host = true; // the host role moves on
			handlers.get("leave")!(gone);
		},
		/** The sketch node on this phone, if it has an active pad. */
		pad: (id: string) => nodes(env.layoutOf(id), "sketch")[0],
		/** The player whose phone has the pad now. */
		drawer: () => players.find((p) => env.pad(p.id))?.id as string | undefined,
		odd: () => secret.odd,
		word: () => secret.word,
		/** One full turn by whoever holds the pad: a stroke of three points, then submit. */
		stroke: (n = 1) => {
			const id = env.drawer()!;
			const control = env.pad(id).props.id;
			env.sketch(id, control, "change", { stroke: n, color: "#000000", size: 8, points: [[0.1, 0.2, 0], [0.4, 0.5, 30], [0.8, 0.6, 60]] });
			env.sketch(id, control, "submit", { stroke: n });
			return id;
		},
		phaseText: () => texts(env.view()).join(" | "),
		drawingSvg: () => {
			const src = nodes(env.view(), "image").map((n) => n.args[0].src).find((s: string) => s.includes("svg"))!;
			return decodeURIComponent(src.slice(src.indexOf(",") + 1));
		},
		image: () => nodes(env.view(), "image").find((n) => n.args[0].alt === "Il disegno di tutti")!.args[0],
	};
	return env;
}
type Env = ReturnType<typeof setup>;

function drawAllTurns(env: Env) {
	const order: string[] = [];
	for (let i = 0; i < 8; i++) order.push(env.stroke());
	return order;
}

/** Everyone but `skip` votes for `target`; `skip` votes for `other`. */
function vote(env: Env, votes: Record<string, string>) {
	for (const [voter, target] of Object.entries(votes)) env.tap(voter, "vote", target);
}

describe("odd one out: setup", () => {
	test("is a valid app that asks for the phones", () => {
		expect(() => validateSimpleApp(game)).not.toThrow();
		expect(game.controllers).toBeDefined();
	});

	test("the host starts it from their phone, and only with three or more players", () => {
		const env = setup(2);
		expect(texts(env.layoutOf("p1")).join(" ")).toContain("In attesa di giocatori");
		expect(nodes(env.layoutOf("p1"), "button")).toHaveLength(0);
		env.tap("p1", "start");
		expect(texts(env.view()).join(" ")).toContain("Entra dal telefono");

		const three = setup(3);
		expect(nodes(three.layoutOf("p1"), "button")).toHaveLength(1); // the host's Start
		expect(nodes(three.layoutOf("p2"), "button")).toHaveLength(0);
		three.tap("p2", "start"); // not the host
		expect(texts(three.view()).join(" ")).toContain("Entra dal telefono");
		three.tap("p1", "start");
		expect(three.drawer()).toBe("p1");
	});
});

describe("odd one out: a round", () => {
	test("one odd player gets only the category, everyone else the word", () => {
		for (let trial = 0; trial < 20; trial++) {
			const env = setup(5);
			env.tap("p1", "start");
			const odd = env.odd();
			let oddCount = 0;
			for (const p of env.players) {
				const t = texts(env.layoutOf(p.id));
				expect(t[0]).toMatch(/^Categoria: .+/);
				if (p.id === odd) {
					oddCount++;
					expect(t).toContain("Sei l'intruso. Mimetizzati.");
				} else {
					expect(t[1]).toBe(env.word());
				}
			}
			expect(oddCount).toBe(1);
			// The kiosk shows the category but never the word.
			expect(env.phaseText()).toContain(t0Category(env));
			expect(env.phaseText()).not.toContain(env.word() + " |");
		}
	});

	test("turns follow the join order for two laps, each in the player's colour", () => {
		const env = setup(4);
		env.tap("p1", "start");
		const ids = new Set<string>();
		const order: string[] = [];
		for (let i = 0; i < 8; i++) {
			const id = env.drawer()!;
			const sketch = env.pad(id);
			ids.add(sketch.props.id);
			expect(sketch.props.colors).toEqual([COLORS[Number(id.slice(1)) - 1]]);
			// Only this phone has a pad; the others are told whose turn it is.
			expect(env.players.filter((p) => env.pad(p.id))).toHaveLength(1);
			expect(env.phaseText()).toContain(`${env.players.find((p) => p.id === id).name} sta disegnando`);
			order.push(env.stroke());
		}
		expect(order).toEqual(["p1", "p2", "p3", "p4", "p1", "p2", "p3", "p4"]);
		expect(ids.size).toBe(8); // a fresh sketch id, so a blank pad, every turn
		expect(env.drawer()).toBeUndefined();
		const svg = env.drawingSvg();
		for (const c of COLORS.slice(0, 4)) expect(svg).toContain(c);
		expect(svg.match(/<path/g)).toHaveLength(8);
		expect(env.phaseText()).toContain("Chi è l'intruso?");
	});

	test("a turn takes one stroke: other numbers, undo, clear and other players are ignored", () => {
		const env = setup(4);
		env.tap("p1", "start");
		const control = env.pad("p1").props.id;
		env.sketch("p2", control, "change", { stroke: 1, points: [[0.5, 0.5, 0]] }); // not their turn
		env.sketch("p2", control, "submit", { stroke: 1 });
		expect(env.drawer()).toBe("p1");
		expect(nodes(env.view(), "image").length).toBeGreaterThan(0);
		expect(env.drawingSvg()).not.toContain("<path");

		env.sketch("p1", control, "change", { stroke: 3, size: 8, points: [[0.1, 0.1, 0], [0.2, 0.2, 10]] });
		env.sketch("p1", control, "change", { stroke: 4, size: 8, points: [[0.9, 0.9, 0], [0.8, 0.8, 10]] }); // a second stroke
		env.sketch("p1", control, "tap", "undo");
		env.sketch("p1", control, "tap", "clear");
		env.sketch("p1", control, "submit", { stroke: 4 }); // not the stroke it started
		expect(env.drawer()).toBe("p1");
		env.sketch("p1", control, "change", { stroke: 3, size: 8, points: [[0.3, 0.3, 20]] });
		env.sketch("p1", control, "submit", { stroke: 3 });
		// Turn over at once: the next phone has the pad, p1's has none.
		expect(env.pad("p1")).toBeUndefined();
		expect(env.drawer()).toBe("p2");

		const svg = env.drawingSvg();
		expect(svg.match(/<path/g)).toHaveLength(1);
		expect(svg).not.toContain("0.9");
		// Late events from the finished turn change nothing.
		env.sketch("p1", control, "change", { stroke: 3, points: [[0.7, 0.7, 40]] });
		expect(env.drawingSvg()).toBe(svg);
	});

	test("the kiosk colour comes from the player, not the event, and bad points are dropped", () => {
		const env = setup(3);
		env.tap("p1", "start");
		const control = env.pad("p1").props.id;
		env.sketch("p1", control, "change", {
			stroke: 1,
			color: '"/><script>',
			size: 999,
			points: [[0.5, 0.5, 0], ["x", 1], null, [2, -1, 5], [Number.NaN, 0.1]],
		});
		const svg = env.drawingSvg();
		expect(svg).not.toContain("script");
		expect(svg).toContain(COLORS[0]);
		expect(svg).toContain('stroke-width="8"');
		expect(svg).toContain("360 0"); // (2, -1) is clamped to the corner of the 360x270 box
	});

	test("the kiosk image is 4:3, white, and scaled like the phone (brush in 360 px units)", () => {
		const env = setup(3);
		env.tap("p1", "start");
		env.stroke();
		const svg = env.drawingSvg();
		expect(svg).toContain('viewBox="0 0 360 270"');
		expect(svg).toContain('fill="#fff"');
		expect(svg).toContain('stroke-width="8"');
		const src = env.image().src as string;
		expect(src.startsWith("data:image/svg+xml")).toBe(true);
	});
});

describe("odd one out: voting and scoring", () => {
	function toVote(count = 4) {
		const env = setup(count);
		env.tap("p1", "start");
		for (let i = 0; i < count * 2; i++) env.stroke();
		return env;
	}

	test("phones list everyone but themselves, by name", () => {
		const env = toVote(4);
		for (const p of env.players) {
			const choice = nodes(env.layoutOf(p.id), "choice")[0];
			expect(choice.props.id).toBe("vote");
			const labels = choice.props.options.map((o: any) => o.label).sort();
			expect(labels).toEqual(env.players.filter((q) => q.id !== p.id).map((q) => q.name).sort());
			expect(choice.props.options.map((o: any) => o.id)).not.toContain(p.id);
		}
	});

	test("you cannot vote for yourself, nor twice", () => {
		const env = toVote(4);
		env.tap("p1", "vote", "p1");
		expect(texts(env.layoutOf("p1"))).not.toContain("Hai votato Ada");
		env.tap("p1", "vote", "p2");
		env.tap("p1", "vote", "p3");
		expect(texts(env.layoutOf("p1"))).toContain("Hai votato Bo");
	});

	test("not caught: the odd one out scores 2", () => {
		const env = toVote(4);
		const odd = env.odd();
		const [a, b, c] = env.players.map((p) => p.id).filter((id) => id !== odd);
		// Everyone suspects `a`, except `a`, who picks someone else.
		vote(env, { [b]: a, [c]: a, [odd]: a, [a]: b });
		expect(env.layoutOf(odd)).toBeDefined();
		expect(texts(env.layoutOf(odd))).toContain("Li hai fregati tutti!");
		expect(texts(env.layoutOf(odd))).toContain("+2");
		expect(env.phaseText()).toContain("Non scoperto.");
		for (const p of env.players) expect(texts(env.layoutOf(p.id))).toContain(`Il tuo punteggio: ${p.id === odd ? 2 : 0}`);
	});

	test("a tie for the most votes counts as not caught", () => {
		const env = toVote(4);
		const odd = env.odd();
		const [a, b, c] = env.players.map((p) => p.id).filter((id) => id !== odd);
		vote(env, { [a]: odd, [b]: odd, [c]: a, [odd]: a }); // odd 2, a 2
		expect(texts(env.layoutOf(odd))).toContain("+2");
		expect(nodes(env.layoutOf(odd), "choice")).toHaveLength(0); // no guess phase
	});

	test("caught and a wrong guess: everyone else scores 1", () => {
		const env = toVote(4);
		const odd = env.odd();
		const [a, b, c] = env.players.map((p) => p.id).filter((id) => id !== odd);
		vote(env, { [a]: odd, [b]: odd, [c]: odd });
		env.tap(odd, "vote", a); // the last vote closes voting
		expect(env.phaseText()).toContain("ed è stato scoperto");
		const choice = nodes(env.layoutOf(odd), "choice")[0];
		expect(choice.props.id).toBe("guess");
		expect(choice.props.options).toHaveLength(4);
		const options: string[] = choice.props.options.map((o: any) => o.id);
		expect(options).toContain(env.word());
		expect(new Set(options).size).toBe(4);
		// Nobody else can guess.
		env.tap(a, "guess", env.word());
		expect(env.phaseText()).toContain("ed è stato scoperto");
		env.tap(odd, "guess", options.find((o) => o !== env.word()));
		for (const p of env.players) expect(texts(env.layoutOf(p.id))).toContain(`Il tuo punteggio: ${p.id === odd ? 0 : 1}`);
		expect(texts(env.layoutOf(odd))).toContain("Scoperto");
		expect(env.phaseText()).toContain("ha sbagliato la parola");
	});

	test("caught but the word is guessed: the odd one out scores 1, the rest nothing", () => {
		const env = toVote(4);
		const odd = env.odd();
		const [a, b, c] = env.players.map((p) => p.id).filter((id) => id !== odd);
		vote(env, { [a]: odd, [b]: odd, [c]: odd, [odd]: a });
		env.tap(odd, "guess", env.word());
		for (const p of env.players) expect(texts(env.layoutOf(p.id))).toContain(`Il tuo punteggio: ${p.id === odd ? 1 : 0}`);
		expect(texts(env.layoutOf(odd))).toContain("+1");
	});

	test("haptics: your turn, caught, win; never awaited", async () => {
		const env = setup(3);
		env.tap("p1", "start");
		expect(env.buzzes).toEqual([["p1", "hit"]]);
		for (let i = 0; i < 6; i++) env.stroke();
		expect(env.buzzes.at(-1)).toEqual(["all", "countdown"]);
		const odd = env.odd();
		env.buzzes.length = 0;
		for (const p of env.players) env.tap(p.id, "vote", p.id === odd ? env.players.find((q) => q.id !== odd && q.id !== p.id)!.id : odd);
		expect(env.buzzes).toContainEqual([odd, "fail"]); // caught
	});
});

describe("odd one out: rounds and the end", () => {
	function finishRound(env: Env, caught = false) {
		for (let i = 0; i < env.players.length * 2; i++) env.stroke();
		const odd = env.odd();
		const others = env.players.map((p) => p.id).filter((id) => id !== odd);
		for (const p of env.players) env.tap(p.id, "vote", caught ? (p.id === odd ? others[0] : odd) : p.id === others[0] ? odd : others[0]);
		if (caught) env.tap(odd, "guess", "nope-not-an-option"); // ignored: not one of the options
		if (caught) elapse(GUESS);
		return odd;
	}

	test("the starting player rotates, three rounds, then final scores and play again", () => {
		const env = setup(3);
		env.tap("p1", "start");
		const starts: string[] = [];
		for (let round = 0; round < 3; round++) {
			starts.push(env.drawer()!);
			finishRound(env);
			expect(nodes(env.layoutOf("p1"), "button")[0].props.label).toBe(round === 2 ? "Classifica finale" : "Round successivo");
			env.tap("p2", "next"); // not the host
			env.tap("p1", "next");
		}
		expect(starts).toEqual(["p1", "p2", "p3"]);
		expect(env.phaseText()).toContain("Classifica finale");
		const best = Math.max(...env.players.map((p) => Number(texts(env.layoutOf(p.id)).find((t) => t.startsWith("Hai totalizzato"))!.match(/\d+/)![0])));
		expect(best).toBeGreaterThan(0); // a not-caught odd one out scored 2 each time
		expect(env.buzzes.filter(([, n]) => n === "win").length).toBeGreaterThan(0);
		expect(nodes(env.layoutOf("p1"), "button")[0].props.id).toBe("restart");

		env.tap("p2", "restart"); // not the host
		expect(env.phaseText()).toContain("Classifica finale");
		env.tap("p1", "restart");
		expect(env.drawer()).toBe("p1");
		for (const p of env.players) expect(texts(env.layoutOf(p.id))).not.toContain("Hai totalizzato 2 punti");
	});

	test("a new word each round", () => {
		const env = setup(3);
		env.tap("p1", "start");
		const words = new Set<string>();
		for (let round = 0; round < 3; round++) {
			words.add(env.word());
			finishRound(env);
			env.tap("p1", "next");
		}
		expect(words.size).toBe(3);
	});
});

describe("odd one out: players leaving and timeouts", () => {
	test("a leaving player's turns are skipped", () => {
		const env = setup(4);
		env.tap("p1", "start");
		env.stroke(); // p1
		expect(env.drawer()).toBe("p2");
		const leaver = env.odd() === "p3" ? "p4" : "p3"; // never the odd one out: that ends the round
		const stays = leaver === "p3" ? "p4" : "p3";
		env.leave(leaver); // not their turn yet
		env.stroke(); // p2
		expect(env.drawer()).toBe(stays);
		env.stroke();
		expect(env.drawer()).toBe("p1");
	});

	test("the drawer leaving hands the pad on at once", () => {
		let env: Env;
		do {
			env = setup(4);
			env.tap("p1", "start");
		} while (env.odd() === "p1"); // the odd one out leaving is a different case, below
		env.leave("p1");
		expect(env.drawer()).toBe("p2");
		expect(env.phaseText()).toContain("Bo sta disegnando");
	});

	test("the odd one out leaving ends the round with no points", () => {
		const env = setup(4);
		env.tap("p1", "start");
		const odd = env.odd();
		env.leave(odd);
		expect(env.phaseText()).toContain("l'intruso, è uscito");
		for (const p of env.players) expect(texts(env.layoutOf(p.id))).toContain("Il tuo punteggio: 0");
		const host = env.players.find((p) => p.host)!.id;
		expect(nodes(env.layoutOf(host), "button")[0].props.id).toBe("next"); // the host can go on
		expect(nodes(env.layoutOf(host), "button")[0].props.label).toBe("Round successivo"); // three are still enough
	});

	test("fewer than three players left ends the round gracefully", () => {
		const env = setup(3);
		env.tap("p1", "start");
		const odd = env.odd();
		const other = env.players.find((p) => p.id !== odd && p.id !== "p1")!.id;
		env.leave(other);
		expect(env.phaseText()).toContain("troppo pochi giocatori");
		expect(env.drawer()).toBeUndefined();
		expect(nodes(env.layoutOf("p1"), "button")[0].props.label).toBe("Classifica finale");
		env.tap("p1", "next");
		expect(env.phaseText()).toContain("Classifica finale");
		env.tap("p1", "restart"); // two players can't restart a round: back to the lobby
		expect(texts(env.layoutOf("p1")).join(" ")).toContain("In attesa di giocatori");
	});

	test("a voter leaving drops their vote requirement", () => {
		const env = setup(4);
		env.tap("p1", "start");
		for (let i = 0; i < 8; i++) env.stroke();
		const odd = env.odd();
		const [a, b, c] = env.players.map((p) => p.id).filter((id) => id !== odd);
		vote(env, { [a]: odd, [b]: odd });
		expect(env.phaseText()).toContain("2 su 4 hanno votato");
		env.leave(c);
		env.tap(odd, "vote", a); // the last one still here
		expect(env.phaseText()).toContain("ed è stato scoperto"); // voting closed: odd got 2 of 3 votes
		expect(nodes(env.layoutOf(odd), "choice")[0].props.id).toBe("guess");
	});

	test("a voter leaving as the last missing vote closes voting", () => {
		const env = setup(4);
		env.tap("p1", "start");
		for (let i = 0; i < 8; i++) env.stroke();
		const odd = env.odd();
		const [a, b, c] = env.players.map((p) => p.id).filter((id) => id !== odd);
		vote(env, { [a]: odd, [b]: odd, [odd]: a });
		env.leave(c);
		expect(nodes(env.layoutOf(odd), "choice")[0].props.id).toBe("guess");
	});

	test("an idle phone does not stall: turns, votes and guesses time out", () => {
		const env = setup(4);
		env.tap("p1", "start");
		elapse(TURN); // p1 never draws
		expect(env.drawer()).toBe("p2");
		const control = env.pad("p2").props.id;
		env.sketch("p2", control, "change", { stroke: 1, size: 4, points: [[0.2, 0.2, 0], [0.3, 0.3, 10]] }); // draws but never submits
		elapse(TURN);
		expect(env.drawer()).toBe("p3");
		for (let i = 0; i < 6; i++) elapse(TURN);
		expect(env.phaseText()).toContain("Chi è l'intruso?");
		expect(env.drawingSvg().match(/<path/g)).toHaveLength(1); // the idle turns left no marks

		const odd = env.odd();
		const [a, b] = env.players.map((p) => p.id).filter((id) => id !== odd);
		vote(env, { [a]: odd, [b]: odd });
		elapse(VOTE); // two silent players; odd has 2 of 2 votes: caught
		expect(nodes(env.layoutOf(odd), "choice")[0].props.id).toBe("guess");
		elapse(GUESS); // no guess counts as wrong
		for (const p of env.players) expect(texts(env.layoutOf(p.id))).toContain(`Il tuo punteggio: ${p.id === odd ? 0 : 1}`);
	});

	test("nobody voting at all is not a catch", () => {
		const env = setup(3);
		env.tap("p1", "start");
		for (let i = 0; i < 6; i++) elapse(TURN);
		elapse(VOTE);
		const odd = env.odd();
		expect(texts(env.layoutOf(odd))).toContain("Li hai fregati tutti!");
	});
});

function t0Category(env: Env) {
	return texts(env.layoutOf("p1"))[0].replace("Categoria: ", "");
}
