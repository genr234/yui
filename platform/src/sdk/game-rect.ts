/**
 * Where the running game is on screen, for the letterbox bars
 * (components/bars). The area is in viewport CSS pixels, or null when no game
 * is running in a window that leaves room around it.
 *
 * Two generic sources, no app-specific code:
 *  - web apps: `ctx.ui.embed({ web, aspect })` reports its frame while the
 *    shell is fullscreen (WebEmbed -> reportWebRect);
 *  - native apps: the controller's `native.window` event (physical screen
 *    pixels) converted with `window.innerWidth / screen.w`.
 */
import { derived, get, writable, type Readable } from "svelte/store";
import { bridge } from "./bridge";

export type Rect = { x: number; y: number; w: number; h: number };

/** The app that owns the foreground (set by AppHost). `bars: false` is its manifest opt-out. */
export type BarsApp = { id: string; name: string; bars?: boolean };

/** Smallest bar worth drawing, in CSS px. */
export const MIN_BAR_PX = 80;

/** `"16:9"`, `"4:3"` or a number (width / height). Returns undefined when it is not a usable ratio. */
export function parseAspect(value: unknown): number | undefined {
	if (typeof value === "number") return Number.isFinite(value) && value > 0 ? value : undefined;
	if (typeof value !== "string") return undefined;
	const match = /^\s*(\d+(?:\.\d+)?)\s*[:/]\s*(\d+(?:\.\d+)?)\s*$/.exec(value);
	if (!match) return undefined;
	const w = Number(match[1]);
	const h = Number(match[2]);
	return w > 0 && h > 0 ? w / h : undefined;
}

/** The largest `aspect` (w / h) rectangle that fits `avail`, centred in it. */
export function fitAspect(avail: Rect, aspect: number): Rect {
	if (!(avail.w > 0 && avail.h > 0) || !(aspect > 0)) return { x: avail.x, y: avail.y, w: 0, h: 0 };
	let w = avail.w;
	let h = w / aspect;
	if (h > avail.h) {
		h = avail.h;
		w = h * aspect;
	}
	return { x: avail.x + (avail.w - w) / 2, y: avail.y + (avail.h - h) / 2, w, h };
}

/** The `native.window` event: rect and screen are physical screen pixels; rect null = the window is fullscreen. */
export type NativeWindow = {
	app_id: string;
	rect: Rect | null;
	screen: { w: number; h: number };
};

const finite = (n: unknown): n is number => typeof n === "number" && Number.isFinite(n);

/** Physical pixels -> CSS px (the kiosk page is fullscreen, so the width ratio is the page scale). Null for a fullscreen or malformed window. */
export function nativeRectToCss(win: Pick<NativeWindow, "rect" | "screen">, innerWidth: number): Rect | null {
	const { rect, screen } = win;
	if (!rect || !screen || !finite(screen.w) || screen.w <= 0 || !finite(innerWidth) || innerWidth <= 0) return null;
	if (![rect.x, rect.y, rect.w, rect.h].every(finite) || rect.w <= 0 || rect.h <= 0) return null;
	const k = innerWidth / screen.w;
	return { x: rect.x * k, y: rect.y * k, w: rect.w * k, h: rect.h * k };
}

/** The space the game leaves above and below, as bar rectangles. A bar under `min` is dropped; null when neither is left. */
export function barRects(game: Rect | null, viewport: { w: number; h: number }, min = MIN_BAR_PX): { top: Rect | null; bottom: Rect | null } | null {
	if (!game || !(viewport.w > 0 && viewport.h > 0)) return null;
	const topH = Math.max(0, game.y);
	const bottomY = game.y + game.h;
	const bottomH = Math.max(0, viewport.h - bottomY);
	const top = topH >= min ? { x: 0, y: 0, w: viewport.w, h: topH } : null;
	const bottom = bottomH >= min ? { x: 0, y: bottomY, w: viewport.w, h: bottomH } : null;
	return top || bottom ? { top, bottom } : null;
}

const activeApp = writable<BarsApp | null>(null);
const shellFullscreen = writable(false);
const webRect = writable<{ appId: string; rect: Rect } | null>(null);
const nativeRect = writable<{ appId: string; rect: Rect } | null>(null);

if (typeof window !== "undefined") {
	window.addEventListener("yui:shell-fullscreen", (event) => {
		shellFullscreen.set(Boolean((event as CustomEvent<{ active: boolean }>).detail?.active));
	});
}

/** AppHost: the app that is open (null when it closes). */
export function setBarsApp(app: BarsApp | null) {
	activeApp.set(app);
	if (!app) {
		webRect.set(null);
		nativeRect.set(null);
	}
}

/** WebEmbed: its aspect-fitted frame in viewport CSS px, or null when it goes away. */
export function reportWebRect(appId: string, rect: Rect | null) {
	webRect.set(rect && rect.w > 0 && rect.h > 0 ? { appId, rect } : null);
}

/** Feed a `native.window` event. Ignored unless it is for the open app. */
export function applyNativeWindow(win: NativeWindow | null | undefined, innerWidth = window.innerWidth) {
	if (!win || typeof win.app_id !== "string") return;
	if (get(activeApp)?.id !== win.app_id) return;
	const rect = nativeRectToCss(win, innerWidth);
	nativeRect.set(rect ? { appId: win.app_id, rect } : null);
}

/** Subscribe to the controller's native events; returns an unsubscribe function. */
export function watchNativeWindow(): () => void {
	const offWindow = bridge.on<NativeWindow>("native.window", (data) => applyNativeWindow(data));
	const offStatus = bridge.on<{ app_id?: string; state?: string }>("native.status", (data) => {
		const state = data?.state;
		if (state === "exited" || state === "crashed" || state === "failed" || state === "idle") {
			if (get(nativeRect)?.appId === data?.app_id) nativeRect.set(null);
		}
	});
	return () => {
		offWindow();
		offStatus();
	};
}

/** The game's area in viewport CSS px, or null. A native window wins; a web frame counts only while the shell is fullscreen. */
export const gameRect: Readable<Rect | null> = derived(
	[activeApp, shellFullscreen, webRect, nativeRect],
	([$app, $fullscreen, $web, $native]) => {
		if (!$app) return null;
		if ($native && $native.appId === $app.id) return $native.rect;
		if ($fullscreen && $web && $web.appId === $app.id) return $web.rect;
		return null;
	},
);

/** The open app, for bar items (title) and the `bars: false` opt-out. */
export const barsApp: Readable<BarsApp | null> = { subscribe: activeApp.subscribe };

/** True while an app has the shell fullscreen (`yui:shell-fullscreen`). */
export const shellFullscreenState: Readable<boolean> = { subscribe: shellFullscreen.subscribe };

// "Snap the screen": the controller reads this global over CDP when a phone
// asks for a picture, to clip the page screenshot to the game. null = the
// whole page.
declare global {
	interface Window {
		__yuiGameRect?: Rect | null;
	}
}
if (typeof window !== "undefined") {
	window.__yuiGameRect = null;
	gameRect.subscribe((rect) => {
		window.__yuiGameRect = rect ? { x: rect.x, y: rect.y, w: rect.w, h: rect.h } : null;
	});
}
