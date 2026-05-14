import type { YuiPermission, YuiSimpleApp } from "./types";

const storageKey = "yui.simple-app.permissions.v0";
const scopedStorageKeyPrefix = "yui.simple-app.permissions.v1";
let accountScope = "anonymous";

export type PermissionState = Record<string, Record<string, boolean>>;

export type PermissionRequest = {
	app: {
		id: string;
		name: string;
	};
	permission: YuiPermission;
	permissions?: YuiPermission[];
	resolve: (granted: boolean) => void;
	resolveAll?: (granted: boolean) => void;
};

const permissionCopy: Record<string, { label: string; description: string }> = {
	storage: {
		label: "Storage",
		description: "Save local data.",
	},
	commands: {
		label: "Commands",
		description: "Add app actions.",
	},
	"clipboard.read": {
		label: "Read clipboard",
		description: "Read copied text.",
	},
	"clipboard.write": {
		label: "Write clipboard",
		description: "Copy text.",
	},
	notifications: {
		label: "Notifications",
		description: "Send alerts.",
	},
	"network.fetch": {
		label: "Network",
		description: "Fetch remote data.",
	},
	fullscreen: {
		label: "Fullscreen",
		description: "Fill the Yui surface.",
	},
};

function readState(): PermissionState {
	try {
		migrateAnonymousPermissions();
		return JSON.parse(localStorage.getItem(scopedStorageKey()) ?? "{}") as PermissionState;
	} catch {
		return {};
	}
}

function writeState(state: PermissionState) {
	localStorage.setItem(scopedStorageKey(), JSON.stringify(state));
	window.dispatchEvent(new CustomEvent("yui:permissions-changed"));
}

function scopedStorageKey() {
	return `${scopedStorageKeyPrefix}.${accountScope}`;
}

export function setPermissionAccountScope(accountId?: string | null) {
	const next = accountId ? `account.${accountId}` : "anonymous";
	if (next === accountScope) return;
	accountScope = next;
	migrateAnonymousPermissions();
	window.dispatchEvent(new CustomEvent("yui:permissions-changed"));
}

function migrateAnonymousPermissions() {
	if (accountScope !== "anonymous") return;
	if (localStorage.getItem(scopedStorageKey()) !== null) return;
	const previous = localStorage.getItem(storageKey);
	if (previous !== null) localStorage.setItem(scopedStorageKey(), previous);
}

export function describePermission(permission: YuiPermission) {
	if (permission.startsWith("embed:")) {
		const origin = permission.slice("embed:".length);
		return {
			label: "Embed website",
			description: origin,
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
		const pending = declaredPermissions(app).filter(
			(item) => !hasPermissionDecision(app.id, item),
		);
		window.dispatchEvent(
			new CustomEvent<PermissionRequest>("yui:permission-request", {
				detail: {
					app: { id: app.id, name: app.name },
					permission,
					permissions: pending.length ? pending : [permission],
					resolve(granted) {
						setAppPermission(app.id, permission, granted);
						resolve(granted);
					},
					resolveAll(granted) {
						for (const item of pending.length ? pending : [permission]) {
							setAppPermission(app.id, item, granted);
						}
						resolve(granted);
					},
				},
			}),
		);
	});
}
