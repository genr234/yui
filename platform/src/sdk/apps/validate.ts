import { SIMPLE_APP_SCHEMA, type YuiPermission, type YuiSimpleApp } from "./types";

const idPattern = /^[a-z0-9][a-z0-9._-]{1,}[a-z0-9_-]$/;
const allowedPermissions = new Set<YuiPermission>([
	"storage",
	"commands",
	"clipboard.read",
	"clipboard.write",
	"notifications",
	"network.fetch",
	"fullscreen",
]);

function isValidPermission(permission: unknown) {
	if (typeof permission !== "string") return false;
	if (allowedPermissions.has(permission)) return true;
	if (!permission.startsWith("embed:")) return false;

	try {
		const url = new URL(permission.slice("embed:".length));
		return url.protocol === "https:" || url.protocol === "http:";
	} catch {
		return false;
	}
}

export function validateSimpleApp(value: unknown): asserts value is YuiSimpleApp {
	if (!value || typeof value !== "object") {
		throw new Error("YUI_INVALID_APP: default export must be an object");
	}

	const app = value as Partial<YuiSimpleApp>;
	if (app.schema !== SIMPLE_APP_SCHEMA) {
		throw new Error(`YUI_UNSUPPORTED_SCHEMA: expected ${SIMPLE_APP_SCHEMA}`);
	}
	if (!app.id || typeof app.id !== "string" || !idPattern.test(app.id) || app.id.length < 3 || app.id.includes("..")) {
		throw new Error("YUI_INVALID_APP: invalid app id");
	}
	if (!app.name || typeof app.name !== "string") {
		throw new Error("YUI_INVALID_APP: app name is required");
	}
	if (!app.version || typeof app.version !== "string") {
		throw new Error("YUI_INVALID_APP: app version is required");
	}
	if (typeof app.mount !== "function") {
		throw new Error("YUI_INVALID_APP: mount(ctx) is required");
	}
	if (app.permissions && (!Array.isArray(app.permissions) || app.permissions.some((permission) => !isValidPermission(permission)))) {
		throw new Error("YUI_INVALID_APP: invalid permission declaration");
	}
}
