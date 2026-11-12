/**
 * Host side of `ctx.surfaces`: a private video view of one window of the
 * app's native program, shown on one player's phone.
 *
 * The overlay checks the `surfaces` permission, attaches the app id, talks to
 * the controller over the bridge (`pad.surfaces.*`) and forwards the
 * `pad.surfaces` push event only to the app whose id matches. Errors from
 * the controller (`YUI_SURFACES: <code>`) are passed through unchanged.
 */
import { bridge } from "../bridge";
import type { YuiPermission, YuiSimpleApp, YuiSurfaceStatus, YuiSurfacesApi } from "./types";

type Ensure = (permission: YuiPermission) => Promise<void>;

export type SurfacesHost = {
	api: YuiSurfacesApi;
	/** Drops every subscription. */
	dispose(): void;
};

function playerId(player: unknown): string | null {
	if (player === null || player === undefined) return null;
	const id = typeof player === "string" ? player : (player as { id?: unknown })?.id;
	if (typeof id !== "string" || !id) throw new Error("YUI_SURFACES: unknown_player");
	return id;
}

export function createSurfacesHost(
	app: YuiSimpleApp,
	ensure: Ensure,
	/** Synchronous check used by `on`, which cannot prompt. */
	requireGranted: (permission: YuiPermission) => void,
): SurfacesHost {
	const offs = new Set<() => void>();
	let disposed = false;

	const api: YuiSurfacesApi = {
		async available() {
			await ensure("surfaces");
			const result = await bridge.send<{ available?: boolean; reason?: "unsupported" | "no_encoder" } | null>(
				"pad.surfaces.available",
			);
			return {
				available: Boolean(result?.available),
				...(result?.reason ? { reason: result.reason } : {}),
			};
		},
		async list() {
			await ensure("surfaces");
			return (await bridge.send<YuiSurfaceStatus[] | null>("pad.surfaces.list", { app_id: app.id })) ?? [];
		},
		async show(surface, player) {
			await ensure("surfaces");
			if (typeof surface !== "string" || !surface) throw new Error("YUI_SURFACES: unknown_surface");
			return bridge.send<YuiSurfaceStatus>("pad.surfaces.show", {
				app_id: app.id,
				surface,
				player_id: playerId(player),
			});
		},
		on(event, handler) {
			requireGranted("surfaces");
			if (event !== "status") throw new Error(`YUI_SURFACES: unknown event ${String(event)}`);
			if (disposed) return () => {};
			const off = bridge.on<{ app_id?: string; surfaces?: YuiSurfaceStatus[] }>("pad.surfaces", (data) => {
				if (disposed || data?.app_id !== app.id) return;
				handler(Array.isArray(data.surfaces) ? data.surfaces : []);
			});
			const stop = () => {
				offs.delete(stop);
				off();
			};
			offs.add(stop);
			return stop;
		},
	};

	return {
		api,
		dispose() {
			disposed = true;
			for (const stop of [...offs]) stop();
		},
	};
}
