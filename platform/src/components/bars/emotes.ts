/** Kenney emote balloons (CC0, see assets/kenney/emotes/CREDITS.md), keyed by emote id. */
import type { EmoteId } from "./social";

const files = import.meta.glob("../../assets/kenney/emotes/*.svg", { eager: true, query: "?url", import: "default" }) as Record<string, string>;

const urls: Record<string, string> = {};
for (const [path, url] of Object.entries(files)) {
	const id = path.split("/").pop()?.replace(/\.svg$/, "");
	if (id) urls[id] = url;
}

export function emoteUrl(id: EmoteId): string {
	return urls[id] ?? "";
}
