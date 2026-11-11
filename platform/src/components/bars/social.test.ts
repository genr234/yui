import { describe, expect, test } from "bun:test";
import {
	BUBBLE_MS, BUTTON_DOTS, COMBO_HOLD_MS, COMBO_WINDOW_MS, EMOTE_IDS, HYPE_COMBO, HYPE_DECAY_PER_S, HYPE_REACTION,
	MAX_BUBBLES_PER_PLAYER, MIN_REACTION_GAP_MS, POKE_MS, SocialState, decayHype, dotsLit, isEmote,
} from "./social";

describe("emotes", () => {
	test("the contract's sixteen ids, nothing else", () => {
		expect(EMOTE_IDS).toHaveLength(16);
		expect(isEmote("heartBroken")).toBe(true);
		expect(isEmote("emote_heart")).toBe(false);
		expect(isEmote("../x")).toBe(false);
		expect(isEmote(3)).toBe(false);
	});
});

describe("reactions", () => {
	test("a bubble lives BUBBLE_MS", () => {
		const s = new SocialState();
		expect(s.reaction("a", "heart", 1000)).toBe(true);
		expect(s.snapshot(1000 + BUBBLE_MS - 1).bubbles).toHaveLength(1);
		expect(s.snapshot(1000 + BUBBLE_MS).bubbles).toHaveLength(0);
	});

	test("unknown emotes and empty players are dropped", () => {
		const s = new SocialState();
		expect(s.reaction("a", "nope", 0)).toBe(false);
		expect(s.reaction("", "heart", 0)).toBe(false);
		expect(s.snapshot(0).bubbles).toHaveLength(0);
	});

	test("one player cannot send faster than the minimum gap", () => {
		const s = new SocialState();
		expect(s.reaction("a", "heart", 0)).toBe(true);
		expect(s.reaction("a", "heart", MIN_REACTION_GAP_MS - 1)).toBe(false);
		expect(s.reaction("a", "heart", MIN_REACTION_GAP_MS)).toBe(true);
		expect(s.reaction("b", "heart", 10)).toBe(true); // others are unaffected
	});

	test("at most MAX_BUBBLES_PER_PLAYER live balloons; the oldest goes", () => {
		const s = new SocialState();
		const emotes = ["heart", "star", "idea", "music", "laugh"];
		emotes.forEach((e, i) => s.reaction("a", e, i * 200));
		const mine = s.snapshot(1000).bubbles.filter((b) => b.playerId === "a");
		expect(mine).toHaveLength(MAX_BUBBLES_PER_PLAYER);
		expect(mine.map((b) => b.emote)).toEqual(["idea", "music", "laugh"]);
	});
});

describe("combo", () => {
	test("the same emote from two different players inside the window", () => {
		const s = new SocialState();
		s.reaction("a", "stars", 0);
		expect(s.snapshot(0).combo).toBeNull();
		s.reaction("b", "stars", 500);
		expect(s.snapshot(500).combo).toMatchObject({ emote: "stars", count: 2 });
		s.reaction("c", "stars", 900);
		expect(s.snapshot(900).combo).toMatchObject({ emote: "stars", count: 3 });
	});

	test("one player repeating an emote is not a combo", () => {
		const s = new SocialState();
		s.reaction("a", "stars", 0);
		s.reaction("a", "stars", 300);
		s.reaction("a", "stars", 600);
		expect(s.snapshot(600).combo).toBeNull();
	});

	test("different emotes do not combine, and the window closes", () => {
		const s = new SocialState();
		s.reaction("a", "heart", 0);
		s.reaction("b", "star", 100);
		expect(s.snapshot(100).combo).toBeNull();
		s.reaction("c", "heart", COMBO_WINDOW_MS + 1);
		expect(s.snapshot(COMBO_WINDOW_MS + 1).combo).toBeNull();
		s.reaction("b", "heart", COMBO_WINDOW_MS + 500); // c and b within the window of each other
		expect(s.snapshot(COMBO_WINDOW_MS + 500).combo?.count).toBe(2);
	});

	test("the banner is held COMBO_HOLD_MS after the last reaction", () => {
		const s = new SocialState();
		s.reaction("a", "heart", 0);
		s.reaction("b", "heart", 100);
		expect(s.snapshot(100 + COMBO_HOLD_MS - 1).combo).not.toBeNull();
		expect(s.snapshot(100 + COMBO_HOLD_MS).combo).toBeNull();
	});

	test("joining an active combo keeps its key (no re-pop), a new emote starts a new one", () => {
		const s = new SocialState();
		s.reaction("a", "heart", 0);
		s.reaction("b", "heart", 100);
		const first = s.snapshot(100).combo!;
		s.reaction("c", "heart", 300);
		expect(s.snapshot(300).combo!.key).toBe(first.key);
		s.reaction("a", "star", 700);
		s.reaction("b", "star", 900);
		expect(s.snapshot(900).combo).toMatchObject({ emote: "star", count: 2 });
		expect(s.snapshot(900).combo!.key).not.toBe(first.key);
	});
});

describe("hype", () => {
	test("each reaction fills, combos fill more, time drains", () => {
		const s = new SocialState();
		s.reaction("a", "heart", 0);
		expect(s.snapshot(0).hype).toBeCloseTo(HYPE_REACTION);
		s.reaction("b", "heart", 0);
		expect(s.snapshot(0).hype).toBeCloseTo(HYPE_REACTION * 2 + HYPE_COMBO);
		expect(s.snapshot(1000).hype).toBeCloseTo(HYPE_REACTION * 2 + HYPE_COMBO - HYPE_DECAY_PER_S);
		expect(s.snapshot(60_000).hype).toBe(0);
	});

	test("never above 1 or below 0", () => {
		const s = new SocialState();
		for (let i = 0; i < 40; i++) {
			s.reaction("a", "heart", i * 130);
			s.reaction("b", "heart", i * 130 + 10);
			s.reaction("c", "heart", i * 130 + 20);
		}
		expect(s.snapshot(40 * 130).hype).toBeLessThanOrEqual(1);
		expect(decayHype(0.2, 10_000)).toBe(0);
		expect(decayHype(0.5, -100)).toBe(0.5);
	});

	test("expire reports whether anything is still moving", () => {
		const s = new SocialState();
		expect(s.expire(0)).toBe(false);
		s.reaction("a", "heart", 0);
		expect(s.expire(10)).toBe(true);
		expect(s.expire(60_000)).toBe(false);
	});
});

describe("pokes", () => {
	test("last POKE_MS; self and empty pokes are ignored", () => {
		const s = new SocialState();
		expect(s.poke("a", "a", 0)).toBe(false);
		expect(s.poke("", "b", 0)).toBe(false);
		expect(s.poke("a", "b", 0)).toBe(true);
		expect(s.snapshot(POKE_MS - 1).pokes).toHaveLength(1);
		expect(s.snapshot(POKE_MS).pokes).toHaveLength(0);
	});
});

describe("held buttons", () => {
	test("dots follow the mask; zero clears the player", () => {
		const s = new SocialState();
		s.input([{ player_id: "a", buttons: 1 | (1 << 13) }, { player_id: "b", buttons: "x" }, { buttons: 3 }]);
		expect(s.snapshot(0).held).toEqual({ a: 1 | (1 << 13) });
		s.input([{ player_id: "a", buttons: 0 }]);
		expect(s.snapshot(0).held).toEqual({});
		s.input("nope");
		expect(s.snapshot(0).held).toEqual({});
	});

	test("which dot lights for which bit", () => {
		const ids = BUTTON_DOTS.map((d) => d.id);
		const lit = (mask: number) => dotsLit(mask).map((on, i) => (on ? ids[i] : null)).filter(Boolean);
		expect(lit(0)).toEqual([]);
		expect(lit(1 << 0)).toEqual(["south"]); // South
		expect(lit((1 << 1) | (1 << 3))).toEqual(["east", "north"]);
		expect(lit(1 << 16)).toEqual(["dpad"]); // Right
		expect(lit((1 << 5) | (1 << 6))).toEqual(["shoulder"]); // R1 and L2 share a dot
		expect(lit(1 << 9)).toEqual(["menu"]); // Start
		expect(lit(1 << 10)).toEqual([]); // Home is the phone's own sheet
		expect(lit(0xffffffff)).toHaveLength(BUTTON_DOTS.length); // high bit must not break the shifts
	});

	test("a player who left goes dark", () => {
		const s = new SocialState();
		s.input([{ player_id: "a", buttons: 5 }]);
		s.clearPlayer("a");
		expect(s.snapshot(0).held).toEqual({});
	});
});
