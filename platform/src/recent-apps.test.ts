import { expect, test } from "bun:test";
import {
	loadRecentApps,
	parseRecentApps,
	recordRecentApp,
	type RecentApp,
	type RecentAppsStore,
} from "./recent-apps";

type Bucket = Map<number, unknown>;

function memoryStore(initial: unknown, epoch = 1): RecentAppsStore & {
	buckets: Bucket;
	epoch: () => number;
	switchTo: (epoch: number, value?: unknown) => void;
	writes: RecentApp[][];
} {
	const buckets: Bucket = new Map([[epoch, initial]]);
	let current = epoch;
	const writes: RecentApp[][] = [];
	return {
		buckets,
		epoch: () => current,
		writes,
		switchTo(next, value = null) {
			current = next;
			if (!buckets.has(next)) buckets.set(next, value);
		},
		async read() {
			return { items: buckets.get(current), epoch: current };
		},
		async write(items, seen) {
			if (seen !== current) throw new Error("store scope changed");
			buckets.set(current, items);
			writes.push(items);
		},
	};
}

test("failed read does not write", async () => {
	let writes = 0;
	const store: RecentAppsStore = {
		async read() {
			throw new Error("disk read failed");
		},
		async write() {
			writes += 1;
		},
	};

	await expect(recordRecentApp("links", 10, store)).rejects.toThrow("disk read failed");
	expect(writes).toBe(0);
	expect(await loadRecentApps(store)).toEqual([]);
});

test("account switch while a record is in flight does not write the old list", async () => {
	let releaseRead: () => void = () => {};
	const readGate = new Promise<void>((resolve) => {
		releaseRead = resolve;
	});
	let markStarted: () => void = () => {};
	const started = new Promise<void>((resolve) => {
		markStarted = resolve;
	});
	const buckets: Bucket = new Map([[1, [{ id: "links", openedAt: 1 }]]]);
	let current = 1;
	const writes: Array<{ epoch: number; items: RecentApp[] }> = [];
	const store: RecentAppsStore = {
		async read() {
			const epoch = current;
			const items = buckets.get(epoch);
			markStarted();
			await readGate;
			return { items, epoch };
		},
		async write(items, epoch) {
			if (epoch !== current) throw new Error("store scope changed");
			buckets.set(current, items);
			writes.push({ epoch, items });
		},
	};

	const pending = recordRecentApp("fruit", 20, store);
	await started;
	current = 2;
	buckets.set(2, [{ id: "kept", openedAt: 4 }]);
	releaseRead();

	await expect(pending).rejects.toThrow("store scope changed");
	expect(writes).toEqual([]);
	expect(buckets.get(2)).toEqual([{ id: "kept", openedAt: 4 }]);
	expect(buckets.get(1)).toEqual([{ id: "links", openedAt: 1 }]);
});

test("queued launch from the previous account is not written after a switch", async () => {
	let release: () => void = () => {};
	const gate = new Promise<void>((resolve) => {
		release = resolve;
	});
	const store = memoryStore([{ id: "links", openedAt: 1 }]);
	let reads = 0;
	const original = store.read.bind(store);
	store.read = async () => {
		const snap = await original();
		reads += 1;
		if (reads === 1) await gate;
		return snap;
	};

	const blocking = recordRecentApp("fruit", 10, store);
	const queued = recordRecentApp("subway", 11, store);
	await Promise.resolve();
	store.switchTo(2, [{ id: "kept", openedAt: 4 }]);
	release();

	await expect(blocking).rejects.toThrow("store scope changed");
	await expect(queued).rejects.toThrow("store scope changed");
	expect(store.writes).toEqual([]);
	expect(store.buckets.get(1)).toEqual([{ id: "links", openedAt: 1 }]);
	expect(store.buckets.get(2)).toEqual([{ id: "kept", openedAt: 4 }]);
});

test("concurrent records keep every launch", async () => {
	const store = memoryStore([{ id: "links", openedAt: 1 }]);
	let releaseFirst: () => void = () => {};
	const firstGate = new Promise<void>((resolve) => {
		releaseFirst = resolve;
	});
	let reads = 0;
	const original = store.read.bind(store);
	store.read = async () => {
		reads += 1;
		if (reads === 1) await firstGate;
		return original();
	};

	const first = recordRecentApp("fruit", 10, store);
	const second = recordRecentApp("subway", 11, store);
	await Promise.resolve();
	releaseFirst();
	await Promise.all([first, second]);

	expect(store.writes.at(-1)?.map((entry) => entry.id)).toEqual(["subway", "fruit", "links"]);
	expect(await loadRecentApps(store)).toEqual([
		{ id: "subway", openedAt: 11 },
		{ id: "fruit", openedAt: 10 },
		{ id: "links", openedAt: 1 },
	]);
});

test("malformed entries are dropped and the list is capped at 12", async () => {
	const stored = [
		{ id: "keep", openedAt: 1 },
		{ id: "", openedAt: 2 },
		null,
		{ id: "nan", openedAt: Number.NaN },
		{ id: "text", openedAt: "3" },
		{ id: "keep", openedAt: 9 },
		...Array.from({ length: 14 }, (_, index) => ({ id: `old-${index}`, openedAt: index + 1 })),
	];
	expect(parseRecentApps(stored).map((entry) => entry.id)).toEqual([
		"keep",
		...Array.from({ length: 11 }, (_, index) => `old-${index}`),
	]);

	const store = memoryStore(stored);
	const next = await recordRecentApp("fresh", 50, store);
	expect(next).toHaveLength(12);
	expect(next[0]).toEqual({ id: "fresh", openedAt: 50 });
	expect(next.map((entry) => entry.id)).not.toContain("old-11");
	expect(store.buckets.get(1)).toEqual(next);
});
