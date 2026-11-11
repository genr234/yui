/**
 * Wake locks: an app keeps the kiosk awake while it shows something nobody
 * is touching (a video, a cutscene, a game in attract mode).
 *
 * While any lock is held the controller keeps the display and the system
 * on, keeps the screensaver closed, and keeps reporting user activity so
 * idle tools on the kiosk (kiosk 1 runs one that closes Chrome after five
 * minutes without mouse input) leave the machine alone. The platform also
 * holds back its own attract screen.
 *
 * Each lock is a lease on the controller: this module renews it every
 * ttl/3 for as long as the lock is held. Releasing it, disposing the app's
 * ctx, reloading the page or losing Chrome all end it (the last two because
 * nobody renews), so idle behaviour always comes back once the app is gone.
 * If the controller forgot a lock (it restarted), the next renewal acquires
 * a new one.
 */
import { bridge } from "./bridge";

export type WakeLockHold = {
	id: string;
	/** The app id, or "native" while a native program runs. */
	owner: string;
	reason?: string;
	since: string;
	/** Absent for the controller's own native-program hold. */
	expires?: string;
};

export type WakeLockView = { holds: WakeLockHold[]; active: boolean };

/** One held wake lock (what `ctx.wakeLock.request` returns). */
export interface YuiWakeLock {
	readonly reason: string;
	/** True once released by the app or because the app closed. */
	readonly released: boolean;
	/** Lets the kiosk idle again. Safe to call more than once. */
	release(): Promise<void>;
}

type Grant = WakeLockHold & { ttl_ms: number };

export type WakeLockDeps = {
	send: <T>(method: string, params: Record<string, unknown>) => Promise<T>;
	setInterval: (fn: () => void, ms: number) => unknown;
	clearInterval: (handle: unknown) => void;
};

const defaultDeps: WakeLockDeps = {
	send: (method, params) => bridge.send(method, params),
	setInterval: (fn, ms) => window.setInterval(fn, ms),
	clearInterval: (handle) => window.clearInterval(handle as number),
};

/** Shortest renewal gap, whatever ttl the controller reports. */
const minRenewMs = 1_000;

/**
 * The wake locks of one app. `releaseAll` runs when the app's ctx is
 * disposed (the app closed).
 */
export function createWakeLocks(appId: string, deps: WakeLockDeps = defaultDeps) {
	const live = new Set<() => Promise<void>>();
	let disposed = false;

	async function request(reason = ""): Promise<YuiWakeLock> {
		if (disposed) throw new Error("YUI_WAKE_LOCK: the app has closed");
		reason = String(reason ?? "").slice(0, 200);
		let grant = await deps.send<Grant>("wakelock.acquire", { app_id: appId, reason });
		let released = false;
		let timer: unknown;
		let renewing = false;

		const renew = async () => {
			if (released || renewing) return;
			renewing = true;
			try {
				const result = await deps.send<{ ok: boolean; hold?: Grant }>("wakelock.renew", { id: grant.id });
				if (released) return;
				if (!result?.ok) {
					// The controller no longer knows this lock (it restarted):
					// take a new one so the app stays covered.
					const fresh = await deps.send<Grant>("wakelock.acquire", { app_id: appId, reason });
					if (released) {
						await deps.send("wakelock.release", { id: fresh.id }).catch(() => {});
						return;
					}
					grant = fresh;
				}
			} catch {
				// Bridge down for a moment: try again at the next tick. If it
				// stays down, the lease simply ends on the controller.
			} finally {
				renewing = false;
			}
		};

		const release = async () => {
			if (released) return;
			released = true;
			deps.clearInterval(timer);
			live.delete(release);
			await deps.send("wakelock.release", { id: grant.id }).catch(() => {});
		};

		timer = deps.setInterval(() => void renew(), Math.max(minRenewMs, Math.floor((grant.ttl_ms || 60_000) / 3)));
		live.add(release);
		const lock: YuiWakeLock = {
			reason,
			get released() {
				return released;
			},
			release,
		};
		// The app closed while the controller was answering.
		if (disposed) await release();
		return lock;
	}

	async function releaseAll() {
		disposed = true;
		await Promise.all([...live].map((release) => release()));
	}

	return { request, releaseAll, get held() { return live.size; } };
}

/**
 * Follows every wake lock on the kiosk (all apps, plus the controller's own
 * hold while a native program runs). Returns an unsubscribe function.
 */
export function watchWakeLocks(fn: (view: WakeLockView) => void): () => void {
	let stopped = false;
	const off = bridge.on<WakeLockView>("wakelock.changed", (view) => {
		if (!stopped) fn(normalize(view));
	});
	bridge
		.send<WakeLockView>("wakelock.list")
		.then((view) => {
			if (!stopped) fn(normalize(view));
		})
		.catch(() => {});
	return () => {
		stopped = true;
		off();
	};
}

function normalize(view: WakeLockView | null | undefined): WakeLockView {
	const holds = Array.isArray(view?.holds) ? view.holds : [];
	return { holds, active: holds.length > 0 };
}
