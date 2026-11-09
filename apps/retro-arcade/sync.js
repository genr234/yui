// Embeds web/systems.js into app.yui.js between the generated markers (an
// app is one script, so it cannot import; the page imports the same file).
// Run: bun run sync
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dir = dirname(fileURLToPath(import.meta.url));
const START = "// <generated:systems>";
const END = "// </generated:systems>";

export function embeddedSource(moduleSource) {
	return moduleSource.replace(/\nexport \{[\s\S]*?\};\n?/, "\n").trim();
}

export function withEmbedded(appSource, moduleSource) {
	const start = appSource.indexOf(START);
	const end = appSource.indexOf(END);
	if (start < 0 || end < start) throw new Error("generated markers not found in app.yui.js");
	return `${appSource.slice(0, start + START.length)}\n${embeddedSource(moduleSource)}\n${appSource.slice(end)}`;
}

if (import.meta.main) {
	const appPath = join(dir, "app.yui.js");
	writeFileSync(appPath, withEmbedded(readFileSync(appPath, "utf8"), readFileSync(join(dir, "web/systems.js"), "utf8")));
	console.log("app.yui.js updated");
}
