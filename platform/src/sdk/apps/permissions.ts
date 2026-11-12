import type { YuiPermission, YuiSimpleApp } from "./types";

const storageKey = "yui.simple-app.permissions.v0";
const scopedStorageKeyPrefix = "yui.simple-app.permissions.v1";
const permissionsChangedEvent = "yui:permissions-changed";
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
	native: {
		label: "Run programs",
		description:
			"Start programs from the kiosk's runtime files and hand them the screen. Only an admin can allow this.",
	},
	library: {
		label: "Library",
		description: "Add titles to Jump back in and remember where you left off.",
	},
	"virtual-pads": {
		label: "Virtual controllers",
		description: "Let programs this app starts read the players' phones as game controllers.",
	},
	surfaces: {
		label: "Private screens",
		description: "Show a private screen on one player's phone.",
	},
	controllers: {
		label: "Phone controllers",
		description: "See who joined from their phone, show them controls, and receive their input.",
	},
	profiles: {
		label: "Player profiles",
		description: "Save each player's progress to their Yui account, if they allow it on their phone.",
	},
	saves: {
		label: "Save files",
		description: "Keep save files for this app, and for players who allow it, on their Yui account.",
	},
	"audio.speaker": {
		label: "Phone speaker",
		description: "Play this app's sound, or a program it started, on a player's phone.",
	},
};

const assetKindCopy: Record<string, string> = {
	game: "games",
	runtime: "runtimes",
	media: "media",
	other: "files",
};

/**
 * Permissions an app can only get from an admin: granted in Settings or when
 * installing from a store, never from the prompt an app raises while it runs.
 */
const adminOnlyPermissions = new Set<YuiPermission>(["native"]);

export function isAdminOnlyPermission(permission: YuiPermission) {
	return adminOnlyPermissions.has(permission);
}

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
	notifyPermissionsChanged();
}

function scopedStorageKey() {
	return `${scopedStorageKeyPrefix}.${accountScope}`;
}

export function setPermissionAccountScope(accountId?: string | null) {
	const next = accountId ? `account.${accountId}` : "anonymous";
	if (next === accountScope) return;
	accountScope = next;
	migrateAnonymousPermissions();
	notifyPermissionsChanged();
}

function notifyPermissionsChanged() {
	window.dispatchEvent(new CustomEvent(permissionsChangedEvent));
}

if (typeof window !== "undefined") {
	window.addEventListener("storage", (event) => {
		if (
			event.key === storageKey ||
			event.key?.startsWith(`${scopedStorageKeyPrefix}.`)
		) {
			notifyPermissionsChanged();
		}
	});
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

	if (permission.startsWith("assets:")) {
		const kind = permission.slice("assets:".length);
		const noun = assetKindCopy[kind] ?? `${kind} files`;
		return {
			label: `Asset library: ${noun}`,
			description: `See the ${noun} synced to this kiosk. Never their location on disk.`,
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
	if (isAdminOnlyPermission(permission)) {
		// Not recorded as a decision: an admin can still grant it in Settings.
		return Promise.resolve(false);
	}

	return new Promise((resolve) => {
		const pending = declaredPermissions(app).filter(
			(item) => !hasPermissionDecision(app.id, item) && !isAdminOnlyPermission(item),
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
