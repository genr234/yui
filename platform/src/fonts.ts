// Bundled as a data URL (see assetsInlineLimit in vite.config.ts), so the font
// needs no network request: the overlay runs inside other people's pages, and
// their Content-Security-Policy could block a font-src load from the controller.
import avenirNext from "./assets/fonts/AvenirNext.woff2";
import { dataUrlBlob } from "../../avatar/src";
import { platformAssetUrl } from "./assets";

/**
 * Registers the overlay's font on the host document. Chrome ignores
 * @font-face inside a shadow root, but shadow trees do use the document's
 * fonts, so the overlay's styles just name "YuiAvenirNext". A FontFace made
 * from bytes is not subject to the page's font-src.
 */
export async function loadPlatformFonts(): Promise<void> {
	if (typeof FontFace !== "function" || !document.fonts) return;
	for (const face of document.fonts) if (face.family.replace(/"/g, "") === "YuiAvenirNext") return;
	// The dev server serves the font by URL instead of inlining it.
	const bytes = await dataUrlBlob(avenirNext)?.arrayBuffer();
	const source = bytes ?? `url("${platformAssetUrl(avenirNext)}") format("woff2")`;
	const face = new FontFace("YuiAvenirNext", source, { weight: "400 850", style: "normal", display: "swap" });
	document.fonts.add(face);
	await face.load().catch((error) => console.warn("[yui] font failed to load", error));
}
