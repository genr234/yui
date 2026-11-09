/**
 * The app-facing `ctx.profiles` (plan 10). Like controllers-runtime.ts, one
 * implementation serves both sides of the app sandbox: AppHost splices
 * `yuiProfilesRuntime.toString()` into the sandbox script and the host ctx
 * calls it directly. It must stay self-contained: no imports, no references
 * outside the function.
 *
 * The backend does the privileged work (`call("request" | "get" | "set",
 * [playerId, ...])` checks the `profiles` permission and reaches the
 * controller). Bridge errors arrive as messages that start with a code
 * (`version_conflict: current version 7`); they become errors with `code`
 * and, for conflicts, `version`.
 */

export type ProfilesBackend = {
	call(method: string, args: unknown[]): Promise<unknown>;
	/** The session's players, with `accountLinked`. Asking starts the controllers. */
	players(): Array<{ id: string; accountLinked?: boolean }>;
};

export function yuiProfilesRuntime(backend: ProfilesBackend) {
	const texts: Record<string, string> = {
		not_linked: "This player is not playing as a Yui account.",
		not_granted: "This player has not allowed this app to use their profile.",
		version_conflict: "The profile changed since it was read.",
		too_large: "The profile is too large (the limit is 64 KB).",
	};

	function fail(error: any) {
		const message = String(error?.message ?? error);
		const match = /^(not_linked|not_granted|version_conflict|too_large)\b/.exec(message);
		if (!match) return error instanceof Error ? error : new Error(message);
		const code = match[1];
		const out: any = new Error(`YUI_PROFILES: ${texts[code]}`);
		out.name = "YuiProfileError";
		out.code = code;
		if (code === "version_conflict") {
			const version = /current version (\d+)/.exec(message);
			out.version = version ? Number(version[1]) : undefined;
		}
		return out;
	}

	async function call(method: string, args: unknown[]) {
		try {
			return await backend.call(method, args);
		} catch (error) {
			throw fail(error);
		}
	}

	function idOf(player: any) {
		const id = typeof player === "string" ? player : player?.id;
		if (typeof id !== "string" || !id) throw new Error("YUI_PROFILES: a player or player id is required");
		return id;
	}

	const api = {
		/**
		 * Whether this player plays as a Yui account. False for guests and
		 * before `ctx.controllers.ready()` has resolved.
		 */
		available(player: any): boolean {
			const id = idOf(player);
			return backend.players().some((p) => p.id === id && p.accountLinked === true);
		},
		/** The player's profile for this app, or null for a guest. */
		for(player: any) {
			const id = idOf(player);
			if (!api.available(id)) return null;
			return {
				/** Asks the player on their phone. Resolves `{ granted }`, never rejects for a "no". */
				async request() {
					const reply: any = await call("request", [id]);
					return { granted: Boolean(reply && reply.granted) };
				},
				/** `{ profileId, data, version }`; `data` is null and `version` 0 until first saved. */
				async get() {
					const profile: any = await call("get", [id]);
					return {
						profileId: String(profile?.profile_id ?? ""),
						data: profile?.data ?? null,
						version: Number(profile?.version ?? 0),
					};
				},
				/** Saves `data` over `version` (from `get`). Rejects with `code: "version_conflict"` and the current `version` when it moved. */
				async set(data: unknown, version: number) {
					if (data === undefined) throw new Error("YUI_PROFILES: set(data, version) needs data (null clears it)");
					if (!Number.isInteger(version) || version < 0) {
						throw new Error("YUI_PROFILES: set(data, version) needs the version from get()");
					}
					const reply: any = await call("set", [id, data, version]);
					return { version: Number(reply?.version ?? version + 1) };
				},
			};
		},
	};

	return { api };
}
