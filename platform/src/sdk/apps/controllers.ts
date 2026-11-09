/**
 * Host side of `ctx.controllers` and `ctx.virtualPads` (plans 05 and 07).
 *
 * The overlay owns the privileged half: it checks the `controllers` and
 * `virtual-pads` permissions, attaches the app id, talks to the controller
 * over the bridge (`pad.app.*`, `pad.vpads.*`) and filters `pad.app.input`
 * to the app it belongs to. The app-facing API lives in
 * controllers-runtime.ts and runs in the app sandbox.
 */
import { bridge } from "../bridge";
import type { YuiControllersManifest, YuiKeymap, YuiPermission, YuiSimpleApp } from "./types";
import { validateControllersManifest } from "./validate";

export type ControllersState = {
	session: { code: string } | null;
	players: Array<Record<string, unknown>>;
};

/** A rectangle of the page viewport in CSS pixels (an embedded game's frame). */
export type ViewportRect = { x: number; y: number; w: number; h: number };

type Push = (event: string, data: unknown) => void;

export type ControllersHost = {
	/** Backend calls from the runtime (host ctx or sandbox RPC). */
	call(method: string, args: unknown[]): Promise<unknown>;
	/** Adds a receiver for pushed events ("players", "input"). */
	addPush(push: Push): () => void;
	/**
	 * The app's manifest `controllers` (reported by the sandbox): validated,
	 * then the app takes the hub's app layer until dispose.
	 */
	declare(value: unknown): Promise<boolean>;
	/** The embedded game's frame, for absolute-trackpad keymaps. */
	setRegion(rect: ViewportRect | null): void;
	/** The manifest keymap, once declared. */
	keymap(): YuiKeymap | undefined;
	dispose(): void;
	/** `ctx.virtualPads` backend. */
	virtualPads(method: string, args: unknown[]): Promise<unknown>;
};

type Ensure = (permission: YuiPermission) => Promise<void>;

/** Contract player → what an app sees. The join URL (QR token) never reaches the app. */
export function controllersState(state: any): ControllersState {
	return {
		session: state?.session?.code ? { code: String(state.session.code) } : null,
		players: Array.isArray(state?.players) ? state.players : [],
	};
}

export function createControllersHost(app: YuiSimpleApp, ensure: Ensure): ControllersHost {
	const pushes = new Set<Push>();
	const push: Push = (event, data) => {
		for (const fn of [...pushes]) fn(event, data);
	};
	let started = false;
	let disposed = false;
	let manifest: YuiControllersManifest | null = null;
	let focused = false;
	let frames = false;
	let region: ViewportRect | null = null;
	let regionTimer: ReturnType<typeof setTimeout> | undefined;
	const offs: Array<() => void> = [];

	const focusParams = () => ({
		app_id: app.id,
		name: app.name,
		active: true,
		keymap: manifest?.keymap,
		frames,
		region: region ?? undefined,
	});

	async function sendFocus() {
		if (!manifest || disposed) return;
		await resetControllersFocus();
		if (disposed) return;
		await bridge.send("pad.app.focus", focusParams());
		focused = true;
		if (disposed) {
			// Closed while the focus was in flight: undo it.
			await bridge.send("pad.app.focus", { app_id: app.id, active: false }).catch(() => {});
		}
	}

	async function start() {
		await ensure("controllers");
		if (!started && !disposed) {
			started = true;
			offs.push(
				bridge.on("pad.players", (state) => push("players", controllersState(state))),
				bridge.on<{ app_id?: string }>("pad.app.input", (data) => {
					if (data?.app_id === app.id) push("input", data);
				}),
			);
		}
		return controllersState(await bridge.send("pad.players").catch(() => null));
	}

	function playerId(value: unknown) {
		if (typeof value !== "string" || !value) throw new Error("YUI_CONTROLLERS: player id is required");
		return value;
	}

	return {
		async call(method, args) {
			const [first, second, third] = args;
			switch (method) {
				case "start":
					return start();
				case "layouts":
					await ensure("controllers");
					return bridge.send("pad.app.layout", { app_id: app.id, layouts: first ?? {} });
				case "frames":
					await ensure("controllers");
					frames = Boolean(first);
					if (focused) await sendFocus();
					return undefined;
				case "haptic":
					await ensure("controllers");
					return bridge.send("pad.app.haptic", {
						app_id: app.id,
						player_id: playerId(first),
						// A vocabulary name or custom ms steps; the controller validates.
						...(typeof second === "string" ? { name: second } : { pattern: second }),
						...(typeof (third as any)?.intensity === "number" ? { intensity: (third as any).intensity } : {}),
					});
				case "sound": {
					await ensure("controllers");
					let target = playerId(first);
					if (target === "speaker") {
						// The phone marked as speaker; the controller falls back to the host.
						const state = await bridge.send<any>("pad.players").catch(() => null);
						const speaker = state?.players?.find((p: any) => p?.speaker && p?.connected !== false);
						if (speaker?.id) target = String(speaker.id);
					}
					return bridge.send("pad.app.sound", {
						app_id: app.id,
						player_id: target,
						url: String(second ?? ""),
						volume: typeof third === "number" ? third : undefined,
					});
				}
				default:
					throw new Error(`YUI_CONTROLLERS: unknown call ${method}`);
			}
		},
		addPush(next) {
			pushes.add(next);
			return () => pushes.delete(next);
		},
		async declare(value) {
			if (value === undefined || value === null || disposed) return false;
			validateControllersManifest(value);
			manifest = value as YuiControllersManifest;
			await ensure("controllers");
			await sendFocus();
			return true;
		},
		setRegion(rect) {
			const same =
				rect && region && Math.round(rect.x) === Math.round(region.x) && Math.round(rect.y) === Math.round(region.y) &&
				Math.round(rect.w) === Math.round(region.w) && Math.round(rect.h) === Math.round(region.h);
			if (same || (!rect && !region)) return;
			region = rect ? { x: Math.round(rect.x), y: Math.round(rect.y), w: Math.round(rect.w), h: Math.round(rect.h) } : null;
			clearTimeout(regionTimer);
			regionTimer = setTimeout(() => {
				if (focused) void sendFocus().catch(() => {});
			}, 120);
		},
		keymap() {
			return manifest?.keymap;
		},
		dispose() {
			if (disposed) return;
			disposed = true;
			clearTimeout(regionTimer);
			for (const off of offs) off();
			offs.length = 0;
			pushes.clear();
			if (focused || manifest) {
				void bridge.send("pad.app.focus", { app_id: app.id, active: false }).catch(() => {});
			}
		},
		async virtualPads(method, args) {
			await ensure("virtual-pads");
			const [first, second] = args as [any, any];
			switch (method) {
				case "attach":
					return bridge.send("pad.vpads.attach", {
						app_id: app.id,
						player_id: playerId(first),
						backend: second?.backend ?? "dsu",
						slot: second?.slot,
						pointer_stick: Boolean(second?.pointerStick),
					});
				case "detach":
					return bridge.send("pad.vpads.detach", { app_id: app.id, player_id: playerId(first) });
				case "list":
					return (await bridge.send<unknown[] | null>("pad.vpads.list", { app_id: app.id })) ?? [];
				case "systemPad":
					return bridge.send("pad.vpads.system_pad", { app_id: app.id });
				case "press":
					return bridge.send("pad.vpads.press", { app_id: app.id, button: String(first ?? ""), action: second ?? "tap" });
				default:
					throw new Error(`YUI_VIRTUAL_PADS: unknown call ${method}`);
			}
		},
	};
}

const hosts = new WeakMap<object, ControllersHost>();

/** The controllers host behind a host ctx (for AppHost and the sandbox RPC). */
export function controllersHostFor(ctx: object) {
	return hosts.get(ctx);
}

export function registerControllersHost(ctx: object, host: ControllersHost) {
	hosts.set(ctx, host);
}

let reset: Promise<unknown> | null = null;

/**
 * Clears any app layer left from before the overlay (re)loaded. Runs once;
 * every app focus waits for it, so it can never land after one.
 */
export function resetControllersFocus() {
	reset ??= bridge.send("pad.app.focus", { active: false }).catch(() => undefined);
	return reset;
}

/** Whether a keymap needs the game's frame rectangle (absolute trackpad). */
export function keymapNeedsRegion(keymap: YuiKeymap | undefined) {
	return Boolean(keymap?.controls.some((c) => c.type === "trackpad" && c.mode === "absolute"));
}
