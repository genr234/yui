/**
 * Host side of `ctx.profiles` (plan 10). The overlay checks the `profiles`
 * permission and attaches the app id; the controller confirms the player's
 * grant with Rails on every call, so nothing here is trusted by the server.
 * The app-facing API lives in profiles-runtime.ts and runs in the sandbox.
 */
import { bridge } from "../bridge";
import type { YuiPermission, YuiSimpleApp } from "./types";

export type ProfilesHost = {
	call(method: string, args: unknown[]): Promise<unknown>;
};

type Ensure = (permission: YuiPermission) => Promise<void>;

export const profileCalls = new Set(["request", "get", "set"]);

export function createProfilesHost(app: YuiSimpleApp, ensure: Ensure): ProfilesHost {
	return {
		async call(method, args) {
			await ensure("profiles");
			const [player, data, version] = args;
			if (typeof player !== "string" || !player) throw new Error("YUI_PROFILES: player id is required");
			const base = { app_id: app.id, player_id: player };
			switch (method) {
				case "request":
					return bridge.send("pad.profile.request", { ...base, app_name: app.name });
				case "get":
					return bridge.send("pad.profile.get", base);
				case "set":
					return bridge.send("pad.profile.set", { ...base, data, version });
				default:
					throw new Error(`YUI_PROFILES: unknown call ${method}`);
			}
		},
	};
}

const hosts = new WeakMap<object, ProfilesHost>();

/** The profiles host behind a host ctx (for the sandbox RPC). Absent when the app did not declare `profiles`. */
export function profilesHostFor(ctx: object) {
	return hosts.get(ctx);
}

export function registerProfilesHost(ctx: object, host: ProfilesHost) {
	hosts.set(ctx, host);
}
