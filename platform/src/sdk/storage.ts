import { bridge } from "./bridge";
import { storageCollections } from "./storage-spaces";

export const defaultStorageCollection = storageCollections.keyValue;

export const storage = {
	get: (key: string) => bridge.send<string | null>("storage.get", { key }),
	set: (key: string, value: string) =>
		bridge.send<void>("storage.set", { key, value }),
	delete: (key: string) => bridge.send<void>("storage.delete", { key }),
};
