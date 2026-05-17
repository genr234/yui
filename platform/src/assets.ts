export function platformAssetUrl(path: string) {
	if (/^(?:[a-z]+:)?\/\//i.test(path) || path.startsWith("data:") || path.startsWith("blob:")) {
		return path;
	}

	const base = (
		window.__YUI_PLATFORM_DEV_SERVER ||
		window.__YUI_PLATFORM_HTTP ||
		""
	).replace(/\/+$/g, "");
	if (!base) return path;

	return `${base}/${path.replace(/^\/+/g, "")}`;
}
