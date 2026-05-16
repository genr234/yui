import type { SidebarThemeSettings } from "./types";

const storageKey = "yui.sidebar-theme.v0";

export const defaultSidebarTheme: SidebarThemeSettings = {
	enabled: true,
	images: [],
};

export function loadSidebarTheme(): SidebarThemeSettings {
	if (typeof localStorage === "undefined") return defaultSidebarTheme;
	try {
		const parsed = JSON.parse(localStorage.getItem(storageKey) ?? "null");
		if (!parsed || typeof parsed !== "object") return defaultSidebarTheme;
		return {
			enabled: parsed.enabled !== false,
			images: Array.isArray(parsed.images)
				? parsed.images.filter(
						(image: unknown) =>
							image &&
							typeof image === "object" &&
							typeof (image as { id?: unknown }).id === "string" &&
							typeof (image as { name?: unknown }).name === "string" &&
							typeof (image as { src?: unknown }).src === "string",
					)
				: [],
		};
	} catch {
		return defaultSidebarTheme;
	}
}

export function saveSidebarTheme(theme: SidebarThemeSettings) {
	localStorage.setItem(storageKey, JSON.stringify(theme));
	window.dispatchEvent(
		new CustomEvent<SidebarThemeSettings>("yui:sidebar-theme-changed", {
			detail: theme,
		}),
	);
}
