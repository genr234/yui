import type { YuiChildren, YuiNode, YuiUiApi } from "./types";

const textElements = new Set(["h1", "h2", "h3", "p", "small", "code", "pre", "text"]);
const childElements = new Set(["div", "span", "row", "column", "grid", "card", "panel", "list", "item"]);

function isProps(value: unknown) {
	return Boolean(value) && typeof value === "object" && !Array.isArray(value) && !("type" in (value as object));
}

function makeNode(type: string, props?: Record<string, unknown>, children?: YuiChildren): YuiNode {
	const normalizedChildren = children === undefined ? [] : Array.isArray(children) ? children : [children];
	return { type, props: props ?? {}, children: normalizedChildren };
}

function element(type: string, ...args: unknown[]): YuiNode {
	if (textElements.has(type)) {
		return makeNode(type, {}, args.length > 1 ? (args as YuiChildren[]) : (args[0] as YuiChildren));
	}

	if (args.length === 0) {
		return makeNode(type);
	}

	if (isProps(args[0])) {
		return makeNode(type, args[0] as Record<string, unknown>, args[1] as YuiChildren);
	}

	return makeNode(type, {}, args[0] as YuiChildren);
}

function button(...args: unknown[]): YuiNode {
	if (typeof args[0] === "string") {
		return makeNode("button", {
			label: args[0],
			onClick: args[1],
		});
	}
	return makeNode("button", (args[0] as Record<string, unknown>) ?? {});
}

export function createUiApi(): YuiUiApi {
	const ui: YuiUiApi = {
		text: (...args) => element("text", ...args),
		button,
		input: (props) => makeNode("input", props ?? {}),
		textarea: (props) => makeNode("textarea", props ?? {}),
		checkbox: (props) => makeNode("checkbox", props ?? {}),
		select: (props) => makeNode("select", props ?? {}),
		slider: (props) => makeNode("slider", props ?? {}),
		spacer: (props) => makeNode("spacer", props ?? {}),
		divider: () => makeNode("divider"),
		icon: (name, props) => makeNode("icon", { ...(props ?? {}), name }),
		image: (props) => makeNode("image", props ?? {}),
		embed: (props) => makeNode("embed", props ?? {}),
		empty: (message = "nothing here yet") => makeNode("empty", { message }),
		when: (condition, node) => (condition ? (node as YuiNode) : null),
		for: (items, render) => (Array.isArray(items) ? items.map(render) : []),
		css: (styles) => makeNode("css", { styles }),
	};

	for (const type of [...textElements, ...childElements]) {
		ui[type] ??= (...args) => element(type, ...args);
	}

	return ui;
}
