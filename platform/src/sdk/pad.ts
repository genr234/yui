import { writable } from "svelte/store";
import { bridge } from "./bridge";

/** One phone in the current play session (contracts §2 Player, plus speaker). */
export type PadPlayer = {
	id: string;
	slot: number | null;
	name: string;
	color: string;
	host: boolean;
	speaker?: boolean;
	connected: boolean;
};

/** The kiosk's play session as the overlay sees it; `session` is null when none is open. */
export type PadState = {
	session: { code: string; join_url: string } | null;
	players: PadPlayer[];
};

const empty: PadState = { session: null, players: [] };

/**
 * Play session and players for the shell (join card, P1–P4 chips).
 * `pad.state` answers `pad.players`; updates arrive as the `pad.players` push
 * event. Before the controller implements them this reports no session.
 */
export const pad = {
	state: () =>
		bridge
			.send<PadState | null>("pad.players")
			.then((state) => normalize(state))
			.catch(() => empty),
	onState: (fn: (state: PadState) => void) =>
		bridge.on<PadState>("pad.players", (state) => fn(normalize(state))),
	/**
	 * Start or end phone play. Only one runs at a time: a second call while one
	 * is in flight gets the same promise, whichever button it came from.
	 * Resolves to the new state, or null when it failed; the error is in
	 * `padAction`. Never rejects.
	 */
	run: (kind: "start" | "end") => runPadAction(kind),
	/** True while the PIN screen or Settings is open. The controller stores it and later drops pointer/text input. */
	setSensitive: (active: boolean) =>
		bridge.send("pad.sensitive", { active }).then(() => undefined).catch(() => undefined),
};

/** The start/end action shared by every play button: busy while one runs, and the last error. */
export const padAction = writable<{ busy: boolean; error: string }>({ busy: false, error: "" });

let inFlight: Promise<PadState | null> | null = null;

function runPadAction(kind: "start" | "end"): Promise<PadState | null> {
	if (inFlight) return inFlight;
	padAction.set({ busy: true, error: "" });
	const method = kind === "start" ? "pad.session.start" : "pad.session.end";
	inFlight = bridge
		.send<PadState | null>(method)
		.then((state) => {
			padAction.set({ busy: false, error: "" });
			return normalize(state);
		})
		.catch((err: unknown) => {
			const message = err instanceof Error && err.message ? err.message : "Could not change phone play";
			padAction.set({ busy: false, error: message });
			return null;
		})
		.finally(() => {
			inFlight = null;
		});
	return inFlight;
}

function normalize(state: PadState | null | undefined): PadState {
	if (!state || typeof state !== "object") return empty;
	return {
		session: state.session?.code ? state.session : null,
		players: Array.isArray(state.players) ? state.players : [],
	};
}

/** Contract colour names → swatches that read on both the chrome and white cards. */
export const playerSwatch: Record<string, string> = {
	blue: "#2f76dc",
	red: "#d84a4a",
	green: "#1f9d68",
	yellow: "#e2a60f",
	purple: "#6558d8",
	orange: "#e5761f",
	pink: "#d64f95",
	teal: "#0f9f8f",
};
