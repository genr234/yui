import type { YuiPermission, YuiSimpleApp } from "./types";

const storageKey = "yui.simple-app.permissions.v0";

export type PermissionState = Record<string, Record<string, boolean>>;

export type PermissionRequest = {
	app: {
		id: string;
		name: string;
	};
	permission: YuiPermission;
	resolve: (granted: boolean) => void;
};

const permissionCopy: Record<string, { label: string; description: string }> = {
	storage: {
		label: "Storage",
		description: "Save and read this app's local data.",
	},
	commands: {
		label: "Commands",
		description: "Add actions to yui command surfaces.",
	},
	"clipboard.read": {
		label: "Read clipboard",
		description: "Read text from the clipboard.",
	},
	"clipboard.write": {
		label: "Write clipboard",
		description: "Write text to the clipboard.",
	},
	notifications: {
		label: "Notifications",
		description: "Send system notifications.",
	},
	"network.fetch": {
		label: "Network",
		description: "Make remote network requests.",
	},
	fullscreen: {
		label: "Fullscreen",
		description: "Expand this app to fill the yui surface.",
	},
};

function readState(): PermissionState {
	try {
		return JSON.parse(localStorage.getItem(storageKey) ?? "{}") as PermissionState;
	} catch {
		return {};
	}
}

function writeState(state: PermissionState) {
	localStorage.setItem(storageKey, JSON.stringify(state));
	window.dispatchEvent(new CustomEvent("yui:permissions-changed"));
}

export function describePermission(permission: YuiPermission) {
	if (permission.startsWith("embed:")) {
		const origin = permission.slice("embed:".length);
		return {
			label: `Embed ${origin}`,
			description: `Allow this app to show ${origin} in an embedded website view.`,
		};
	}

	return (
		permissionCopy[permission] ?? {
			label: permission,
			description: "Allow this app to use this yui capability.",
		}
	);
}

export function declaredPermissions(app: YuiSimpleApp) {
	return app.permissions ?? [];
}

export function isPermissionDeclared(app: YuiSimpleApp, permission: YuiPermission) {
	return declaredPermissions(app).includes(permission);
}

export function isPermissionGranted(appId: string, permission: YuiPermission) {
	return readState()[appId]?.[permission] === true;
}

export function hasPermissionDecision(appId: string, permission: YuiPermission) {
	return Object.prototype.hasOwnProperty.call(readState()[appId] ?? {}, permission);
}

export function setAppPermission(appId: string, permission: YuiPermission, granted: boolean) {
	const state = readState();
	state[appId] ??= {};
	state[appId][permission] = granted;
	writeState(state);
}

export function getAppPermissionState(appId: string) {
	return readState()[appId] ?? {};
}

export function requestAppPermission(app: YuiSimpleApp, permission: YuiPermission): Promise<boolean> {
	if (!isPermissionDeclared(app, permission)) {
		return Promise.resolve(false);
	}
	if (isPermissionGranted(app.id, permission)) {
		return Promise.resolve(true);
	}
	if (hasPermissionDecision(app.id, permission)) {
		return Promise.resolve(false);
	}

	return new Promise((resolve) => {
		window.dispatchEvent(
			new CustomEvent<PermissionRequest>("yui:permission-request", {
				detail: {
					app: { id: app.id, name: app.name },
					permission,
					resolve(granted) {
						setAppPermission(app.id, permission, granted);
						resolve(granted);
					},
				},
			}),
		);
	});
}
