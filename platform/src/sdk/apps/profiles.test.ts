import { beforeEach, describe, expect, mock, test } from "bun:test";

type Sent = { method: string; params: any };
const sent: Sent[] = [];
let failWith: string | null = null;

mock.module("../bridge", () => ({
	bridge: {
		send: (method: string, params: any = {}) => {
			sent.push({ method, params });
			if (failWith) return Promise.reject(new Error(failWith));
			if (method === "pad.profile.request") return Promise.resolve({ granted: true });
			if (method === "pad.profile.get") return Promise.resolve({ profile_id: "pid", data: { best: 3 }, version: 2 });
			if (method === "pad.profile.set") return Promise.resolve({ version: 3 });
			return Promise.resolve(null);
		},
		on: () => () => {},
	},
}));

const { yuiProfilesRuntime } = await import("./profiles-runtime");
const { createProfilesHost } = await import("./profiles");
const { sandboxApiSource } = await import("./sandbox-apis");

const app = { id: "dev.example.quiz", name: "Quiz" } as any;
const players = [
	{ id: "p1", accountLinked: true },
	{ id: "p2", accountLinked: false },
];

function runtime(ensure: (p: string) => Promise<void> = async () => {}) {
	const host = createProfilesHost(app, ensure as any);
	return yuiProfilesRuntime({ call: (m, a) => host.call(m, a), players: () => players }).api;
}

beforeEach(() => {
	sent.length = 0;
	failWith = null;
});

describe("ctx.profiles", () => {
	test("guests have no profile", () => {
		const api = runtime();
		expect(api.available("p1")).toBe(true);
		expect(api.available("p2")).toBe(false);
		expect(api.for("p2")).toBeNull();
		expect(api.for("nobody")).toBeNull();
	});

	test("request, get and set go through the bridge with the app id", async () => {
		const profile = runtime().for({ id: "p1" } as any)!;
		expect(await profile.request()).toEqual({ granted: true });
		expect(await profile.get()).toEqual({ profileId: "pid", data: { best: 3 }, version: 2 });
		expect(await profile.set({ best: 9 }, 2)).toEqual({ version: 3 });
		expect(sent).toEqual([
			{ method: "pad.profile.request", params: { app_id: app.id, player_id: "p1", app_name: "Quiz" } },
			{ method: "pad.profile.get", params: { app_id: app.id, player_id: "p1" } },
			{ method: "pad.profile.set", params: { app_id: app.id, player_id: "p1", data: { best: 9 }, version: 2 } },
		]);
	});

	test("the profiles permission is checked on every call", async () => {
		const profile = runtime(async () => {
			throw new Error("YUI_PERMISSION_DENIED: profiles permission is required");
		}).for("p1")!;
		await expect(profile.get()).rejects.toThrow("YUI_PERMISSION_DENIED");
		expect(sent).toHaveLength(0);
	});

	test("a version conflict is a typed error with the current version", async () => {
		const profile = runtime().for("p1")!;
		failWith = "version_conflict: current version 7";
		const error: any = await profile.set({}, 1).catch((e) => e);
		expect(error.name).toBe("YuiProfileError");
		expect(error.code).toBe("version_conflict");
		expect(error.version).toBe(7);
	});

	test("other bridge errors keep a code", async () => {
		const profile = runtime().for("p1")!;
		for (const code of ["not_linked", "not_granted", "too_large"]) {
			failWith = `${code}: details`;
			const error: any = await profile.get().catch((e) => e);
			expect(error.code).toBe(code);
		}
		failWith = "the app does not own the controllers";
		await expect(profile.get()).rejects.toThrow("does not own");
	});

	test("set needs data and a version", async () => {
		const profile = runtime().for("p1")!;
		await expect(profile.set(undefined, 0)).rejects.toThrow("needs data");
		await expect(profile.set({}, -1)).rejects.toThrow("version");
		await expect(profile.set({}, 1.5)).rejects.toThrow("version");
	});

	test("the sandbox gets the same runtime, only for apps that declare profiles", () => {
		expect(sandboxApiSource).toContain("yuiProfilesRuntime");
		expect(sandboxApiSource).toContain('app.permissions.includes("profiles")');
		// Evaluated the way the sandbox does, with a declaring and a non-declaring app.
		const make = (declared: boolean) => {
			const body =
				`${sandboxApiSource}\nreturn yuiExtendContext({});`.replace(/^/, "");
			return new Function("rpc", "disposables", "scheduleRender", "window", "app", body)(
				async () => null,
				new Set(),
				() => {},
				{ addEventListener() {} },
				{ permissions: declared ? ["profiles"] : [] },
			);
		};
		expect("profiles" in make(true)).toBe(true);
		expect("profiles" in make(false)).toBe(false);
	});
});
