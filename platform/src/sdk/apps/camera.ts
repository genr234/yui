/**
 * Host side of `ctx.camera`: the kiosk's own camera, for apps that declare
 * the `camera` permission.
 *
 * The sandboxed app cannot call getUserMedia, so the overlay owns the stream.
 * The app gets handles, normalized face positions, JPEG stills (`snapshot`) and a `ctx.ui.camera(...)`
 * preview node that shows the stream on the host side; live video never
 * crosses the sandbox boundary, and nothing here uploads a frame.
 *
 * At most one stream exists on the kiosk. `liveCamera` / `watchCamera` let
 * the overlay's own parts (the "Camera on" badge, the preview node) follow
 * it. The stream stops when the last handle is stopped, when the app's ctx
 * is disposed, and when the app's permission is taken away.
 */
import type {
	YuiCameraApi,
	YuiCameraErrorCode,
	YuiCameraFace,
	YuiCameraHandle,
	YuiCameraOptions,
	YuiCameraState,
	YuiPermission,
	YuiSimpleApp,
} from "./types";
import { isPermissionDeclared } from "./permissions";
import { platformAssetUrl } from "../../assets";
import { createFaceLock } from "./face-lock";

type Ensure = (permission: YuiPermission) => Promise<void>;

/** The bits of the DOM the host uses, so tests can fake them. */
export type CameraDeps = {
	mediaDevices(): Pick<MediaDevices, "getUserMedia" | "enumerateDevices"> | undefined;
	createVideo(): HTMLVideoElement;
	createCanvas(width: number, height: number): HTMLCanvasElement;
};

const defaultDeps: CameraDeps = {
	mediaDevices: () => (typeof navigator === "undefined" ? undefined : navigator.mediaDevices),
	createVideo: () => document.createElement("video"),
	createCanvas: (width, height) => {
		const canvas = document.createElement("canvas");
		canvas.width = width;
		canvas.height = height;
		return canvas;
	},
};

export type LiveCamera = {
	appId: string;
	appName: string;
	stream: MediaStream;
	width: number;
	height: number;
};

let live: (LiveCamera & { frames?: HTMLVideoElement }) | null = null;
const watchers = new Set<(camera: LiveCamera | null) => void>();

/** The running camera, or null. */
export function liveCamera(): LiveCamera | null {
	return live;
}

/** Calls `fn` now and whenever the camera starts or stops. */
export function watchCamera(fn: (camera: LiveCamera | null) => void): () => void {
	watchers.add(fn);
	fn(live);
	return () => watchers.delete(fn);
}

function setLive(next: typeof live) {
	live = next;
	for (const fn of [...watchers]) fn(next);
}

function cameraError(code: YuiCameraErrorCode) {
	return new Error(`YUI_CAMERA: ${code}`);
}

/** Maps getUserMedia's DOMException names to a code (same names as the phone pad's). */
export function cameraErrorCode(error: unknown): YuiCameraErrorCode {
	switch ((error as { name?: unknown } | null)?.name) {
		case "NotAllowedError":
		case "SecurityError":
			return "blocked";
		case "NotFoundError":
		case "OverconstrainedError":
			return "not_found";
		case "NotReadableError":
		case "AbortError":
			return "busy";
		default:
			return "failed";
	}
}

const defaultSnapshotWidth = 640;
const maxSnapshotWidth = 1280;

/** A still's size: scaled down to `maxWidth` (default 640, at most 1280), never enlarged. */
export function snapshotSize(sourceWidth: number, sourceHeight: number, maxWidth?: number) {
	const wanted = typeof maxWidth === "number" && Number.isFinite(maxWidth) ? maxWidth : defaultSnapshotWidth;
	const limit = Math.min(maxSnapshotWidth, Math.max(16, Math.round(wanted)));
	const scale = Math.min(1, limit / sourceWidth);
	return {
		width: Math.max(1, Math.round(sourceWidth * scale)),
		height: Math.max(1, Math.round(sourceHeight * scale)),
	};
}

function clampSize(value: unknown, fallback: number) {
	return typeof value === "number" && Number.isFinite(value) ? Math.min(1920, Math.max(64, Math.round(value))) : fallback;
}

function waitForFrame(video: HTMLVideoElement, timeoutMs = 3000): Promise<void> {
	if (video.videoWidth > 0 && video.readyState >= 2) return Promise.resolve();
	return new Promise((resolve, reject) => {
		const timer = setTimeout(() => {
			video.removeEventListener("loadeddata", done);
			reject(cameraError("failed"));
		}, timeoutMs);
		const done = () => {
			clearTimeout(timer);
			resolve();
		};
		video.addEventListener("loadeddata", done, { once: true });
	});
}

export type CameraHost = {
	api: YuiCameraApi;
	/** Turns the camera off if this app has it on, and drops subscriptions. */
	dispose(): void;
};

export function createCameraHost(
	app: YuiSimpleApp,
	ensure: Ensure,
	/** Synchronous check used by `snapshot` and `on`, which cannot prompt. */
	requireGranted: (permission: YuiPermission) => void,
	deps: CameraDeps = defaultDeps,
): CameraHost {
	const handles = new Set<string>();
	const listeners = new Set<(state: YuiCameraState) => void>();
	const faceListeners = new Set<(face: YuiCameraFace | null) => void>();
	let disposed = false;
	let starting: Promise<void> | null = null;
	let counter = 0;
	let offPermissions: (() => void) | undefined;
	let stopTracking: (() => void) | undefined;

	const owns = () => live !== null && live.appId === app.id;
	const emit = (state: YuiCameraState) => {
		for (const fn of [...listeners]) {
			try {
				fn(state);
			} catch (error) {
				console.error(error);
			}
		}
	};

	function frameVideo(current: NonNullable<typeof live>) {
		if (!current.frames) {
			const video = deps.createVideo();
			video.muted = true;
			video.playsInline = true;
			video.srcObject = current.stream;
			current.frames = video;
			void video.play().catch(() => {});
		}
		return current.frames;
	}

	function trackFaces() {
		if (stopTracking || !live || !owns() || !faceListeners.size) return;
		const current = live;
		let stopped = false;
		let timer: ReturnType<typeof setInterval> | undefined;
		const publish = (face: YuiCameraFace | null) => {
			for (const fn of [...faceListeners]) {
				try { fn(face); } catch (error) { console.error(error); }
			}
		};
		stopTracking = () => {
			stopped = true;
			clearInterval(timer);
			stopTracking = undefined;
		};
		void import("../../../../pad/src/face/face").then(async ({ findFaces, loadFaceFinder, toGray }) => {
			const classifier = await loadFaceFinder(platformAssetUrl);
			if (stopped || live !== current) return;
			const video = frameVideo(current);
			let context: CanvasRenderingContext2D | null = null;
			let lastFrame = -1;
			let lastFrameAt = 0;
			const track = createFaceLock();
			timer = setInterval(() => {
				if (video.readyState < 2 || !video.videoWidth || video.currentTime === lastFrame) {
					if (Date.now() - lastFrameAt > 250) publish(null);
					return;
				}
				lastFrame = video.currentTime;
				lastFrameAt = Date.now();
				try {
					const { width, height } = snapshotSize(video.videoWidth, video.videoHeight, 192);
					if (context?.canvas.width !== width || context.canvas.height !== height) {
						context = deps.createCanvas(width, height).getContext("2d", { willReadFrequently: true });
					}
					if (!context) throw cameraError("failed");
					context.drawImage(video, 0, 0, width, height);
					const gray = toGray(context.getImageData(0, 0, width, height).data, width, height);
					const faces = findFaces(gray, classifier, 0.18).map(({ x, y, size }) => {
						const pixels = size * Math.min(width, height);
						return { x, y, width: pixels / width, height: pixels / height };
					});
					publish(track(faces, lastFrameAt));
				} catch {
					publish(null);
				}
			}, 50);
		}).catch(() => { if (!stopped) publish(null); });
	}

	/** Ends the stream (if this app has it) and every handle. */
	function shutDown(state: YuiCameraState = { state: "closed" }) {
		stopTracking?.();
		handles.clear();
		offPermissions?.();
		offPermissions = undefined;
		if (!owns() || !live) return;
		const current = live;
		for (const track of current.stream.getTracks()) track.stop();
		if (current.frames) current.frames.srcObject = null;
		setLive(null);
		emit(state);
	}

	// Taking the permission away turns the camera off at once.
	function watchPermission() {
		if (offPermissions || typeof window === "undefined") return;
		const check = () => {
			try {
				requireGranted("camera");
			} catch {
				shutDown();
			}
		};
		window.addEventListener("yui:permissions-changed", check);
		offPermissions = () => window.removeEventListener("yui:permissions-changed", check);
	}

	async function start(options: YuiCameraOptions | undefined) {
		const devices = deps.mediaDevices();
		if (!devices?.getUserMedia) throw cameraError("unsupported");
		let stream: MediaStream;
		try {
			stream = await devices.getUserMedia({
				audio: false,
				video: {
					width: { ideal: clampSize(options?.width, 640) },
					height: { ideal: clampSize(options?.height, 480) },
					...(options?.facing === "user" || options?.facing === "environment"
						? { facingMode: { ideal: options.facing } }
						: {}),
				},
			});
		} catch (error) {
			throw cameraError(cameraErrorCode(error));
		}
		try {
			if (disposed) throw cameraError("failed");
			if (live && !owns()) throw cameraError("busy");
			requireGranted("camera");
		} catch (error) {
			for (const track of stream.getTracks()) track.stop();
			throw error;
		}
		const track = stream.getVideoTracks()[0];
		const settings = track?.getSettings?.() ?? {};
		setLive({
			appId: app.id,
			appName: app.name,
			stream,
			width: settings.width ?? clampSize(options?.width, 640),
			height: settings.height ?? clampSize(options?.height, 480),
		});
		track?.addEventListener("ended", () => {
			if (live?.stream === stream) shutDown({ state: "error", error: "failed" });
		});
		watchPermission();
		emit({ state: "open", width: live?.width, height: live?.height });
		trackFaces();
	}

	const api: YuiCameraApi = {
		async available() {
			if (!isPermissionDeclared(app, "camera")) throw new Error("YUI_PERMISSION_DENIED: camera permission is required");
			const devices = deps.mediaDevices();
			if (!devices?.getUserMedia) return false;
			try {
				return (await devices.enumerateDevices()).some((device) => device.kind === "videoinput");
			} catch {
				return false;
			}
		},
		async open(options) {
			await ensure("camera");
			if (disposed) throw cameraError("failed");
			if (live && !owns()) throw cameraError("busy");
			if (!owns()) {
				// Two opens at once share one getUserMedia call.
				starting ??= start(options).finally(() => {
					starting = null;
				});
				await starting;
			} else if (starting) {
				await starting;
			}
			if (disposed || !live || !owns()) throw cameraError("failed");
			const id = `cam${++counter}`;
			handles.add(id);
			const handle: YuiCameraHandle = {
				id,
				width: live.width,
				height: live.height,
				stop: async () => {
					if (!handles.delete(id)) return;
					if (handles.size === 0) shutDown();
				},
			};
			return handle;
		},
		async snapshot(options) {
			requireGranted("camera");
			if (!owns() || !live || handles.size === 0) throw cameraError("not_open");
			const current = live;
			const video = frameVideo(current);
			await waitForFrame(video);
			if (live !== current) throw cameraError("not_open");
			const size = snapshotSize(video.videoWidth, video.videoHeight, options?.maxWidth);
			const canvas = deps.createCanvas(size.width, size.height);
			const context = canvas.getContext("2d");
			if (!context) throw cameraError("failed");
			context.drawImage(video, 0, 0, size.width, size.height);
			const quality =
				typeof options?.quality === "number" && Number.isFinite(options.quality)
					? Math.min(0.95, Math.max(0.3, options.quality))
					: 0.8;
			return { dataUrl: canvas.toDataURL("image/jpeg", quality), width: size.width, height: size.height };
		},
		on(event: "state" | "face", handler: ((state: YuiCameraState) => void) | ((face: YuiCameraFace | null) => void)) {
			requireGranted("camera");
			if (disposed) return () => {};
			if (event === "face") {
				const listener = handler as (face: YuiCameraFace | null) => void;
				faceListeners.add(listener);
				trackFaces();
				return () => {
					faceListeners.delete(listener);
					if (!faceListeners.size) stopTracking?.();
				};
			}
			if (event !== "state") throw new Error(`YUI_CAMERA: unknown event ${String(event)}`);
			const listener = handler as (state: YuiCameraState) => void;
			listeners.add(listener);
			return () => listeners.delete(listener);
		},
	};

	return {
		api,
		dispose() {
			disposed = true;
			shutDown();
			listeners.clear();
			faceListeners.clear();
		},
	};
}
