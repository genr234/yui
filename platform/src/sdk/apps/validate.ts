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
	"native",
	"library",
	"virtual-pads",
	"controllers",
	"audio.speaker",
	"assets:game",
	"assets:runtime",
	"assets:media",
	"assets:other",
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
	if (app.controllers !== undefined) {
		validateControllersManifest(app.controllers);
		if (!app.permissions?.includes("controllers")) {
			throw new Error("YUI_INVALID_APP: controllers needs the controllers permission");
		}
	}
}

/** Key names a keymap may use (the controller's CDP adapter accepts the same). */
const namedKeys = new Set([
	"Enter",
	"Backspace",
	"Tab",
	"Escape",
	"Space",
	"PageUp",
	"PageDown",
	"End",
	"Home",
	"ArrowLeft",
	"ArrowUp",
	"ArrowRight",
	"ArrowDown",
	"Delete",
]);
const directions = new Set(["up", "down", "left", "right"]);
const maxKeymapControls = 8;
const maxKeymapButtons = 10;
const maxLabelLength = 40;

export function isKeymapKey(key: unknown): key is string {
	return typeof key === "string" && (namedKeys.has(key) || /^[A-Za-z0-9]$/.test(key));
}

function controllersError(message: string): never {
	throw new Error(`YUI_INVALID_APP: controllers: ${message}`);
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
	return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function checkKeys(at: string, keys: unknown) {
	if (!isPlainObject(keys) || Object.keys(keys).length === 0) controllersError(`${at}: keys must map up/down/left/right to keys`);
	for (const [dir, key] of Object.entries(keys)) {
		if (!directions.has(dir)) controllersError(`${at}: unknown direction "${dir}"`);
		if (!isKeymapKey(key)) controllersError(`${at}: unknown key "${String(key)}"`);
	}
}

function checkLabel(at: string, label: unknown, required = false) {
	if (label === undefined && !required) return;
	if (typeof label !== "string" || !label || [...label].length > maxLabelLength) {
		controllersError(`${at}: label must be 1-${maxLabelLength} characters`);
	}
}

/**
 * Checks a manifest's `controllers` block. `{}` declares that the app uses
 * ctx.controllers; `keymap` maps phone controls onto an embedded web game's
 * keyboard and mouse. The controller validates keymaps again.
 */
export function validateControllersManifest(value: unknown) {
	if (!isPlainObject(value)) controllersError("must be an object");
	for (const key of Object.keys(value)) {
		if (key !== "keymap") controllersError(`unknown field "${key}"`);
	}
	const keymap = value.keymap;
	if (keymap === undefined) return;
	if (!isPlainObject(keymap)) controllersError("keymap must be an object");
	for (const key of Object.keys(keymap)) {
		if (key !== "players" && key !== "controls") controllersError(`keymap: unknown field "${key}"`);
	}
	if (keymap.players !== undefined && keymap.players !== "host" && keymap.players !== "all") {
		controllersError('keymap.players must be "host" or "all"');
	}
	const controls = keymap.controls;
	if (!Array.isArray(controls) || controls.length === 0 || controls.length > maxKeymapControls) {
		controllersError(`keymap.controls needs 1-${maxKeymapControls} controls`);
	}
	const once = new Set<string>();
	let buttons = 0;
	controls.forEach((control: unknown, index: number) => {
		const at = `keymap.controls[${index}]`;
		if (!isPlainObject(control)) controllersError(`${at} must be an object`);
		const single = (kind: string) => {
			if (once.has(kind)) controllersError(`${at}: only one ${kind}`);
			once.add(kind);
		};
		switch (control.type) {
			case "swipe":
				single("swipe");
				checkKeys(at, control.keys);
				if (control.tap !== undefined && !isKeymapKey(control.tap)) controllersError(`${at}: unknown key "${String(control.tap)}"`);
				checkLabel(at, control.label);
				break;
			case "dpad":
				single("dpad");
				checkKeys(at, control.keys);
				checkLabel(at, control.label);
				break;
			case "button":
				buttons += 1;
				if (buttons > maxKeymapButtons) controllersError(`at most ${maxKeymapButtons} buttons`);
				checkLabel(at, control.label, true);
				if (control.key !== undefined && control.mouse !== undefined) controllersError(`${at}: a button has key or mouse, not both`);
				if (control.key !== undefined) {
					if (!isKeymapKey(control.key)) controllersError(`${at}: unknown key "${String(control.key)}"`);
				} else if (control.mouse !== "left") {
					controllersError(`${at}: a button needs key or mouse "left"`);
				}
				break;
			case "trackpad":
				single("trackpad");
				checkLabel(at, control.label);
				if (control.mode === "absolute") {
					if (control.mouse !== "drag") controllersError(`${at}: an absolute trackpad needs mouse "drag"`);
				} else if (control.mode !== undefined && control.mode !== "relative") {
					controllersError(`${at}: mode must be "absolute" or "relative"`);
				} else if (control.mouse !== undefined && control.mouse !== "cursor") {
					controllersError(`${at}: a relative trackpad moves the cursor`);
				}
				break;
			default:
				controllersError(`${at}: unknown type "${String(control.type)}"`);
		}
	});
}
