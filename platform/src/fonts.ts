import avenirNext from "./assets/fonts/AvenirNext.woff2";
import { platformAssetUrl } from "./assets";

/**
 * Registers the overlay's font on the host document. Chrome ignores
 * @font-face inside a shadow root, but shadow trees do use the document's
 * fonts, so the overlay's styles just name "YuiAvenirNext".
 */
export function loadPlatformFonts(): void {
	if (typeof FontFace !== "function" || !document.fonts) return;
	for (const face of document.fonts) if (face.family.replace(/"/g, "") === "YuiAvenirNext") return;
	const face = new FontFace("YuiAvenirNext", `url("${platformAssetUrl(avenirNext)}") format("woff2")`, {
		weight: "400 850",
		style: "normal",
		display: "swap",
	});
	document.fonts.add(face);
	face.load().catch((error) => console.warn("[yui] font failed to load", error));
}
