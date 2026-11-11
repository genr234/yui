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
	/** "left" or "right" while two speaker phones form a stereo pair; absent for a mono speaker. */
	speaker_channel?: "left" | "right" | null;
	/** Playing as a Yui account this session (plan 10); the account itself is never sent. */
	account_linked?: boolean;
	connected: boolean;
	/** Last control round trip in ms and accepted input frames per second (absent until measured). */
	rtt_ms?: number;
	input_hz?: number;
	/** A creature recipe, or from older pads an emoji or a selfie data URL (contracts §2). Render it with `mountAvatar` from `avatar/`. */
	avatar?: string | null;
};

/** How a phone's link feels: good under 60 ms, ok under 150 ms, slow above. */
export function linkQuality(rttMs: number | undefined): "good" | "ok" | "slow" | null {
	if (typeof rttMs !== "number") return null;
	if (rttMs < 60) return "good";
	if (rttMs < 150) return "ok";
	return "slow";
}

/** `pad.cursor`: the phone trackpad's cursor, in CSS pixels of the page viewport. */
export type PadCursor = {
	player_id?: string;
	x: number;
	y: number;
	visible: boolean;
	color?: string;
	click?: boolean;
};

/** `pad.remote`: a button on the host's phone remote. */
export type PadRemoteAction = { player_id: string; action: "back" | "home" | "open_yui" };

/** `pad.home`: a player pressed the Home button on their phone. */
export type PadHome = { player_id: string; via: "button" | "system" };

/** The kiosk's play session as the overlay sees it; `session` is null when none is open. */
export type PadState = {
	/** `lobby_open`: anyone with the code can join; otherwise phones must scan the current QR. */
	session: {
		code: string;
		join_url: string;
		lobby_open?: boolean;
		/** The kiosk's own hotspot while it is on: `qr` is a WIFI: join code. */
		wifi?: PadWifi;
		/** The hotspot is still starting; `wifi` follows when it is up. */
		wifi_pending?: boolean;
	} | null;
	players: PadPlayer[];
	/** Controller order: a lower `seq` than one already shown is stale. */
	seq?: number;
};

/** The kiosk hotspot: name, passphrase (for phones that cannot scan) and its WIFI: QR text. */
export type PadWifi = { ssid: string; password?: string; qr: string };

const empty: PadState = { session: null, players: [] };

/**
 * Play session and players for the shell (join card, P1–P4 chips).
 * `pad.state` answers `pad.players`; updates arrive as the `pad.players` push
 * event. Before the controller implements them this reports no session.
 */
export const pad = {
	/** The latest state; asks the controller and keeps whichever is newer. */
	state: () => refresh(),
	/** Every newer state, from events and from replies to pad commands alike. */
	onState: (fn: (state: PadState) => void) => subscribe(fn),
	/**
	 * The session ended by itself because nobody was there (no phones, or
	 * nobody played for a while). Any native game it ran is being closed.
	 */
	onAbandoned: (fn: () => void) => bridge.on("pad.abandoned", () => fn()),
	/**
	 * Start or end phone play. Only one runs at a time: a second call while one
	 * is in flight gets the same promise, whichever button it came from.
	 * Resolves to the new state, or null when it failed; the error is in
	 * `padAction`. Never rejects.
	 */
	run: (kind: "start" | "end") => runPadAction(kind),
	/** Let anyone with the code join (open) or require the QR (closed). */
	setLobby: (open: boolean) => playerAction("pad.lobby", { open }),
	/** Remove a phone from the session. */
	kick: (playerId: string) => playerAction("pad.kick", { player_id: playerId }),
	/** Give the host role (the remote and the Home sheet) to another phone. */
	makeHost: (playerId: string) => playerAction("pad.host", { player_id: playerId }),
	/**
	 * Play the kiosk's sound on this phone. With a channel the phone pairs with
	 * the current speaker as that half of a stereo pair.
	 */
	makeSpeaker: (playerId: string, channel?: "left" | "right") =>
		playerAction("pad.speaker", channel ? { player_id: playerId, channel } : { player_id: playerId }),
	/** True while the PIN screen or Settings is open. The controller drops phone pointer and text input meanwhile (D9). */
	setSensitive: (active: boolean) =>
		bridge.send("pad.sensitive", { active }).then(() => undefined).catch(() => undefined),
	/** A player pressed Home on their phone: bring Yui forward. */
	onHome: (fn: (home: PadHome) => void) => bridge.on<PadHome>("pad.home", fn),
	/** The host's remote: back, home (the controller also loads the start page), open Yui. */
	onRemote: (fn: (action: PadRemoteAction) => void) => bridge.on<PadRemoteAction>("pad.remote", fn),
	/** Cursor moves and clicks from a phone trackpad. */
	onCursor: (fn: (cursor: PadCursor) => void) => bridge.on<PadCursor>("pad.cursor", fn),
	/** Whether any phone shows a keyboard; the on-screen keyboard stays hidden while one does. */
	onKeyboard: (fn: (active: boolean) => void) =>
		bridge.on<{ active?: boolean }>("pad.keyboard", (data) => fn(Boolean(data?.active))),
};

/** True while a phone keyboard is active (`pad.keyboard`). */
export const padKeyboard = writable(false);

/** How long a still cursor stays on screen. */
export const cursorIdleMs = 3000;

/** Colour for a player's cursor: their swatch, or the Yui blue token. */
export function cursorColor(color: string | undefined): string {
	return (color && playerSwatch[color]) || "var(--yui-blue)";
}

/**
 * What the remote's buttons do in the overlay. `back` closes Yui when it is
 * open and otherwise goes back in the page; `home` closes Yui (the controller
 * loads the start page); `open_yui` toggles Yui.
 */
export function remoteIntent(
	action: PadRemoteAction["action"],
	yuiOpen: boolean,
): "open" | "close" | "history-back" | "none" {
	switch (action) {
		case "open_yui":
			return yuiOpen ? "close" : "open";
		case "back":
			return yuiOpen ? "close" : "history-back";
		case "home":
			return yuiOpen ? "close" : "none";
		default:
			return "none";
	}
}

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
			return accept(state);
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

/** Session admin calls: resolve to the new state, or null with the error in `padAction`. */
function playerAction(method: string, params: Record<string, unknown>): Promise<PadState | null> {
	return bridge
		.send<PadState | null>(method, params)
		.then((state) => {
			padAction.update((current) => ({ ...current, error: "" }));
			return accept(state);
		})
		.catch((err: unknown) => {
			const message = err instanceof Error && err.message ? err.message : "That didn't work";
			padAction.update((current) => ({ ...current, error: message }));
			return null;
		});
}

function normalize(state: PadState | null | undefined): PadState {
	if (!state || typeof state !== "object") return empty;
	return {
		session: state.session?.code ? state.session : null,
		players: Array.isArray(state.players) ? state.players : [],
		seq: typeof state.seq === "number" ? state.seq : undefined,
	};
}

/*
 * One shared copy of the session for every view (top bar, attract screen,
 * Apps, app join cards). Snapshots reach it three ways: `pad.players`
 * events, replies to pad commands, and fresh fetches. They can arrive out
 * of order, so one older than the newest seen is dropped; without that a
 * late "no session" from the last session could hide the new QR. An event
 * lost while the bridge reconnected is made up for by a fetch on reopen,
 * and a watchdog fetches again if a session goes quiet for longer than the
 * QR's 30-second rotation.
 */
let current: PadState = empty;
let lastSeq = -1;
let lastUpdate = 0;
const subscribers = new Set<(state: PadState) => void>();
let stopFeed: (() => void) | null = null;

/** Seconds a live session may go without an update before it is fetched again. */
const QUIET_MS = 40_000;

function accept(raw: PadState | null | undefined): PadState {
	const next = normalize(raw);
	if (typeof next.seq === "number") {
		if (next.seq <= lastSeq) return current;
		lastSeq = next.seq;
	}
	current = next;
	lastUpdate = Date.now();
	for (const fn of [...subscribers]) {
		try {
			fn(current);
		} catch (error) {
			console.error("pad state listener failed", error);
		}
	}
	return current;
}

function refresh(): Promise<PadState> {
	return bridge
		.send<PadState | null>("pad.players")
		.then((state) => accept(state))
		.catch(() => current);
}

function subscribe(fn: (state: PadState) => void): () => void {
	subscribers.add(fn);
	if (!stopFeed) stopFeed = startFeed();
	return () => {
		subscribers.delete(fn);
		if (subscribers.size === 0 && stopFeed) {
			stopFeed();
			stopFeed = null;
		}
	};
}

function startFeed(): () => void {
	const offEvent = bridge.on<PadState>("pad.players", (state) => {
		accept(state);
	});
	// A restarted controller counts from zero again; its first answer is the truth.
	const offOpen =
		typeof bridge.onOpen === "function"
			? bridge.onOpen(() => {
					lastSeq = -1;
					void refresh();
				})
			: () => {};
	const timer =
		typeof window !== "undefined"
			? window.setInterval(() => {
					if (current.session && Date.now() - lastUpdate > QUIET_MS) void refresh();
				}, 10_000)
			: undefined;
	return () => {
		offEvent();
		offOpen();
		if (timer !== undefined) window.clearInterval(timer);
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
