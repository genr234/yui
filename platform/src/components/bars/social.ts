/**
 * Live "social" state for the letterbox bars: reactions, combos, hype, pokes
 * and held buttons. `SocialState` is pure (every method takes the time), so
 * it is tested without a clock; `social` wraps one instance in a Svelte
 * store fed by the bridge events `pad.reaction`, `pad.poke` and `pad.input`.
 *
 * Event shapes (contracts, "Reactions and pokes"):
 *   pad.reaction {player_id, emote}
 *   pad.poke     {from, to}
 *   pad.input    {players: [{player_id, buttons}]}   (button bitmask, <= 10 Hz, changed players only)
 */
import { writable, type Readable } from "svelte/store";
import { bridge } from "../../sdk/bridge";

/** The sixteen emotes, as Kenney's file names without `emote_`. */
export const EMOTE_IDS = [
	"heart", "hearts", "laugh", "faceHappy", "faceSad", "faceAngry", "exclamation", "exclamations",
	"question", "star", "stars", "idea", "music", "sleeps", "anger", "heartBroken",
] as const;
export type EmoteId = (typeof EMOTE_IDS)[number];

const emoteSet = new Set<string>(EMOTE_IDS);
export function isEmote(value: unknown): value is EmoteId {
	return typeof value === "string" && emoteSet.has(value);
}

/** A balloon floats and fades for this long. */
export const BUBBLE_MS = 1800;
/** Most balloons on screen per player (the oldest goes first). */
export const MAX_BUBBLES_PER_PLAYER = 3;
/** One player cannot send reactions faster than this (a stuck phone must not flood the bar). */
export const MIN_REACTION_GAP_MS = 120;
/** The same emote from two or more different players inside this window is a combo. */
export const COMBO_WINDOW_MS = 2000;
/** A combo banner stays up this long after its last reaction. */
export const COMBO_HOLD_MS = 2200;
/** A poke wiggle lasts this long. */
export const POKE_MS = 1000;
/** Hype: each reaction adds this much (of 1), each extra player in a combo adds `HYPE_COMBO`; it drains at `HYPE_DECAY_PER_S`. */
export const HYPE_REACTION = 0.05;
export const HYPE_COMBO = 0.12;
export const HYPE_DECAY_PER_S = 0.1;
/** Cap on remembered players with held buttons, so a bad feed cannot grow it forever. */
const MAX_HELD = 64;

export type Bubble = { key: number; playerId: string; emote: EmoteId; at: number };
export type Combo = { key: number; emote: EmoteId; count: number; at: number };
export type Poke = { key: number; from: string; to: string; at: number };

export type SocialSnapshot = {
	bubbles: Bubble[];
	combo: Combo | null;
	/** 0..1 at snapshot time. */
	hype: number;
	pokes: Poke[];
	held: Record<string, number>;
};

/** Hype after `elapsedMs` of draining from `level`. */
export function decayHype(level: number, elapsedMs: number): number {
	return Math.max(0, level - (HYPE_DECAY_PER_S * Math.max(0, elapsedMs)) / 1000);
}

/** Small dots that light while buttons are held; bit numbers are `controller/internal/pad/protocol/input.go`. */
export const BUTTON_DOTS = [
	{ id: "dpad", label: "Direction pad", bits: [13, 14, 15, 16] },
	{ id: "south", label: "South button", bits: [0] },
	{ id: "east", label: "East button", bits: [1] },
	{ id: "west", label: "West button", bits: [2] },
	{ id: "north", label: "North button", bits: [3] },
	{ id: "shoulder", label: "Shoulder or trigger", bits: [4, 5, 6, 7] },
	{ id: "menu", label: "Start or select", bits: [8, 9] },
] as const;

/** Which of `BUTTON_DOTS` are lit for this mask. */
export function dotsLit(mask: number): boolean[] {
	const m = mask >>> 0;
	return BUTTON_DOTS.map((dot) => dot.bits.some((bit) => ((m >>> bit) & 1) === 1));
}

export class SocialState {
	private bubbles: Bubble[] = [];
	private recent: Array<{ playerId: string; emote: EmoteId; at: number }> = [];
	private lastReaction = new Map<string, number>();
	private combo: Combo | null = null;
	private pokes: Poke[] = [];
	private held = new Map<string, number>();
	private hypeLevel = 0;
	private hypeAt = 0;
	private seq = 0;

	/** Returns false when the reaction was dropped (unknown emote, or too soon after this player's last). */
	reaction(playerId: string, emote: unknown, now: number): boolean {
		if (!playerId || !isEmote(emote)) return false;
		const last = this.lastReaction.get(playerId);
		if (last !== undefined && now - last < MIN_REACTION_GAP_MS && now >= last) return false;
		this.lastReaction.set(playerId, now);
		this.expire(now);

		// Per-player cap: keep room for the new one.
		const mine = this.bubbles.filter((b) => b.playerId === playerId);
		for (const old of mine.slice(0, Math.max(0, mine.length - (MAX_BUBBLES_PER_PLAYER - 1)))) {
			this.bubbles = this.bubbles.filter((b) => b !== old);
		}
		this.bubbles.push({ key: ++this.seq, playerId, emote, at: now });

		this.recent.push({ playerId, emote, at: now });
		const same = new Set(this.recent.filter((r) => r.emote === emote && now - r.at <= COMBO_WINDOW_MS).map((r) => r.playerId));
		let gain = HYPE_REACTION;
		if (same.size >= 2) {
			const count = same.size;
			if (this.combo && this.combo.emote === emote) this.combo = { ...this.combo, count: Math.max(this.combo.count, count), at: now };
			else this.combo = { key: ++this.seq, emote, count, at: now };
			gain += HYPE_COMBO * (count - 1);
		}
		this.hypeLevel = Math.min(1, decayHype(this.hypeLevel, now - this.hypeAt) + gain);
		this.hypeAt = now;
		return true;
	}

	poke(from: string, to: string, now: number): boolean {
		if (!from || !to || from === to) return false;
		this.expire(now);
		this.pokes.push({ key: ++this.seq, from, to, at: now });
		if (this.pokes.length > 16) this.pokes.shift();
		return true;
	}

	/** Apply a `pad.input` batch; a zero mask clears that player. */
	input(players: unknown): void {
		if (!Array.isArray(players)) return;
		for (const entry of players) {
			const id = entry?.player_id;
			const buttons = entry?.buttons;
			if (typeof id !== "string" || typeof buttons !== "number" || !Number.isFinite(buttons)) continue;
			if (buttons === 0) this.held.delete(id);
			else if (this.held.has(id) || this.held.size < MAX_HELD) this.held.set(id, buttons >>> 0);
		}
	}

	/** Forget a player who left or lost their link (their dots go dark). */
	clearPlayer(id: string): void {
		this.held.delete(id);
	}

	/** Drop whatever has run its course. Returns true when anything is still animating or draining. */
	expire(now: number): boolean {
		this.bubbles = this.bubbles.filter((b) => now - b.at < BUBBLE_MS);
		this.pokes = this.pokes.filter((p) => now - p.at < POKE_MS);
		this.recent = this.recent.filter((r) => now - r.at <= COMBO_WINDOW_MS);
		if (this.combo && now - this.combo.at >= COMBO_HOLD_MS) this.combo = null;
		return this.bubbles.length > 0 || this.pokes.length > 0 || this.combo !== null || decayHype(this.hypeLevel, now - this.hypeAt) > 0;
	}

	snapshot(now: number): SocialSnapshot {
		this.expire(now);
		return {
			bubbles: [...this.bubbles],
			combo: this.combo,
			hype: decayHype(this.hypeLevel, now - this.hypeAt),
			pokes: [...this.pokes],
			held: Object.fromEntries(this.held),
		};
	}

	reset(): void {
		this.bubbles = [];
		this.recent = [];
		this.lastReaction.clear();
		this.combo = null;
		this.pokes = [];
		this.held.clear();
		this.hypeLevel = 0;
	}
}

// ---- store ----------------------------------------------------------------

const emptySnapshot: SocialSnapshot = { bubbles: [], combo: null, hype: 0, pokes: [], held: {} };
const state = new SocialState();
const store = writable<SocialSnapshot>(emptySnapshot);
let users = 0;
let offs: Array<() => void> = [];
let timer: ReturnType<typeof setInterval> | undefined;

const nowMs = () => (typeof performance !== "undefined" ? performance.now() : Date.now());

function publish() {
	const now = nowMs();
	store.set(state.snapshot(now));
	if (state.expire(now)) {
		timer ??= setInterval(() => {
			const t = nowMs();
			store.set(state.snapshot(t));
			if (!state.expire(t)) {
				clearInterval(timer);
				timer = undefined;
				store.set(state.snapshot(t));
			}
		}, 100);
	}
}

export const social: Readable<SocialSnapshot> & {
	/** Subscribe to the bridge events; returns the unsubscribe. Reference counted, so every bar item may call it. */
	start: () => () => void;
	/** Feed an event by hand (dev simulator, tests). */
	emit: (event: "pad.reaction" | "pad.poke" | "pad.input", data: unknown) => void;
	clearPlayer: (id: string) => void;
} = {
	subscribe: store.subscribe,
	start() {
		if (users++ === 0) {
			offs = [
				bridge.on<{ player_id?: string; emote?: string }>("pad.reaction", (d) => dispatch("pad.reaction", d)),
				bridge.on<{ from?: string; to?: string }>("pad.poke", (d) => dispatch("pad.poke", d)),
				bridge.on<{ players?: unknown }>("pad.input", (d) => dispatch("pad.input", d)),
			];
		}
		let stopped = false;
		return () => {
			if (stopped) return;
			stopped = true;
			if (--users === 0) {
				offs.forEach((off) => off());
				offs = [];
				clearInterval(timer);
				timer = undefined;
				state.reset();
				store.set(emptySnapshot);
			}
		};
	},
	emit: (event, data) => dispatch(event, data),
	clearPlayer(id) {
		state.clearPlayer(id);
		publish();
	},
};

function dispatch(event: string, data: any) {
	const now = nowMs();
	if (event === "pad.reaction") state.reaction(String(data?.player_id ?? ""), data?.emote, now);
	else if (event === "pad.poke") state.poke(String(data?.from ?? ""), String(data?.to ?? ""), now);
	else if (event === "pad.input") state.input(data?.players);
	publish();
}
