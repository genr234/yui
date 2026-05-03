import { createUiApi } from "./ui";
import type { YuiContext, YuiPermission, YuiSimpleApp } from "./types";
import { isPermissionDeclared, isPermissionGranted, requestAppPermission } from "./permissions";
import { bridge } from "../bridge";

function permissionError(permission: YuiPermission) {
	return new Error(`YUI_PERMISSION_DENIED: ${permission} permission is required`);
}

let shellFullscreenActive = false;

window.addEventListener("yui:shell-fullscreen", (event) => {
	shellFullscreenActive = Boolean((event as CustomEvent<{ active: boolean }>).detail?.active);
});

function setShellFullscreen(app: YuiSimpleApp, active: boolean) {
	shellFullscreenActive = active;
	window.dispatchEvent(
		new CustomEvent("yui:shell-fullscreen", {
			detail: { active, appId: app.id },
		}),
	);
}

function isShellFullscreen() {
	return shellFullscreenActive;
}

function scheduleOnce(callback: () => void) {
	let scheduled = false;
	return () => {
		if (scheduled) return;
		scheduled = true;
		queueMicrotask(() => {
			scheduled = false;
			callback();
		});
	};
}

function createState<T extends object>(initial: T, scheduleRender: () => void): T {
	return new Proxy(initial, {
		set(target, key, value) {
			Reflect.set(target, key, value);
			scheduleRender();
			return true;
		},
		deleteProperty(target, key) {
			Reflect.deleteProperty(target, key);
			scheduleRender();
			return true;
		},
	});
}

type NetworkFetchBridgeResult = {
	url: string;
	status: number;
	headers?: Record<string, string[] | string>;
	body?: string;
};

function responseHeadersFromBridge(headers: NetworkFetchBridgeResult["headers"]) {
	const result = new Headers();
	for (const [key, value] of Object.entries(headers ?? {})) {
		if (Array.isArray(value)) {
			for (const item of value) {
				result.append(key, item);
			}
		} else {
			result.set(key, String(value));
		}
	}
	return result;
}

async function fetchThroughBridge(url: string, options?: RequestInit) {
	if (options?.body && typeof options.body !== "string") {
		throw new Error("YUI_NETWORK_ERROR: bridge fetch only supports string request bodies");
	}

	const headers: Record<string, string> = {};
	new Headers(options?.headers).forEach((value, key) => {
		headers[key] = value;
	});

	const result = await bridge.send<NetworkFetchBridgeResult>("network.fetch", {
		url,
		method: options?.method,
		headers,
		body: options?.body ?? "",
	});

	return new Response(result.body ?? "", {
		status: result.status,
		headers: responseHeadersFromBridge(result.headers),
	});
}

export function createYuiContext(app: YuiSimpleApp, onRender: () => void): YuiContext {
	const disposables = new Set<() => void>();
	const eventHandlers = new Map<string, Set<(data: unknown) => void | Promise<void>>>();
	const storagePrefix = `yui.simple-app.${app.id}.`;
	const scheduleRender = scheduleOnce(onRender);

	const ensurePermission = async (permission: YuiPermission) => {
		if (!isPermissionDeclared(app, permission)) {
			throw permissionError(permission);
		}
		if (isPermissionGranted(app.id, permission)) {
			return;
		}
		if (!(await requestAppPermission(app, permission))) {
			throw permissionError(permission);
		}
	};

	const requireGrantedPermission = (permission: YuiPermission) => {
		if (!isPermissionDeclared(app, permission) || !isPermissionGranted(app.id, permission)) {
			throw permissionError(permission);
		}
	};

	return {
		app: {
			id: app.id,
			name: app.name,
			version: app.version,
		},
		env: {
			runtimeVersion: "0.1.0",
			platform: "web",
			mode: "dev",
			theme: "system",
		},
		ui: createUiApi(),
		storage: {
			async get(key) {
				await ensurePermission("storage");
				const value = localStorage.getItem(storagePrefix + key);
				return value === null ? null : JSON.parse(value);
			},
			async set(key, value) {
				await ensurePermission("storage");
				localStorage.setItem(storagePrefix + key, JSON.stringify(value));
			},
			async delete(key) {
				await ensurePermission("storage");
				localStorage.removeItem(storagePrefix + key);
			},
			async keys() {
				await ensurePermission("storage");
				return Object.keys(localStorage)
					.filter((key) => key.startsWith(storagePrefix))
					.map((key) => key.slice(storagePrefix.length));
			},
			async clear() {
				await ensurePermission("storage");
				for (const key of Object.keys(localStorage)) {
					if (key.startsWith(storagePrefix)) {
						localStorage.removeItem(key);
					}
				}
			},
		},
		commands: {
			register(command) {
				let active = true;
				void requestAppPermission(app, "commands").then((granted) => {
					if (granted && active) {
						console.info(`[${app.id}] registered command ${app.id}:${command.id}`);
					}
				});
				const dispose = () => console.info(`[${app.id}] unregistered command ${app.id}:${command.id}`);
				disposables.add(dispose);
				return () => {
					active = false;
					disposables.delete(dispose);
					dispose();
				};
			},
		},
		events: {
			on(event, handler) {
				const handlers = eventHandlers.get(event) ?? new Set();
				handlers.add(handler);
				eventHandlers.set(event, handlers);
				const dispose = () => handlers.delete(handler);
				disposables.add(dispose);
				return dispose;
			},
			async emit(event, data) {
				const handlers = eventHandlers.get(event);
				if (!handlers) return;
				for (const handler of handlers) {
					await handler(data);
				}
			},
		},
		clipboard: {
			async readText() {
				await ensurePermission("clipboard.read");
				return navigator.clipboard.readText();
			},
			async writeText(text) {
				await ensurePermission("clipboard.write");
				await navigator.clipboard.writeText(text);
			},
		},
		notifications: {
			async send(notification) {
				await ensurePermission("notifications");
				new Notification(notification.title, { body: notification.body, icon: notification.icon });
			},
		},
		network: {
			async fetch(url, options) {
				await ensurePermission("network.fetch");
				return fetchThroughBridge(url, options);
			},
		},
		fullscreen: {
			async enter() {
				await ensurePermission("fullscreen");
				setShellFullscreen(app, true);
			},
			async exit() {
				await ensurePermission("fullscreen");
				setShellFullscreen(app, false);
			},
			async toggle() {
				await ensurePermission("fullscreen");
				setShellFullscreen(app, !isShellFullscreen());
			},
			isActive() {
				requireGrantedPermission("fullscreen");
				return isShellFullscreen();
			},
		},
		log: console,
		state: (initial) => createState(initial, scheduleRender),
		async toast(message) {
			console.info(`[${app.id}] ${message}`);
		},
		dispose() {
			for (const dispose of disposables) {
				dispose();
			}
			disposables.clear();
			eventHandlers.clear();
		},
	};
}
