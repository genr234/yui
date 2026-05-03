import { store, type StoreDocument } from "../store";

const APP_STORAGE_COLLECTION = "app-storage";
const APP_STORAGE_SEPARATOR = ":";

const appStorage = store.collection<unknown>(APP_STORAGE_COLLECTION);

function appPrefix(appId: string) {
	return `${appId}${APP_STORAGE_SEPARATOR}`;
}

function entryId(appId: string, key: string) {
	return `${appPrefix(appId)}${encodeURIComponent(key)}`;
}

function keyFromEntryId(appId: string, id: string) {
	const prefix = appPrefix(appId);
	if (!id.startsWith(prefix)) return null;

	try {
		return decodeURIComponent(id.slice(prefix.length));
	} catch {
		return null;
	}
}

export type AppStorageEntry<T = unknown> = StoreDocument<T>;

export async function getAppStorageValue<T = unknown>(appId: string, key: string) {
	return appStorage.get(entryId(appId, key)) as Promise<T | null>;
}

export async function setAppStorageValue(appId: string, key: string, value: unknown) {
	await appStorage.put(entryId(appId, key), value);
	window.dispatchEvent(new CustomEvent("yui:app-storage-changed", { detail: { appId } }));
}

export async function deleteAppStorageValue(appId: string, key: string) {
	await appStorage.delete(entryId(appId, key));
	window.dispatchEvent(new CustomEvent("yui:app-storage-changed", { detail: { appId } }));
}

export async function listAppStorageKeys(appId: string) {
	const docs = await appStorage.list({ prefix: appPrefix(appId) });
	return docs
		.map((doc) => keyFromEntryId(appId, doc.id))
		.filter((key): key is string => key !== null);
}

export async function clearAppStorage(appId: string) {
	const docs = await appStorage.list({ prefix: appPrefix(appId) });
	await Promise.all(docs.map((doc) => appStorage.delete(doc.id)));
	window.dispatchEvent(new CustomEvent("yui:app-storage-changed", { detail: { appId } }));
}
