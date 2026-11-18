import { beforeEach, describe, expect, test } from "bun:test";

const windowEvents = new Map<string, Set<() => void>>();
(globalThis as any).window ??= {};
(globalThis as any).window.addEventListener = (name: string, fn: () => void) => {
	let set = windowEvents.get(name);
	if (!set) windowEvents.set(name, (set = new Set()));
	set.add(fn);
};
(globalThis as any).window.removeEventListener = (name: string, fn: () => void) => windowEvents.get(name)?.delete(fn);
(globalThis as any).window.dispatchEvent = () => {};

const { createCameraHost, cameraErrorCode, liveCamera, snapshotSize, watchCamera } = await import("./camera");
const { handleSandboxApiCall, sandboxApiSource } = await import("./sandbox-apis");
const { validateSimpleApp } = await import("./validate");
const { describePermission } = await import("./permissions");

const app: any = { id: "dev.example.mirror", name: "Mirror", version: "1", permissions: ["camera"] };
const other: any = { id: "dev.example.other", name: "Other", version: "1", permissions: ["camera"] };

const granted = async () => {};
const denied = async () => {
	throw new Error("YUI_PERMISSION_DENIED: camera permission is required");
};
let allowSync = true;
const requireGranted = () => {
	if (!allowSync) throw new Error("YUI_PERMISSION_DENIED: camera permission is required");
};

let stopped = 0;
let requests: any[] = [];
let failWith: string | null = null;
let gate: Promise<void> | null = null;
let trackListeners: Array<() => void> = [];

function fakeStream() {
	const track = {
		stop: () => {
			stopped += 1;
		},
		getSettings: () => ({ width: 1920, height: 1080 }),
		addEventListener: (_name: string, fn: () => void) => trackListeners.push(fn),
	};
	return { getTracks: () => [track], getVideoTracks: () => [track] } as unknown as MediaStream;
}

const drawn: any[] = [];
let encoded: any[] = [];
const fakeVideo = (): any => ({
	videoWidth: 1920,
	videoHeight: 1080,
	readyState: 4,
	srcObject: null,
	play: async () => {},
	addEventListener() {},
	removeEventListener() {},
});
const deps: any = {
	mediaDevices: () => ({
		getUserMedia: async (constraints: any) => {
			requests.push(constraints);
			if (gate) await gate;
			if (failWith) throw Object.assign(new Error("nope"), { name: failWith });
			return fakeStream();
		},
		enumerateDevices: async () => [{ kind: "audioinput" }, { kind: "videoinput" }],
	}),
	createVideo: fakeVideo,
	createCanvas: (width: number, height: number) => ({
		width,
		height,
		getContext: () => ({ drawImage: (...args: any[]) => drawn.push(args) }),
		toDataURL: (type: string, quality: number) => {
			encoded.push({ type, quality, width, height });
			return `data:${type};base64,AAAA`;
		},
	}),
};

const hosts: Array<{ dispose(): void }> = [];
function host(who = app, ensure: any = granted) {
	const created = createCameraHost(who, ensure, requireGranted, deps);
	hosts.push(created);
	return created;
}

beforeEach(() => {
	for (const created of hosts.splice(0)) created.dispose();
	stopped = 0;
	requests = [];
	failWith = null;
	gate = null;
	trackListeners = [];
	drawn.length = 0;
	encoded = [];
	allowSync = true;
	windowEvents.clear();
});

describe("ctx.camera host", () => {
	test("open needs the permission and does not touch the camera without it", async () => {
		const { api } = host(app, denied);
		await expect(api.open()).rejects.toThrow("YUI_PERMISSION_DENIED");
		expect(requests).toHaveLength(0);
		expect(liveCamera()).toBeNull();
	});

	test("open asks for video only, with small defaults, and reports the real size", async () => {
		const { api } = host();
		const handle = await api.open();
		expect(requests).toHaveLength(1);
		expect(requests[0].audio).toBe(false);
		expect(requests[0].video.width).toEqual({ ideal: 640 });
		expect(requests[0].video.height).toEqual({ ideal: 480 });
		expect(requests[0].video.facingMode).toBeUndefined();
		expect(handle.width).toBe(1920);
		expect(liveCamera()?.appId).toBe(app.id);
	});

	test("facing and sizes are passed on, sizes are clamped", async () => {
		const { api } = host();
		await api.open({ width: 99999, height: 1, facing: "environment" });
		expect(requests[0].video.width).toEqual({ ideal: 1920 });
		expect(requests[0].video.height).toEqual({ ideal: 64 });
		expect(requests[0].video.facingMode).toEqual({ ideal: "environment" });
	});

	test("errors map to codes", async () => {
		const { api } = host();
		const cases: Array<[string, string]> = [
			["NotAllowedError", "blocked"],
			["SecurityError", "blocked"],
			["NotFoundError", "not_found"],
			["OverconstrainedError", "not_found"],
			["NotReadableError", "busy"],
			["AbortError", "busy"],
			["TypeError", "failed"],
		];
		for (const [name, code] of cases) {
			failWith = name;
			await expect(api.open()).rejects.toThrow(`YUI_CAMERA: ${code}`);
			expect(cameraErrorCode({ name })).toBe(code as any);
		}
		expect(liveCamera()).toBeNull();
	});

	test("no mediaDevices (insecure page) is unsupported", async () => {
		const created = createCameraHost(app, granted, requireGranted, { ...deps, mediaDevices: () => undefined });
		hosts.push(created);
		await expect(created.api.open()).rejects.toThrow("YUI_CAMERA: unsupported");
		expect(await created.api.available()).toBe(false);
	});

	test("available looks for a video input without opening it", async () => {
		const { api } = host();
		expect(await api.available()).toBe(true);
		expect(requests).toHaveLength(0);
		await expect(host({ ...app, permissions: [] }).api.available()).rejects.toThrow("YUI_PERMISSION_DENIED");
	});

	test("the camera stays on until the last handle stops", async () => {
		const { api } = host();
		const seen: string[] = [];
		api.on("state", (state) => seen.push(state.state));
		const a = await api.open();
		const b = await api.open();
		expect(requests).toHaveLength(1);
		await a.stop();
		await a.stop();
		expect(stopped).toBe(0);
		expect(liveCamera()).not.toBeNull();
		await b.stop();
		expect(stopped).toBe(1);
		expect(liveCamera()).toBeNull();
		expect(seen).toEqual(["open", "closed"]);
	});

	test("two opens at once share one getUserMedia call", async () => {
		const { api } = host();
		const [a, b] = await Promise.all([api.open(), api.open()]);
		expect(requests).toHaveLength(1);
		expect(a.id).not.toBe(b.id);
	});

	test("dispose turns the camera off and tells watchers", async () => {
		const created = host();
		await created.api.open();
		const seen: Array<string | null> = [];
		const off = watchCamera((camera) => seen.push(camera?.appId ?? null));
		created.dispose();
		off();
		expect(stopped).toBe(1);
		expect(liveCamera()).toBeNull();
		expect(seen).toEqual([app.id, null]);
	});

	test("a camera opened after dispose is stopped at once", async () => {
		const created = host();
		let release = () => {};
		gate = new Promise<void>((resolve) => (release = resolve));
		const opening = created.api.open();
		await new Promise((resolve) => setTimeout(resolve, 0));
		expect(requests).toHaveLength(1);
		created.dispose();
		release();
		await expect(opening).rejects.toThrow("YUI_CAMERA");
		expect(stopped).toBe(1);
		expect(liveCamera()).toBeNull();
	});

	test("another app cannot take the camera while it is on", async () => {
		const first = host(app);
		const second = host(other);
		await first.api.open();
		await expect(second.api.open()).rejects.toThrow("YUI_CAMERA: busy");
		await expect(second.api.snapshot()).rejects.toThrow("YUI_CAMERA: not_open");
		second.dispose();
		expect(liveCamera()?.appId).toBe(app.id);
	});

	test("losing the permission turns the camera off", async () => {
		const { api } = host();
		const seen: string[] = [];
		api.on("state", (state) => seen.push(state.state));
		await api.open();
		allowSync = false;
		for (const fn of windowEvents.get("yui:permissions-changed") ?? []) fn();
		expect(stopped).toBe(1);
		expect(liveCamera()).toBeNull();
		expect(seen).toEqual(["open", "closed"]);
	});

	test("an unplugged camera reports an error state", async () => {
		const { api } = host();
		const seen: any[] = [];
		api.on("state", (state) => seen.push(state));
		await api.open();
		for (const fn of trackListeners) fn();
		expect(liveCamera()).toBeNull();
		expect(seen[seen.length - 1]).toEqual({ state: "error", error: "failed" });
	});

	test("snapshot needs an open camera", async () => {
		const { api } = host();
		await expect(api.snapshot()).rejects.toThrow("YUI_CAMERA: not_open");
		const handle = await api.open();
		await handle.stop();
		await expect(api.snapshot()).rejects.toThrow("YUI_CAMERA: not_open");
	});

	test("snapshot is a downscaled JPEG, 640 wide by default", async () => {
		const { api } = host();
		await api.open();
		const shot = await api.snapshot();
		expect(shot).toEqual({ dataUrl: "data:image/jpeg;base64,AAAA", width: 640, height: 360 });
		expect(encoded[0]).toMatchObject({ type: "image/jpeg", quality: 0.8, width: 640, height: 360 });
		expect(drawn[0].slice(1)).toEqual([0, 0, 640, 360]);
	});

	test("snapshot caps the size at 1280 and never enlarges", async () => {
		const { api } = host();
		await api.open();
		expect((await api.snapshot({ maxWidth: 5000 })).width).toBe(1280);
		expect((await api.snapshot({ maxWidth: 320, quality: 5 })).width).toBe(320);
		expect(encoded[encoded.length - 1].quality).toBe(0.95);
		expect(snapshotSize(400, 300, 1000)).toEqual({ width: 400, height: 300 });
		expect(snapshotSize(1920, 1080, Number.NaN)).toEqual({ width: 640, height: 360 });
		expect(snapshotSize(1920, 1080, -5).width).toBe(16);
	});

	test("on() needs the permission and only knows the state event", () => {
		const { api } = host();
		expect(() => api.on("nope" as any, () => {})).toThrow("YUI_CAMERA");
		allowSync = false;
		expect(() => api.on("state", () => {})).toThrow("YUI_PERMISSION_DENIED");
	});
});

describe("ctx.camera across the sandbox", () => {
	test("handles are held by the host ctx and stopped by id", async () => {
		const created = host();
		const ctx: any = { camera: created.api };
		const push = () => {};
		const opened = (await handleSandboxApiCall(ctx, "camera", "open", [undefined], push)) as { id: string; width: number };
		expect(opened.width).toBe(1920);
		expect(liveCamera()).not.toBeNull();
		await handleSandboxApiCall(ctx, "camera", "stop", ["unknown"], push);
		expect(liveCamera()).not.toBeNull();
		await handleSandboxApiCall(ctx, "camera", "stop", [opened.id], push);
		expect(liveCamera()).toBeNull();
	});

	test("state events are pushed to the sandbox", async () => {
		const created = host();
		const ctx: any = { camera: created.api };
		const pushed: any[] = [];
		await handleSandboxApiCall(ctx, "camera", "subscribe", ["state"], (...args) => pushed.push(args));
		await handleSandboxApiCall(ctx, "camera", "open", [undefined], () => {});
		expect(pushed[0].slice(0, 2)).toEqual(["camera", "state"]);
		expect(pushed[0][2].state).toBe("open");
		await expect(handleSandboxApiCall(ctx, "camera", "subscribe", ["nope"], () => {})).rejects.toThrow("YUI_CAMERA");
	});

	test("the sandbox gets ctx.camera and a ui.camera node builder", () => {
		expect(sandboxApiSource).toContain("ctx.camera = {");
		expect(sandboxApiSource).toContain('ctx.ui.camera = (props) => ({ type: "camera"');
	});
});

describe("camera permission", () => {
	test("is a valid manifest permission with plain copy", () => {
		const manifest = { schema: "yui.simple-js.v0", id: "dev.example.mirror", name: "Mirror", version: "1.0.0", permissions: ["camera"], mount() {} };
		expect(() => validateSimpleApp(manifest)).not.toThrow();
		expect(describePermission("camera").label).toBe("Camera");
		expect(describePermission("camera").description).toContain("Pictures stay on the kiosk");
	});
});
