import { getAppStorageValue, setAppStorageValue } from "./app-storage";

export type EmbedStorageEntry = {
	appId: string;
	origin: string;
	lastUsedAt: number;
};

const EMBED_STORAGE_KEY = "__yui_embed_storage";

function normalizeEntries(appId: string, value: unknown): EmbedStorageEntry[] {
	if (!Array.isArray(value)) return [];
	return value.filter(
		(entry): entry is EmbedStorageEntry =>
			Boolean(
				entry &&
					typeof entry === "object" &&
					(entry as EmbedStorageEntry).appId === appId &&
					typeof (entry as EmbedStorageEntry).origin === "string" &&
					typeof (entry as EmbedStorageEntry).lastUsedAt === "number",
			),
	);
}

async function readEntries(appId: string): Promise<EmbedStorageEntry[]> {
	return normalizeEntries(appId, await getAppStorageValue(appId, EMBED_STORAGE_KEY));
}

async function writeEntries(appId: string, entries: EmbedStorageEntry[]) {
	await setAppStorageValue(appId, EMBED_STORAGE_KEY, entries);
	window.dispatchEvent(new CustomEvent("yui:embed-storage-changed", { detail: { appId } }));
}

export async function getEmbedStorageEntries(appId: string) {
	const entries = await readEntries(appId);
	return entries.sort((a, b) => b.lastUsedAt - a.lastUsedAt);
}

export async function rememberEmbedStorage(appId: string, origin: string) {
	if (!origin) return;
	const entries = await readEntries(appId);
	const existing = entries.find((entry) => entry.origin === origin);
	if (existing) {
		existing.lastUsedAt = Date.now();
		await writeEntries(appId, entries);
		return;
	}
	await writeEntries(appId, [
		...entries,
		{
			appId,
			origin,
			lastUsedAt: Date.now(),
		},
	]);
}

export async function clearEmbedStorage(appId: string, origin?: string) {
	const entries = await readEntries(appId);
	const next = origin ? entries.filter((entry) => entry.origin !== origin) : [];
	await writeEntries(appId, next);
	window.dispatchEvent(
		new CustomEvent("yui:embed-storage-cleared", {
			detail: { appId, origin },
		}),
	);
}
