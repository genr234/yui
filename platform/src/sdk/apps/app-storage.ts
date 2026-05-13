import type { StoreDocument } from "../store";
import { storageCollections, storageSpace } from "../storage-spaces";

const appStorage = storageSpace<unknown>(storageCollections.apps, { encodeKeys: true });

export type AppStorageEntry<T = unknown> = StoreDocument<T>;

export async function getAppStorageValue<T = unknown>(appId: string, key: string) {
	return appStorage.scope(appId).get(key) as Promise<T | null>;
}

export async function setAppStorageValue(appId: string, key: string, value: unknown) {
	await appStorage.scope(appId).set(key, value);
	window.dispatchEvent(new CustomEvent("yui:app-storage-changed", { detail: { appId } }));
}

export async function deleteAppStorageValue(appId: string, key: string) {
	await appStorage.scope(appId).delete(key);
	window.dispatchEvent(new CustomEvent("yui:app-storage-changed", { detail: { appId } }));
}

export async function listAppStorageKeys(appId: string) {
	return appStorage.scope(appId).keys();
}

export async function clearAppStorage(appId: string) {
	await appStorage.scope(appId).clear();
	window.dispatchEvent(new CustomEvent("yui:app-storage-changed", { detail: { appId } }));
}
