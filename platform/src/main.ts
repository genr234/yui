import App from "./App.svelte";
import appsCss from "./styles/apps.css?inline";
import baseCss from "./styles/base.css?inline";
import dashboardCss from "./styles/dashboard.css?inline";
import interactionsCss from "./styles/interactions.css?inline";
import keyboardCss from "./styles/keyboard.css?inline";
import overlayPermissionsCss from "./styles/overlay-permissions.css?inline";
import pluginsCss from "./styles/plugins.css?inline";
import settingsCss from "./styles/settings.css?inline";
import shellCss from "./styles/shell.css?inline";
import sidebarCss from "./styles/sidebar.css?inline";
import workspaceCss from "./styles/workspace.css?inline";
import yuiAppCss from "./styles/yui-app.css?inline";

const css = [
	baseCss,
	overlayPermissionsCss,
	keyboardCss,
	shellCss,
	sidebarCss,
	workspaceCss,
	dashboardCss,
	appsCss,
	settingsCss,
	pluginsCss,
	yuiAppCss,
	interactionsCss,
].join("\n");

declare global {
	interface Window {
		__YUI_APP__?: {
			destroy: () => void;
		};
		__YUI_BRIDGE_URL?: string;
		__YUI_PLATFORM_HTTP?: string;
		__YUI_PLATFORM_HTTP_TOKEN?: string;
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
	const style = document.createElement("style");
	style.textContent = css;
	const target = document.createElement("div");
	target.id = "yui-shadow-root";
	shadow.append(style, target);
	document.documentElement.appendChild(host);

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
