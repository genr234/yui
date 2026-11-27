// The atlas is bundled as a data URL and handed over as bytes, so a host
// page's Content-Security-Policy cannot keep avatars from drawing.
import { configureAvatars, dataUrlBlob } from "../../avatar/src";
import avatarAtlas from "../../avatar/src/atlas.png";
import App from "./App.svelte";
import { platformAssetUrl } from "./assets";
import { loadPlatformFonts } from "./fonts";
import appsCss from "./styles/apps.css?inline";
import attractCss from "./styles/attract.css?inline";
import barsCss from "./styles/bars.css?inline";
import calibrateCss from "./styles/calibrate.css?inline";
import baseCss from "./styles/base.css?inline";
import cameraCss from "./styles/camera.css?inline";
import dashboardCss from "./styles/dashboard.css?inline";
import interactionsCss from "./styles/interactions.css?inline";
import joinCss from "./styles/join.css?inline";
import keyboardCss from "./styles/keyboard.css?inline";
import overlayPermissionsCss from "./styles/overlay-permissions.css?inline";
import padCursorCss from "./styles/pad-cursor.css?inline";
import pluginsCss from "./styles/plugins.css?inline";
import settingsCss from "./styles/settings.css?inline";
import shellCss from "./styles/shell.css?inline";
import topbarCss from "./styles/topbar.css?inline";
import tokensCss from "./styles/tokens.css?inline";
import workspaceCss from "./styles/workspace.css?inline";
import yuiAppCss from "./styles/yui-app.css?inline";

const css = [
	baseCss,
	tokensCss,
	overlayPermissionsCss,
	keyboardCss,
	shellCss,
	topbarCss,
	joinCss,
	workspaceCss,
	dashboardCss,
	appsCss,
	settingsCss,
	pluginsCss,
	yuiAppCss,
	cameraCss,
	interactionsCss,
	barsCss,
	calibrateCss,
	padCursorCss,
	attractCss,
].join("\n");

declare global {
	interface Window {
		__YUI_APP__?: {
			destroy: () => void;
		};
		__YUI_BRIDGE_URL?: string;
		__YUI_PLATFORM_HTTP?: string;
		__YUI_PLATFORM_HTTP_TOKEN?: string;
		__YUI_PLATFORM_DEV_SERVER?: string;
	}
}

const hostId = "yui-platform-host";

function mount() {
	if (document.getElementById(hostId)) {
		return;
	}

	const host = document.createElement("div");
	host.id = hostId;
	host.style.position = "fixed";
	host.style.inset = "0";
	host.style.zIndex = "2147483647";
	host.style.pointerEvents = "none";

	const shadow = host.attachShadow({ mode: "open" });
	const target = document.createElement("div");
	target.id = "yui-shadow-root";
	// A constructed stylesheet is not subject to the host page's
	// Content-Security-Policy; a <style> element is held to its style-src.
	let adopted = false;
	if ("adoptedStyleSheets" in shadow && typeof CSSStyleSheet === "function") {
		try {
			const sheet = new CSSStyleSheet();
			sheet.replaceSync(css);
			shadow.adoptedStyleSheets = [sheet];
			adopted = true;
		} catch {
			// fall back to a <style> element
		}
	}
	if (!adopted) {
		const style = document.createElement("style");
		style.textContent = css;
		shadow.append(style);
	}
	shadow.append(target);
	document.documentElement.appendChild(host);

	void loadPlatformFonts();
	const atlasData = dataUrlBlob(avatarAtlas);
	configureAvatars(atlasData ? { atlasData } : { atlasUrl: platformAssetUrl(avatarAtlas) });
	const app = new App({ target });
	window.__YUI_APP__ = {
		destroy: () => {
			app.$destroy();
			host.remove();
		},
	};
}

if (document.readyState === "loading") {
	document.addEventListener("DOMContentLoaded", mount, { once: true });
} else {
	mount();
}
