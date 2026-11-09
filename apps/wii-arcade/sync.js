// Embeds dolphin-config.js into app.yui.js between the generated markers
// (an app is one script, so it cannot import). Run: bun run sync
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dir = dirname(fileURLToPath(import.meta.url));
const START = "// <generated:dolphin-config>";
const END = "// </generated:dolphin-config>";

/** The module as plain script text: the export list removed. */
export function embeddedSource(moduleSource) {
	return moduleSource.replace(/\nexport \{[\s\S]*?\};\n?/, "\n").trim();
}

/** app.yui.js with the generated block replaced by `moduleSource`. */
export function withEmbedded(appSource, moduleSource) {
	const start = appSource.indexOf(START);
	const end = appSource.indexOf(END);
	if (start < 0 || end < start) throw new Error("generated markers not found in app.yui.js");
	return `${appSource.slice(0, start + START.length)}\n${embeddedSource(moduleSource)}\n${appSource.slice(end)}`;
}

if (import.meta.main) {
	const appPath = join(dir, "app.yui.js");
	const next = withEmbedded(readFileSync(appPath, "utf8"), readFileSync(join(dir, "dolphin-config.js"), "utf8"));
	writeFileSync(appPath, next);
	console.log("app.yui.js updated");
}
