/**
 * App web bundles (plan 11, contracts §6).
 *
 * Build: copies every dev app's `web/` folder to
 * `<outDir>/app-web/<app id>/`, which the controller embeds and serves on
 * the app's loopback origin.
 *
 * Demo (`vite --mode demo`): serves the same files at
 * `/app-web/<app id>/…`, plus `/_yui/embed.js`, runtime folders from
 * `platform/.demo-runtimes/<name>/` and demo assets from
 * `platform/.demo-assets/` (both git-ignored; drop files there to try an app
 * without a kiosk). The demo bridge points `webapps.origin` here.
 */
import { createReadStream, existsSync, readdirSync, readFileSync, rmSync, statSync, cpSync, mkdirSync, copyFileSync } from "node:fs";
import { dirname, extname, join, resolve, sep } from "node:path";
import type { Plugin } from "vite";

type WebApp = { id: string; dir: string };

const mime: Record<string, string> = {
	".html": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8",
	".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8",
	".json": "application/json",
	".wasm": "application/wasm",
	".data": "application/octet-stream",
	".png": "image/png",
	".jpg": "image/jpeg",
	".jpeg": "image/jpeg",
	".gif": "image/gif",
	".webp": "image/webp",
	".svg": "image/svg+xml",
	".woff2": "font/woff2",
	".woff": "font/woff",
	".ttf": "font/ttf",
	".mp3": "audio/mpeg",
	".ogg": "audio/ogg",
	".wav": "audio/wav",
	".zip": "application/zip",
	".7z": "application/x-7z-compressed",
};

/** Demo asset kinds and platforms by extension (the real analyzers run on the server). */
const demoPlatforms: Record<string, string> = {
	".nes": "nes",
	".sfc": "snes",
	".smc": "snes",
	".gb": "gb",
	".gbc": "gbc",
	".gba": "gba",
	".md": "genesis",
	".gen": "genesis",
	".bin": "genesis",
	".sms": "sms",
	".gg": "gg",
	".pce": "pce",
};

export function discoverWebApps(appsRoot: string): WebApp[] {
	if (!existsSync(appsRoot)) return [];
	const apps: WebApp[] = [];
	for (const folder of readdirSync(appsRoot)) {
		const dir = join(appsRoot, folder);
		if (!statSync(dir).isDirectory()) continue;
		const web = join(dir, "web");
		if (!existsSync(web) || !statSync(web).isDirectory()) continue;
		const entry = readdirSync(dir).find((name) => name.endsWith(".yui.js"));
		if (!entry) continue;
		const source = readFileSync(join(dir, entry), "utf8");
		const id = /\bid\s*:\s*(["'])([a-z0-9][a-z0-9._-]+)\1/.exec(source)?.[2];
		if (id) apps.push({ id, dir: web });
	}
	return apps;
}

/** Resolves `rel` under `root`, or null when it escapes. */
function inside(root: string, rel: string) {
	let decoded: string;
	try {
		decoded = decodeURIComponent(rel);
	} catch {
		return null;
	}
	if (decoded.includes("\0") || decoded.includes("\\")) return null;
	const target = resolve(root, `.${sep}${decoded}`);
	if (target !== root && !target.startsWith(root + sep)) return null;
	return target;
}

function demoAssets(root: string) {
	if (!existsSync(root)) return [];
	return readdirSync(root)
		.filter((name) => !name.startsWith(".") && statSync(join(root, name)).isFile())
		.map((name) => {
			const ext = extname(name).toLowerCase();
			const platform = demoPlatforms[ext] ?? null;
			const title = name.slice(0, name.length - ext.length).replace(/[_-]+/g, " ").trim();
			return {
				id: `demo-${name}`,
				kind: platform ? "game" : ext === ".zip" ? "runtime" : "media",
				title,
				byte_size: statSync(join(root, name)).size,
				state: "ready",
				metadata: platform ? { platform, format: ext.slice(1), title } : {},
				file: name,
			};
		});
}

/** Files served at `_yui/<name>` on every app origin besides embed.js (see controller/internal/webapps). */
const yuiFiles = ["avatar.js", "avatar-atlas.png"];

export function yuiAppWeb(options: { appsRoot: string; embedJs: string; demoRoot: string }): Plugin {
	let outDir = "";
	return {
		name: "yui-app-web",
		configResolved(config) {
			outDir = resolve(config.root, config.build.outDir);
		},
		buildStart() {
			// Removed apps must not linger in the controller's embedded files.
			if (outDir) rmSync(join(outDir, "app-web"), { recursive: true, force: true });
		},
		writeBundle() {
			const target = join(outDir, "app-web");
			for (const app of discoverWebApps(options.appsRoot)) {
				cpSync(app.dir, join(target, app.id), { recursive: true, dereference: false, filter: (src) => !src.split(sep).pop()?.startsWith(".") });
			}
			mkdirSync(target, { recursive: true });
			copyFileSync(options.embedJs, join(target, "embed.js"));
			// The avatar renderer for app pages sits next to the embed helper.
			for (const name of yuiFiles) copyFileSync(join(dirname(options.embedJs), name), join(target, name));
		},
		configureServer(server) {
			const runtimes = join(options.demoRoot, ".demo-runtimes");
			const assets = join(options.demoRoot, ".demo-assets");
			server.middlewares.use((req, res, next) => {
				const url = (req.url ?? "").split("?")[0];
				if (url === "/app-web/_demo-assets.json") {
					res.setHeader("Content-Type", "application/json");
					res.end(JSON.stringify(demoAssets(assets)));
					return;
				}
				if (url === "/app-web/_demo-runtimes.json") {
					res.setHeader("Content-Type", "application/json");
					res.end(JSON.stringify(existsSync(runtimes) ? readdirSync(runtimes).filter((n) => !n.startsWith(".")) : []));
					return;
				}
				const match = /^\/app-web\/([a-z0-9][a-z0-9._-]+)\/(.*)$/.exec(url);
				if (!match) return next();
				const [, appId, rest] = match;
				let file: string | null = null;
				if (rest === "_yui/embed.js") file = options.embedJs;
				else if (rest.startsWith("_yui/") && yuiFiles.includes(rest.slice("_yui/".length))) file = join(dirname(options.embedJs), rest.slice("_yui/".length));
				else if (rest.startsWith("_runtime/")) {
					const [, name, ...tail] = rest.split("/");
					file = name ? inside(resolve(runtimes, name), tail.join("/") || "index.html") : null;
				} else if (rest.startsWith("_asset/")) {
					const name = decodeURIComponent(rest.slice("_asset/".length)).replace(/^demo-/, "");
					file = inside(resolve(assets), name);
				} else {
					const app = discoverWebApps(options.appsRoot).find((candidate) => candidate.id === appId);
					file = app ? inside(resolve(app.dir), rest || "index.html") : null;
				}
				if (file && existsSync(file) && statSync(file).isDirectory()) file = join(file, "index.html");
				if (!file || !existsSync(file)) {
					res.statusCode = 404;
					res.end("not found");
					return;
				}
				const size = statSync(file).size;
				res.setHeader("Content-Type", mime[extname(file).toLowerCase()] ?? "application/octet-stream");
				res.setHeader("Accept-Ranges", "bytes");
				res.setHeader("Cache-Control", "no-cache");
				const range = /^bytes=(\d*)-(\d*)$/.exec(String(req.headers.range ?? ""));
				if (range) {
					const start = range[1] ? Number(range[1]) : Math.max(0, size - Number(range[2]));
					const end = range[1] && range[2] ? Math.min(Number(range[2]), size - 1) : size - 1;
					if (start >= size || start > end) {
						res.statusCode = 416;
						res.setHeader("Content-Range", `bytes */${size}`);
						res.end();
						return;
					}
					res.statusCode = 206;
					res.setHeader("Content-Range", `bytes ${start}-${end}/${size}`);
					res.setHeader("Content-Length", String(end - start + 1));
					createReadStream(file, { start, end }).pipe(res);
					return;
				}
				res.setHeader("Content-Length", String(size));
				createReadStream(file).pipe(res);
			});
		},
	};
}
