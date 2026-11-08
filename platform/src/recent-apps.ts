/**
 * Recently opened apps, newest first. Stand-in for `ctx.library` (plan D10):
 * once apps can register their own resume points, the Jump back in banner
 * reads those instead and this list only orders apps.
 *
 * Stored in the kiosk's internal store (collection `shell`, id `recent-apps`)
 * so it follows the active account and survives a browser profile reset.
 *
 * Writes are read-modify-write against a store epoch. A failed read does not
 * write. The epoch is captured when the launch is requested, not when its
 * turn on the queue begins, so a launch queued before an account switch does
 * not read the next account and record itself there. A put carries the epoch
 * from the read that built its snapshot, and the controller rejects it when
 * the account store has already changed — including the gap before
 * `yui:account-changed` is dispatched. Records run one at a time so two
 * launches cannot each write a snapshot that drops the other.
 */
import { internalStore } from "./sdk/internal-store";

export type RecentApp = { id: string; openedAt: number };

export type RecentAppsSnapshot = {
	items: unknown;
	epoch: number;
};

/** Account-scoped recent-apps document. Tests pass a fake. */
export type RecentAppsStore = {
	read(): Promise<RecentAppsSnapshot>;
	write(items: RecentApp[], epoch: number): Promise<void>;
};

const COLLECTION = "shell";
const ID = "recent-apps";
const LIMIT = 12;

const defaultStore: RecentAppsStore = {
	async read() {
		const snap = await internalStore.collection<RecentApp[]>(COLLECTION).getScoped(ID);
		return { items: snap.value, epoch: snap.epoch };
	},
	async write(items, epoch) {
		await internalStore.collection<RecentApp[]>(COLLECTION).putScoped(ID, items, epoch);
	},
};

let queue: Promise<void> = Promise.resolve();

function enqueue<T>(task: () => Promise<T>): Promise<T> {
	const run = queue.then(task, task);
	queue = run.then(
		() => undefined,
		() => undefined,
	);
	return run;
}

export function parseRecentApps(value: unknown): RecentApp[] {
	if (!Array.isArray(value)) return [];
	const items: RecentApp[] = [];
	const seen = new Set<string>();
	for (const entry of value) {
		if (!entry || typeof entry !== "object") continue;
		const id = (entry as { id?: unknown }).id;
		const openedAt = (entry as { openedAt?: unknown }).openedAt;
		if (typeof id !== "string" || id === "" || seen.has(id)) continue;
		if (typeof openedAt !== "number" || !Number.isFinite(openedAt)) continue;
		seen.add(id);
		items.push({ id, openedAt });
		if (items.length === LIMIT) break;
	}
	return items;
}

export async function loadRecentApps(store: RecentAppsStore = defaultStore): Promise<RecentApp[]> {
	try {
		const snap = await store.read();
		return parseRecentApps(snap.items);
	} catch {
		return [];
	}
}

async function commitRecentApp(
	store: RecentAppsStore,
	id: string,
	now: number,
	origin: Promise<RecentAppsSnapshot>,
): Promise<RecentApp[]> {
	const started = await origin;
	const snap = await store.read();
	if (snap.epoch !== started.epoch) {
		throw new Error("store scope changed");
	}
	const next = [{ id, openedAt: now }, ...parseRecentApps(snap.items).filter((entry) => entry.id !== id)].slice(0, LIMIT);
	await store.write(next, snap.epoch);
	return next;
}

export function recordRecentApp(
	id: string,
	now = Date.now(),
	store: RecentAppsStore = defaultStore,
): Promise<RecentApp[]> {
	// Start the scope read now. Waiting for the queue first would let a switch
	// land, then this launch would read and write the new account.
	const origin = store.read();
	return enqueue(() => commitRecentApp(store, id, now, origin));
}

export function openedAgo(openedAt: number, now = Date.now()) {
	const minutes = Math.max(0, Math.round((now - openedAt) / 60000));
	if (minutes < 1) return "just now";
	if (minutes < 60) return `${minutes} min ago`;
	const hours = Math.round(minutes / 60);
	if (hours < 24) return `${hours} h ago`;
	const days = Math.round(hours / 24);
	return days === 1 ? "yesterday" : `${days} days ago`;
}
