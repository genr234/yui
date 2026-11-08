/**
 * What the shell asked an app to open, read by `ctx.launch.item`.
 *
 * The Apps page sets the item right before it mounts the app (for example
 * when a library title is tapped in Jump back in) and clears it for a plain
 * launch. A reload of the same app keeps it, so the app reopens the same
 * title.
 */
export type YuiLaunchItem = {
	/** The library title id the app published. */
	id: string;
	title: string;
	/** The resume value the app published or last passed to `played`. */
	resume: unknown;
};

const items = new Map<string, YuiLaunchItem>();

export function setLaunchItem(appId: string, item: YuiLaunchItem | null) {
	if (item) items.set(appId, item);
	else items.delete(appId);
}

export function getLaunchItem(appId: string): YuiLaunchItem | null {
	return items.get(appId) ?? null;
}
