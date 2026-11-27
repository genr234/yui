// When the host page's Content-Security-Policy blocks the overlay from
// reaching the controller (bridge websocket, assets, embed proxy), tell the
// controller so it can reload the page with the CSP bypassed. The controller
// adds the binding when it attaches; if that has not happened yet, leave a
// flag it checks when it does. See bypassCSP in controller/internal/platform.

declare global {
	interface Window {
		__YUI_CSP_BLOCKED?: boolean;
		__yuiCspBlocked?: (payload: string) => void;
	}
}

function host(url: string | undefined): string | null {
	if (!url) return null;
	try {
		return new URL(url).host;
	} catch {
		return null;
	}
}

/** Whether a blocked URI points at the controller. */
export function blocksController(blockedURI: string, controllerHosts: (string | null)[]): boolean {
	const blocked = host(blockedURI);
	return !!blocked && controllerHosts.includes(blocked);
}

export function watchForCSPBlocks(): void {
	if (window.__YUI_CSP_BLOCKED !== undefined) return;
	window.__YUI_CSP_BLOCKED = false;
	document.addEventListener("securitypolicyviolation", (event) => {
		if (window.__YUI_CSP_BLOCKED) return;
		const controller = [host(window.__YUI_BRIDGE_URL), host(window.__YUI_PLATFORM_HTTP), host(window.__YUI_PLATFORM_DEV_SERVER)];
		if (!blocksController(event.blockedURI, controller)) return;
		window.__YUI_CSP_BLOCKED = true;
		if (typeof window.__yuiCspBlocked === "function") window.__yuiCspBlocked(location.href);
	});
}
