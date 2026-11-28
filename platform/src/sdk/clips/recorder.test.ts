import { beforeEach, describe, expect, test } from "bun:test";
import { CameraRecorder, CROP_FACE_SCALE, faceCrop, KEEP_MS, OVERLAP_MS, pickMime, pruneSegments, RETRY_MS, SEGMENT_MS, type RecorderDeps, type Segment } from "./recorder";

// ---- fakes -----------------------------------------------------------------

let clock = 1_000_000;
type Timer = { id: number; at: number; fn: () => void; every?: number };
let timers: Timer[] = [];
let nextTimer = 1;

function advance(ms: number) {
	const end = clock + ms;
	for (;;) {
		timers.sort((a, b) => a.at - b.at || a.id - b.id);
		const due = timers[0];
		if (!due || due.at > end) break;
		clock = due.at;
		if (due.every) due.at += due.every;
		else timers.shift();
		due.fn();
	}
	clock = end;
}

const flushMicrotasks = async () => {
	for (let i = 0; i < 20; i++) await Promise.resolve();
};

class FakeRecorder {
	static all: FakeRecorder[] = [];
	static supported = new Set(["video/webm;codecs=vp8", "video/webm"]);
	static isTypeSupported(type: string) {
		return FakeRecorder.supported.has(type);
	}
	state = "inactive";
	private listeners = new Map<string, Array<(event: any) => void>>();
	constructor(
		public stream: unknown,
		public options: MediaRecorderOptions,
	) {
		FakeRecorder.all.push(this);
	}
	addEventListener(type: string, fn: (event: any) => void) {
		const list = this.listeners.get(type) ?? [];
		list.push(fn);
		this.listeners.set(type, list);
	}
	private fire(type: string, event: any = {}) {
		for (const fn of this.listeners.get(type) ?? []) fn(event);
	}
	start() {
		this.state = "recording";
		this.fire("start");
	}
	stop() {
		if (this.state === "inactive") return;
		this.state = "inactive";
		this.fire("dataavailable", { data: new Blob([new Uint8Array(1000)], { type: "video/webm" }) });
		this.fire("stop");
	}
}

let stoppedTracks = 0;
let gumCalls = 0;
let gumFails = false;
function fakeStream() {
	const track = {
		stop: () => {
			stoppedTracks++;
		},
		getSettings: () => ({ width: 640, height: 480 }),
		addEventListener: () => {},
	};
	return { getTracks: () => [track], getVideoTracks: () => [track] } as unknown as MediaStream;
}

type Sent = { method: string; params: any };
let sent: Sent[] = [];
let eventHandlers = new Map<string, (data: any) => void>();
let faceSample: ((face: { x: number; y: number; size: number } | null) => void) | null = null;
let facesStopped = 0;

function deps(over: Partial<RecorderDeps> = {}): RecorderDeps {
	return {
		bridge: {
			send: async (method: string, params?: Record<string, unknown>) => {
				sent.push({ method, params });
				return { ok: true } as any;
			},
			on: (event: string, fn: (data: any) => void) => {
				eventHandlers.set(event, fn);
				return () => eventHandlers.delete(event);
			},
		},
		mediaDevices: () =>
			({
				getUserMedia: async () => {
					gumCalls++;
					if (gumFails) throw Object.assign(new Error("no"), { name: "NotFoundError" });
					return fakeStream();
				},
			}) as any,
		recorderClass: () => FakeRecorder as any,
		now: () => clock,
		setTimeout: (fn, ms) => {
			const t = { id: nextTimer++, at: clock + ms, fn };
			timers.push(t);
			return t.id;
		},
		clearTimeout: (h) => {
			timers = timers.filter((t) => t.id !== h);
		},
		setInterval: (fn, ms) => {
			const t = { id: nextTimer++, at: clock + ms, fn, every: ms };
			timers.push(t);
			return t.id;
		},
		clearInterval: (h) => {
			timers = timers.filter((t) => t.id !== h);
		},
		trackFaces: (_stream, sample) => {
			faceSample = sample;
			return () => {
				facesStopped++;
			};
		},
		encode: async (blob) => `b64:${blob.size}`,
		...over,
	};
}

beforeEach(() => {
	clock = 1_000_000;
	timers = [];
	nextTimer = 1;
	FakeRecorder.all = [];
	FakeRecorder.supported = new Set(["video/webm;codecs=vp8", "video/webm"]);
	stoppedTracks = 0;
	gumCalls = 0;
	gumFails = false;
	sent = [];
	eventHandlers = new Map();
	faceSample = null;
	facesStopped = 0;
});

// ---- pure helpers ------------------------------------------------------------

describe("faceCrop", () => {
	test("null without samples in the window", () => {
		expect(faceCrop([], 0, 10, 640, 480)).toBeNull();
		expect(faceCrop([{ at: 50, x: 0.5, y: 0.5, size: 0.2 }], 0, 10, 640, 480)).toBeNull();
	});

	test("square in pixels around the median face, scaled", () => {
		const samples = [
			{ at: 1, x: 0.5, y: 0.5, size: 0.2 },
			{ at: 2, x: 0.52, y: 0.5, size: 0.2 },
			{ at: 3, x: 0.9, y: 0.1, size: 0.6 }, // outlier: the median ignores it
		];
		const crop = faceCrop(samples, 0, 10, 640, 480)!;
		const side = 0.2 * 480 * CROP_FACE_SCALE;
		expect(crop.w * 640).toBeCloseTo(side, 5);
		expect(crop.h * 480).toBeCloseTo(side, 5);
		expect((crop.x + crop.w / 2) * 640).toBeCloseTo(0.52 * 640, 5);
		expect((crop.y + crop.h / 2) * 480).toBeCloseTo(240, 5);
	});

	test("clamped to the frame", () => {
		const crop = faceCrop([{ at: 1, x: 0.98, y: 0.02, size: 0.3 }], 0, 10, 640, 480)!;
		expect(crop.x + crop.w).toBeCloseTo(1, 6);
		expect(crop.y).toBe(0);
		const huge = faceCrop([{ at: 1, x: 0.5, y: 0.5, size: 0.9 }], 0, 10, 640, 480)!;
		expect(huge.h).toBeCloseTo(1, 6);
		expect(huge.w * 640).toBeCloseTo(480, 5);
	});
});

describe("pruneSegments / pickMime", () => {
	const seg = (start: number, end: number, size: number): Segment => ({ start_ms: start, end_ms: end, mime: "video/webm", blob: new Blob([new Uint8Array(size)]) });

	test("drops old and over-budget segments, oldest first", () => {
		const now = 100_000;
		const kept = pruneSegments([seg(0, 2000, 10), seg(now - 4000, now - 2000, 10), seg(now - 2000, now, 10)], now);
		expect(kept.map((s) => s.start_ms)).toEqual([now - 4000, now - 2000]);
		const capped = pruneSegments([seg(1, 2, 60), seg(2, 3, 60), seg(3, 4, 60)], 4, KEEP_MS, 130);
		expect(capped.map((s) => s.start_ms)).toEqual([2, 3]);
	});

	test("picks the first supported type", () => {
		expect(pickMime((t) => t === "video/webm;codecs=h264" || t === "video/webm")).toBe("video/webm;codecs=h264");
		expect(pickMime(() => false)).toBeNull();
	});
});

// ---- the recorder ------------------------------------------------------------

describe("CameraRecorder", () => {
	async function started() {
		const rec = new CameraRecorder(deps());
		rec.start();
		await flushMicrotasks();
		return rec;
	}

	test("records overlapping segments and reports status", async () => {
		const rec = await started();
		expect(rec.active).toBe(true);
		expect(sent[0]).toEqual({ method: "clips.camera.status", params: { active: true, width: 640, height: 480 } });
		expect(FakeRecorder.all[0].options).toEqual({ mimeType: "video/webm;codecs=vp8", videoBitsPerSecond: 900_000 });

		advance(SEGMENT_MS); // rotation: second recorder starts, first stops OVERLAP_MS later
		expect(FakeRecorder.all.length).toBe(2);
		expect(FakeRecorder.all[0].state).toBe("recording");
		advance(OVERLAP_MS);
		expect(FakeRecorder.all[0].state).toBe("inactive");
		expect(rec.recorded.length).toBe(1);
		const [first] = rec.recorded;
		expect(first.mime).toBe("video/webm");
		expect(first.end_ms).toBeGreaterThan(first.start_ms);
		// The next segment started before this one ended: no gap.
		advance(SEGMENT_MS + 500);
		expect(rec.recorded[1].start_ms).toBeLessThan(first.end_ms);

		advance(15_000);
		expect(sent.filter((s) => s.method === "clips.camera.status").length).toBeGreaterThan(1);
		rec.dispose();
	});

	test("keeps only the last 45 s", async () => {
		const rec = await started();
		advance(120_000);
		const oldest = rec.recorded[0];
		expect(oldest.end_ms).toBeGreaterThanOrEqual(clock - KEEP_MS - SEGMENT_MS);
		rec.dispose();
	});

	test("answers a request with the overlapping segments and a face crop", async () => {
		const rec = await started();
		for (let i = 0; i < 40; i++) {
			faceSample?.({ x: 0.5, y: 0.4, size: 0.25 });
			advance(250);
		}
		const to = clock;
		const from = to - 6000;
		const answer = rec.answer({ req_id: "r1", from_ms: from, to_ms: to });
		advance(OVERLAP_MS); // lets the flushed segment stop
		await answer;
		const deliver = sent.find((s) => s.method === "clips.camera.deliver")!;
		expect(deliver.params.req_id).toBe("r1");
		expect(deliver.params.error).toBeUndefined();
		const segs = deliver.params.segments as Array<{ start_ms: number; end_ms: number; data: string }>;
		expect(segs.length).toBeGreaterThanOrEqual(3);
		expect(segs[0].start_ms).toBeLessThanOrEqual(from);
		expect(segs[segs.length - 1].end_ms).toBeGreaterThanOrEqual(to);
		for (const s of segs) expect(s.data).toBe("b64:1000");
		expect(deliver.params.crop).not.toBeNull();
		expect(deliver.params.crop.x + deliver.params.crop.w / 2).toBeCloseTo(0.5, 5);
		rec.dispose();
	});

	test("the bridge event reaches the recorder", async () => {
		const rec = await started();
		advance(5000);
		eventHandlers.get("clips.camera.request")!({ req_id: "ev", from_ms: clock - 3000, to_ms: clock });
		advance(OVERLAP_MS);
		await flushMicrotasks();
		expect(sent.some((s) => s.method === "clips.camera.deliver" && s.params.req_id === "ev")).toBe(true);
		rec.dispose();
	});

	test("refuses when there is no camera, and retries later", async () => {
		gumFails = true;
		const rec = new CameraRecorder(deps());
		rec.start();
		await flushMicrotasks();
		expect(rec.active).toBe(false);
		expect(sent[0]).toEqual({ method: "clips.camera.status", params: { active: false } });
		await rec.answer({ req_id: "r2", from_ms: clock - 1000, to_ms: clock });
		expect(sent.find((s) => s.method === "clips.camera.deliver")!.params).toEqual({ req_id: "r2", segments: [], crop: null, error: "camera off" });

		gumFails = false;
		advance(RETRY_MS);
		await flushMicrotasks();
		expect(gumCalls).toBe(2);
		expect(rec.active).toBe(true);
		rec.dispose();
	});

	test("no MediaRecorder support: inactive, no retry", async () => {
		FakeRecorder.supported = new Set();
		const rec = new CameraRecorder(deps());
		rec.start();
		await flushMicrotasks();
		expect(rec.active).toBe(false);
		expect(gumCalls).toBe(0);
		expect(timers.length).toBe(0);
		rec.dispose();
	});

	test("dispose stops the camera, timers and listeners", async () => {
		const rec = await started();
		advance(3000);
		rec.dispose();
		expect(stoppedTracks).toBe(1);
		expect(facesStopped).toBe(1);
		expect(FakeRecorder.all.every((r) => r.state === "inactive")).toBe(true);
		expect(eventHandlers.size).toBe(0);
		expect(timers.filter((t) => t.every).length).toBe(0);
		expect(rec.recorded.length).toBe(0);
	});
});
