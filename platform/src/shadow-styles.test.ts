import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

/**
 * The overlay renders inside a shadow root (main.ts), and only the
 * styles/*.css files main.ts inlines reach it. A Svelte <style> block is
 * injected outside the shadow root, so on the kiosk its component renders
 * unstyled (this is how the join QR codes blew up to full width and pushed
 * "End phone play" off the screen).
 */
const src = join(import.meta.dir);

function svelteFiles(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const path = join(dir, name);
		if (statSync(path).isDirectory()) return svelteFiles(path);
		return name.endsWith(".svelte") ? [path] : [];
	});
}

describe("shadow root styles", () => {
	test("no component has its own <style> block", () => {
		const offenders = svelteFiles(src).filter((path) => {
			const text = readFileSync(path, "utf8");
			// PluginPage writes a plugin's CSS into a <style> element at runtime: that one is markup, not a Svelte style block.
			return /^<style[\s>]/m.test(text);
		});
		expect(offenders.map((path) => relative(src, path))).toEqual([]);
	});

	test("every styles/*.css file is inlined into the shadow root", () => {
		const main = readFileSync(join(src, "main.ts"), "utf8");
		const files = readdirSync(join(src, "styles")).filter((name) => name.endsWith(".css"));
		for (const name of files) expect(main).toContain(`./styles/${name}?inline`);
	});
});
