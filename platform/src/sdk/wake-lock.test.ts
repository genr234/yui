import { describe, expect, test } from "bun:test";
import { createWakeLocks, type WakeLockDeps } from "./wake-lock";

type Call = { method: string; params: Record<string, unknown> };

function fakeController() {
	const calls: Call[] = [];
	const holds = new Set<string>();
	let next = 0;
	let down = false;
	const timers = new Map<number, () => void>();
	let timerId = 0;
	const intervals: number[] = [];

	const deps: WakeLockDeps = {
		async send<T>(method: string, params: Record<string, unknown>) {
			calls.push({ method, params });
			if (down) throw new Error("bridge down");
			switch (method) {
				case "wakelock.acquire": {
					const id = `wl_${++next}`;
					holds.add(id);
					return { id, owner: params.app_id, reason: params.reason, since: "", ttl_ms: 60_000 } as T;
				}
				case "wakelock.renew":
					return (holds.has(String(params.id)) ? { ok: true } : { ok: false }) as T;
				case "wakelock.release":
					return { ok: holds.delete(String(params.id)) } as T;
			}
			throw new Error(`unexpected ${method}`);
		},
		setInterval(fn, ms) {
			const id = ++timerId;
			timers.set(id, fn);
			intervals.push(ms);
			return id;
		},
		clearInterval(handle) {
			timers.delete(handle as number);
		},
	};

	return {
		deps,
		calls,
		holds,
		intervals,
		timers,
		setDown(value: boolean) {
			down = value;
		},
		/** Forget every hold, as a restarted controller would. */
		restart() {
			holds.clear();
		},
		async tick() {
			for (const fn of [...timers.values()]) fn();
			// let the renew promises settle
			for (let i = 0; i < 5; i++) await Promise.resolve();
		},
	};
}

describe("wake locks", () => {
	test("request acquires for the app and renews every ttl/3", async () => {
		const c = fakeController();
		const locks = createWakeLocks("dev.genr.youtube", c.deps);
		const lock = await locks.request("video playing");
		expect(c.calls[0]).toEqual({ method: "wakelock.acquire", params: { app_id: "dev.genr.youtube", reason: "video playing" } });
		expect(c.intervals).toEqual([20_000]);
		expect(lock.released).toBe(false);

		await c.tick();
		expect(c.calls.at(-1)).toEqual({ method: "wakelock.renew", params: { id: "wl_1" } });
		expect(c.holds.size).toBe(1);
	});

	test("release ends the hold and stops renewing; twice is harmless", async () => {
		const c = fakeController();
		const locks = createWakeLocks("app", c.deps);
		const lock = await locks.request();
		await lock.release();
		await lock.release();
		expect(lock.released).toBe(true);
		expect(c.holds.size).toBe(0);
		expect(c.timers.size).toBe(0);
		expect(c.calls.filter((call) => call.method === "wakelock.release")).toHaveLength(1);
		expect(locks.held).toBe(0);
	});

	test("closing the app releases every lock it still holds", async () => {
		const c = fakeController();
		const locks = createWakeLocks("app", c.deps);
		const a = await locks.request("a");
		const b = await locks.request("b");
		await locks.releaseAll();
		expect(a.released && b.released).toBe(true);
		expect(c.holds.size).toBe(0);
		expect(c.timers.size).toBe(0);
		await expect(locks.request("late")).rejects.toThrow("closed");
	});

	test("an app closed while acquiring does not leave a hold behind", async () => {
		const c = fakeController();
		const locks = createWakeLocks("app", c.deps);
		const pending = locks.request("racing");
		await locks.releaseAll();
		const lock = await pending;
		expect(lock.released).toBe(true);
		expect(c.holds.size).toBe(0);
	});

	test("a lock the controller forgot (restart) is taken again on the next renewal", async () => {
		const c = fakeController();
		const locks = createWakeLocks("app", c.deps);
		const lock = await locks.request("keep");
		c.restart();
		await c.tick();
		expect(c.holds.size).toBe(1);
		expect(c.holds.has("wl_2")).toBe(true);
		await lock.release();
		expect(c.holds.size).toBe(0);
	});

	test("a bridge outage is retried, not fatal", async () => {
		const c = fakeController();
		const locks = createWakeLocks("app", c.deps);
		const lock = await locks.request();
		c.setDown(true);
		await c.tick();
		c.setDown(false);
		await c.tick();
		expect(c.calls.at(-1)?.method).toBe("wakelock.renew");
		expect(lock.released).toBe(false);
		expect(c.timers.size).toBe(1);
	});
});
