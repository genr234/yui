import type { YuiNativeSpec } from "./types";

/** Validates `spec.window` and returns its wire form; undefined when absent. */
export function nativeWindowToWire(window: YuiNativeSpec["window"]): { aspect: number; align: "center" } | undefined {
	if (window === undefined || window === null) return undefined;
	if (typeof window !== "object") throw new Error("native window must be an object");
	let aspect: number;
	if (typeof window.aspect === "number") {
		aspect = window.aspect;
	} else if (typeof window.aspect === "string") {
		const match = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/.exec(window.aspect);
		if (!match) throw new Error('native window aspect must be a number or a ratio like "16:9"');
		aspect = Number(match[1]) / Number(match[2]);
	} else {
		throw new Error('native window aspect must be a number or a ratio like "16:9"');
	}
	if (!Number.isFinite(aspect) || aspect < 0.2 || aspect > 5) {
		throw new Error("native window aspect must be between 0.2 and 5");
	}
	if (window.align !== undefined && window.align !== "center") {
		throw new Error('native window align must be "center"');
	}
	return { aspect, align: "center" };
}
