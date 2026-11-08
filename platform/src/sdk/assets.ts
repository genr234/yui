import { bridge } from "./bridge";

/** `assets.status` (contracts §1). */
export type AssetSyncStatus = {
	state: "idle" | "syncing" | "paused" | "error";
	paused_reasons: string[];
	current: { id: string; bytes_done: number; byte_size: number } | null;
	last_reconcile_at?: string;
	error?: string;
};

export const assetSync = {
	status: () =>
		bridge
			.send<AssetSyncStatus | null>("assets.status")
			.then((status) => status ?? null)
			.catch(() => null),
	/** `assets.changed` carries the same shape as `assets.status`. */
	onChange: (fn: (status: AssetSyncStatus) => void) => bridge.on<AssetSyncStatus>("assets.changed", fn),
};
