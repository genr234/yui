/**
 * The kiosk camera's replay ring, for phone clips (the "facecam" bubble).
 *
 * The overlay keeps the last ~45 s of the kiosk camera as short,
 * self-contained WebM segments (each from its own MediaRecorder, so each
 * starts with a header and a keyframe) plus face-tracker samples. When a
 * phone makes a clip the controller asks for a time window over the bridge
 * (`clips.camera.request`) and gets the overlapping segments and a
 * face-follow crop back (`clips.camera.deliver`); it composes the video.
 *
 * This is a system feature: it opens its own getUserMedia stream (Chrome
 * shares the device within the page), never touches the apps' camera host
 * (`sdk/apps/camera.ts`), and shows no indicator. Nothing leaves the kiosk
 * unless a phone asks for a clip.
 */

export const SEGMENT_MS = 2000;
/** The next recorder starts this long before the previous one stops, so segments overlap. */
export const OVERLAP_MS = 150;
export const KEEP_MS = 45_000;
export const MAX_BYTES = 12 * 1024 * 1024;
export const RETRY_MS = 60_000;
export const STATUS_MS = 15_000;
export const FACE_EVERY_MS = 200;
/** How long a request waits for the segment covering its end to finish. */
export const REQUEST_WAIT_MS = 2500;
/** Crop side as a multiple of the median face size. */
export const CROP_FACE_SCALE = 2.6;

const MIME_TYPES = ["video/webm;codecs=vp8", "video/webm;codecs=h264", "video/webm"];

export type FaceSample = { at: number; x: number; y: number; size: number };
export type Crop = { x: number; y: number; w: number; h: number };
export type Segment = { start_ms: number; end_ms: number; mime: string; blob: Blob };

type RequestFrame = { req_id?: unknown; from_ms?: unknown; to_ms?: unknown };

/** The bridge as the recorder uses it. */
export type RecorderBridge = {
	send<T>(method: string, params?: Record<string, unknown>): Promise<T>;
	on<T>(event: string, fn: (data: T) => void): () => void;
};

type RecorderLike = {
	state: string;
	start(): void;
	stop(): void;
	addEventListener(type: string, fn: (event: any) => void): void;
};

/** Everything from the browser, so tests can fake it. */
export type RecorderDeps = {
	bridge: RecorderBridge;
	mediaDevices(): Pick<MediaDevices, "getUserMedia"> | undefined;
	/** null when MediaRecorder is missing. */
	recorderClass(): { new (stream: MediaStream, options: MediaRecorderOptions): RecorderLike; isTypeSupported(type: string): boolean } | null;
	now(): number;
	setTimeout(fn: () => void, ms: number): unknown;
	clearTimeout(handle: unknown): void;
	setInterval(fn: () => void, ms: number): unknown;
	clearInterval(handle: unknown): void;
	/** Starts face tracking on the stream; calls `sample` with normalized faces. Returns a stop function. */
	trackFaces?(stream: MediaStream, sample: (face: { x: number; y: number; size: number } | null) => void): () => void;
	encode(blob: Blob): Promise<string>;
};

function median(values: number[]): number {
	const sorted = [...values].sort((a, b) => a - b);
	const mid = sorted.length >> 1;
	return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

/**
 * The facecam crop for a window: a square (in pixels) around the median face
 * centre, CROP_FACE_SCALE times the median face size, kept inside the frame.
 * Face x/y are the centre, size a share of the frame's short edge (as the
 * tracker reports it). Fractions of the frame; null without samples.
 */
export function faceCrop(samples: FaceSample[], fromMs: number, toMs: number, width: number, height: number): Crop | null {
	if (!(width > 0 && height > 0)) return null;
	const inside = samples.filter((s) => s.at >= fromMs && s.at <= toMs && Number.isFinite(s.x) && Number.isFinite(s.y) && s.size > 0);
	if (!inside.length) return null;
	const edge = Math.min(width, height);
	const cx = median(inside.map((s) => s.x)) * width;
	const cy = median(inside.map((s) => s.y)) * height;
	const side = Math.min(edge, Math.max(edge * 0.25, median(inside.map((s) => s.size)) * edge * CROP_FACE_SCALE));
	const left = Math.min(width - side, Math.max(0, cx - side / 2));
	const top = Math.min(height - side, Math.max(0, cy - side / 2));
	return { x: left / width, y: top / height, w: side / width, h: side / height };
}

/** Drops segments older than keepMs before now, then oldest until under maxBytes. */
export function pruneSegments(segments: Segment[], now: number, keepMs = KEEP_MS, maxBytes = MAX_BYTES): Segment[] {
	const kept = segments.filter((s) => s.end_ms >= now - keepMs);
	let bytes = kept.reduce((sum, s) => sum + s.blob.size, 0);
	while (kept.length > 1 && bytes > maxBytes) bytes -= kept.shift()!.blob.size;
	return kept;
}

export function pickMime(isTypeSupported: (type: string) => boolean): string | null {
	for (const type of MIME_TYPES) {
		try {
			if (isTypeSupported(type)) return type;
		} catch {
			/* keep looking */
		}
	}
	return null;
}

type Slot = { rec: RecorderLike; start: number | null; done: Promise<void> };

type Live = {
	stream: MediaStream;
	width: number;
	height: number;
	mime: string;
	current: Slot | null;
	/** The previous recorder while it overlaps the current one. */
	overlapping: RecorderLike | null;
	stopFaces?: () => void;
};

export class CameraRecorder {
	private segments: Segment[] = [];
	private faces: FaceSample[] = [];
	private live: Live | null = null;
	private disposed = false;
	private starting = false;
	private retryTimer: unknown = null;
	private statusTimer: unknown = null;
	private rotateTimer: unknown = null;
	private offRequest: (() => void) | null = null;
	/** Recorders that were asked to stop and have not delivered their blob yet. */
	private finishing = new Set<Promise<void>>();

	constructor(private deps: RecorderDeps) {}

	get active(): boolean {
		return this.live !== null;
	}

	/** Recorded segments (oldest first), for tests and diagnostics. */
	get recorded(): readonly Segment[] {
		return this.segments;
	}

	start() {
		if (this.disposed) return;
		this.offRequest ??= this.deps.bridge.on<RequestFrame>("clips.camera.request", (frame) => void this.answer(frame));
		void this.open();
	}

	dispose() {
		this.disposed = true;
		this.offRequest?.();
		this.offRequest = null;
		this.deps.clearTimeout(this.retryTimer);
		this.retryTimer = null;
		this.close(false);
		this.segments = [];
		this.faces = [];
	}

	private async open() {
		if (this.disposed || this.live || this.starting) return;
		this.starting = true;
		try {
			const Recorder = this.deps.recorderClass();
			const devices = this.deps.mediaDevices();
			const mime = Recorder ? pickMime((t) => Recorder.isTypeSupported(t)) : null;
			if (!Recorder || !devices?.getUserMedia || !mime) {
				this.report(false);
				return; // no way to record here; do not retry
			}
			let stream: MediaStream;
			try {
				stream = await devices.getUserMedia({ audio: false, video: { width: { ideal: 640 }, height: { ideal: 480 } } });
			} catch {
				this.report(false);
				this.retryLater();
				return;
			}
			if (this.disposed) {
				for (const track of stream.getTracks()) track.stop();
				return;
			}
			const track = stream.getVideoTracks()[0];
			const settings = track?.getSettings?.() ?? {};
			const live: Live = { stream, width: settings.width ?? 640, height: settings.height ?? 480, mime, current: null, overlapping: null };
			this.live = live;
			track?.addEventListener?.("ended", () => {
				if (this.live !== live) return;
				this.close(true);
				this.retryLater();
			});
			if (this.deps.trackFaces) {
				try {
					live.stopFaces = this.deps.trackFaces(stream, (face) => {
						if (face) this.faces.push({ at: this.deps.now(), x: face.x, y: face.y, size: face.size });
						this.faces = this.faces.filter((s) => s.at >= this.deps.now() - KEEP_MS);
					});
				} catch (error) {
					console.warn("clips: face tracking unavailable", error);
				}
			}
			this.rotate();
			this.rotateTimer = this.deps.setInterval(() => this.tick(), 250);
			this.report(true);
			this.statusTimer = this.deps.setInterval(() => this.report(this.active), STATUS_MS);
		} catch (error) {
			console.warn("clips: camera recorder failed", error);
			this.close(true);
			this.retryLater();
		} finally {
			this.starting = false;
		}
	}

	private retryLater() {
		if (this.disposed || this.retryTimer !== null) return;
		this.retryTimer = this.deps.setTimeout(() => {
			this.retryTimer = null;
			void this.open();
		}, RETRY_MS);
	}

	/** Stops recording; `report` tells the controller. Keeps already recorded segments. */
	private close(report: boolean) {
		const live = this.live;
		this.deps.clearInterval(this.rotateTimer);
		this.deps.clearInterval(this.statusTimer);
		this.rotateTimer = this.statusTimer = null;
		if (!live) return;
		this.live = null;
		live.stopFaces?.();
		if (live.overlapping) this.stopRecorder(live.overlapping);
		if (live.current) this.stopRecorder(live.current.rec);
		live.current = null;
		live.overlapping = null;
		for (const track of live.stream.getTracks()) track.stop();
		if (report) this.report(false);
	}

	private report(active: boolean) {
		const live = this.live;
		const params: Record<string, unknown> = { active };
		if (active && live) {
			params.width = live.width;
			params.height = live.height;
		}
		this.deps.bridge.send("clips.camera.status", params).catch(() => {});
	}

	/** Rotates when the current segment is old enough. Runs from the timer and from recorder events. */
	private tick() {
		const cur = this.live?.current;
		if (!cur || cur.start === null) return;
		if (this.deps.now() - cur.start >= SEGMENT_MS) this.rotate();
	}

	/** Starts the next recorder; the previous one stops OVERLAP_MS after the new one started. */
	private rotate() {
		const live = this.live;
		if (!live) return;
		const Recorder = this.deps.recorderClass();
		if (!Recorder) return;
		const prev = live.current;
		// A stop timer that lagged (throttled timers) must not leave two old recorders running.
		if (live.overlapping) {
			this.stopRecorder(live.overlapping);
			live.overlapping = null;
		}
		let rec: RecorderLike;
		try {
			rec = new Recorder(live.stream, { mimeType: live.mime, videoBitsPerSecond: 900_000 });
		} catch (error) {
			console.warn("clips: MediaRecorder failed", error);
			return;
		}
		const chunks: Blob[] = [];
		let endMs = 0;
		let resolveDone!: () => void;
		const done = new Promise<void>((resolve) => (resolveDone = resolve));
		const slot: Slot = { rec, start: null, done };
		rec.addEventListener("start", () => {
			slot.start = this.deps.now();
			if (!prev) return;
			live.overlapping = prev.rec;
			this.deps.setTimeout(() => {
				this.stopRecorder(prev.rec);
				if (live.overlapping === prev.rec) live.overlapping = null;
			}, OVERLAP_MS);
		});
		rec.addEventListener("dataavailable", (event: { data?: Blob }) => {
			if (event.data && event.data.size > 0) chunks.push(event.data);
		});
		rec.addEventListener("stop", () => {
			endMs = this.deps.now();
			if (slot.start !== null && chunks.length) {
				const blob = chunks.length === 1 ? chunks[0] : new Blob(chunks, { type: live.mime });
				this.segments.push({ start_ms: slot.start, end_ms: endMs, mime: live.mime.split(";")[0] || "video/webm", blob });
				this.segments = pruneSegments(this.segments, endMs);
			}
			resolveDone();
			// Chrome throttles timers while a native game covers the window;
			// recorder events still arrive, so they keep the ring turning.
			this.tick();
		});
		rec.addEventListener("error", () => resolveDone());
		this.finishing.add(done);
		void done.then(() => this.finishing.delete(done));
		live.current = slot;
		try {
			rec.start();
		} catch (error) {
			console.warn("clips: MediaRecorder start failed", error);
			live.current = prev;
			resolveDone();
		}
	}

	private stopRecorder(rec: RecorderLike) {
		try {
			if (rec.state !== "inactive") rec.stop();
		} catch {
			/* already stopped */
		}
	}

	/** Stops of earlier recorders still to deliver their blob (not the one recording now). */
	private pendingStops(): Promise<void>[] {
		const current = this.live?.current?.done;
		return [...this.finishing].filter((p) => p !== current);
	}

	/** Ends the segment in progress now (a new one starts), and waits for its blob. */
	private async flush(waitMs: number) {
		if (this.live?.current) this.rotate();
		const pending = this.pendingStops();
		if (pending.length) await Promise.race([Promise.all(pending), this.sleep(waitMs)]);
	}

	private sleep(ms: number) {
		return new Promise<void>((resolve) => this.deps.setTimeout(resolve, ms));
	}

	/** Answers `clips.camera.request`. */
	async answer(frame: RequestFrame) {
		const reqId = typeof frame?.req_id === "string" ? frame.req_id : "";
		if (!reqId) return;
		const from = Number(frame.from_ms);
		const to = Number(frame.to_ms);
		const fail = (error: string) =>
			this.deps.bridge.send("clips.camera.deliver", { req_id: reqId, segments: [], crop: null, error }).catch(() => {});
		if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return fail("bad window");
		if (!this.live && !this.segments.length) return fail("camera off");
		try {
			// The segment holding `to` may still be recording.
			const cur = this.live?.current;
			if (cur && cur.start !== null && cur.start <= to) await this.flush(REQUEST_WAIT_MS);
			else if (this.pendingStops().length) await Promise.race([Promise.all(this.pendingStops()), this.sleep(REQUEST_WAIT_MS)]);
			const picked = this.segments.filter((s) => s.end_ms > from && s.start_ms < to);
			if (!picked.length) return fail("no camera footage for that moment");
			const segments = [];
			for (const s of picked) {
				segments.push({ start_ms: s.start_ms, end_ms: s.end_ms, mime: s.mime, data: await this.deps.encode(s.blob) });
			}
			const size = this.live ?? { width: 640, height: 480 };
			const crop = faceCrop(this.faces, from, to, size.width, size.height);
			await this.deps.bridge.send("clips.camera.deliver", { req_id: reqId, segments, crop });
		} catch (error) {
			console.warn("clips: camera delivery failed", error);
			void fail("camera delivery failed");
		}
	}
}

/** Base64 of a Blob, without the data: prefix. */
export function blobToBase64(blob: Blob): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.onload = () => {
			const url = String(reader.result);
			resolve(url.slice(url.indexOf(",") + 1));
		};
		reader.onerror = () => reject(reader.error);
		reader.readAsDataURL(blob);
	});
}

/** Face tracking on a hidden video, like the apps' camera host does, at ~5 Hz. */
export function browserFaceTracker(resolveUrl: (url: string) => string) {
	return (stream: MediaStream, sample: (face: { x: number; y: number; size: number } | null) => void) => {
		let stopped = false;
		let timer: ReturnType<typeof setInterval> | undefined;
		const video = document.createElement("video");
		video.muted = true;
		video.playsInline = true;
		video.srcObject = stream;
		void video.play().catch(() => {});
		void Promise.all([import("../../../../pad/src/face/face"), import("../../../../pad/src/face/tracker")])
			.then(async ([{ loadFaceFinder, toGray }, { FaceTracker }]) => {
				const classifier = await loadFaceFinder(resolveUrl);
				if (stopped) return;
				const tracker = new FaceTracker(classifier);
				let context: CanvasRenderingContext2D | null = null;
				let lastFrame = -1;
				timer = setInterval(() => {
					if (video.readyState < 2 || !video.videoWidth || video.currentTime === lastFrame) return;
					lastFrame = video.currentTime;
					try {
						const scale = Math.min(1, 192 / video.videoWidth);
						const width = Math.max(1, Math.round(video.videoWidth * scale));
						const height = Math.max(1, Math.round(video.videoHeight * scale));
						if (context?.canvas.width !== width || context.canvas.height !== height) {
							const canvas = document.createElement("canvas");
							canvas.width = width;
							canvas.height = height;
							context = canvas.getContext("2d", { willReadFrequently: true });
						}
						if (!context) return;
						context.drawImage(video, 0, 0, width, height);
						sample(tracker.update(toGray(context.getImageData(0, 0, width, height).data, width, height)));
					} catch {
						sample(null);
					}
				}, FACE_EVERY_MS);
			})
			.catch((error) => console.warn("clips: face finder unavailable", error));
		return () => {
			stopped = true;
			clearInterval(timer);
			video.srcObject = null;
		};
	};
}

/** Starts the ring recorder with the real browser APIs. Returns its dispose. */
export function startCameraRecorder(bridge: RecorderBridge, resolveUrl: (url: string) => string): () => void {
	const recorder = new CameraRecorder({
		bridge,
		mediaDevices: () => (typeof navigator === "undefined" ? undefined : navigator.mediaDevices),
		recorderClass: () => (typeof MediaRecorder === "undefined" ? null : (MediaRecorder as unknown as ReturnType<RecorderDeps["recorderClass"]>)),
		now: () => Date.now(),
		setTimeout: (fn, ms) => window.setTimeout(fn, ms),
		clearTimeout: (h) => window.clearTimeout(h as number),
		setInterval: (fn, ms) => window.setInterval(fn, ms),
		clearInterval: (h) => window.clearInterval(h as number),
		trackFaces: browserFaceTracker(resolveUrl),
		encode: blobToBase64,
	});
	recorder.start();
	return () => recorder.dispose();
}
