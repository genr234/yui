/**
 * Demo-mode only (VITE_YUI_DEMO): fakes the controller's social events so the
 * bars can be tried without phones. Loaded by SeatChips when the demo bridge
 * is in use; never imported in a real build.
 *
 * In the browser console of the demo page:
 *   __yuiSocialDemo.react("p1", "heart")      one balloon
 *   __yuiSocialDemo.combo("stars")            three players send the same emote
 *   __yuiSocialDemo.poke("p1", "p2")
 *   __yuiSocialDemo.input("p1", 0b11)         hold South + East (0 releases)
 *   __yuiSocialDemo.auto(true)                random reactions, pokes and button mashing
 * Or open the demo with `?social-demo=auto`.
 */
import { pad } from "../../sdk/pad";
import { EMOTE_IDS, social } from "./social";

type Demo = {
	react: (playerId: string, emote: string) => void;
	combo: (emote?: string) => void;
	poke: (from: string, to: string) => void;
	input: (playerId: string, buttons: number) => void;
	auto: (on: boolean) => void;
};

let ids: string[] = [];
let timers: number[] = [];

async function refresh() {
	const state = await pad.state();
	ids = state.players.map((p) => p.id);
}

const pick = <T>(list: readonly T[]): T => list[Math.floor(Math.random() * list.length)];

const demo: Demo = {
	react: (player_id, emote) => social.emit("pad.reaction", { player_id, emote }),
	combo(emote = pick(EMOTE_IDS)) {
		ids.slice(0, 3).forEach((id, i) => window.setTimeout(() => demo.react(id, emote), i * 250));
	},
	poke: (from, to) => social.emit("pad.poke", { from, to }),
	input: (player_id, buttons) => social.emit("pad.input", { players: [{ player_id, buttons }] }),
	auto(on) {
		timers.forEach((t) => window.clearInterval(t));
		timers = [];
		if (!on) return;
		void refresh();
		timers.push(
			window.setInterval(() => ids.length && demo.react(pick(ids), pick(EMOTE_IDS)), 900),
			window.setInterval(() => ids.length > 1 && demo.poke(pick(ids), pick(ids)), 4000),
			window.setInterval(() => demo.combo(), 9000),
			window.setInterval(() => ids.length && demo.input(pick(ids), Math.floor(Math.random() * 0x3ff) & Math.floor(Math.random() * 0x3ff)), 250),
			window.setInterval(refresh, 3000),
		);
	},
};

(window as unknown as { __yuiSocialDemo: Demo }).__yuiSocialDemo = demo;
void refresh();
if (new URLSearchParams(window.location.search).get("social-demo") === "auto") demo.auto(true);
