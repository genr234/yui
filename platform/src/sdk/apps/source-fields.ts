/** Reads manifest fields out of an app's source text without running it. */

// A field key, not the tail of another key or a property access: `title_id: ""`,
// `meta.id : ""` and `padId: "x"` must not read as the app's `id`.
const KEY_START = "(?<![\\w.$])";

export function stringField(source: string, field: string) {
	const match = source.match(new RegExp(`${KEY_START}${field}\\s*:\\s*(["'])([\\s\\S]*?)\\1`));
	return match?.[2];
}

export function stringArrayField(source: string, field: string) {
	const match = source.match(new RegExp(`${KEY_START}${field}\\s*:\\s*\\[([\\s\\S]*?)\\]`));
	if (!match) return undefined;
	return [...match[1].matchAll(/(["'])([\s\S]*?)\1/g)].map((item) => item[2]);
}
