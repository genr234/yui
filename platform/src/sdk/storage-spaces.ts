import { store, type StoreDocument } from "./store";

export const storageCollections = {
	keyValue: "storage",
	apps: "app-storage",
	plugins: "plugin-storage",
} as const;

const SCOPE_SEPARATOR = ":";

type StorageSpaceOptions = {
	encodeKeys?: boolean;
};

export class ScopedStorageSpace<T = unknown> {
	private readonly collection = store.collection<T>(this.collectionName);
	private readonly encodeKeys: boolean;

	constructor(
		private readonly collectionName: string,
		private readonly scopeId: string,
		options: StorageSpaceOptions = {},
	) {
		this.encodeKeys = options.encodeKeys ?? false;
	}

	get(key: string): Promise<T | null> {
		return this.collection.get(this.entryId(key));
	}

	async set(key: string, value: T): Promise<void> {
		await this.collection.put(this.entryId(key), value);
	}

	delete(key: string): Promise<void> {
		return this.collection.delete(this.entryId(key));
	}

	async keys(): Promise<string[]> {
		const docs = await this.entries();
		return docs
			.map((doc) => this.keyFromEntryId(doc.id))
			.filter((key): key is string => key !== null);
	}

	async clear(): Promise<void> {
		const docs = await this.entries();
		await Promise.all(docs.map((doc) => this.collection.delete(doc.id)));
	}

	entries(): Promise<Array<StoreDocument<T>>> {
		return this.collection.list({ prefix: this.prefix() });
	}

	private prefix() {
		return `${this.scopeId}${SCOPE_SEPARATOR}`;
	}

	private entryId(key: string) {
		return `${this.prefix()}${this.encodeKeys ? encodeURIComponent(key) : key}`;
	}

	private keyFromEntryId(id: string) {
		const prefix = this.prefix();
		if (!id.startsWith(prefix)) return null;

		const key = id.slice(prefix.length);
		if (!this.encodeKeys) return key;

		try {
			return decodeURIComponent(key);
		} catch {
			return null;
		}
	}
}

export class StorageSpace<T = unknown> {
	constructor(
		readonly collectionName: string,
		private readonly options: StorageSpaceOptions = {},
	) {}

	scope(scopeId: string) {
		return new ScopedStorageSpace<T>(this.collectionName, scopeId, this.options);
	}
}

export function storageSpace<T = unknown>(
	collectionName: string,
	options: StorageSpaceOptions = {},
) {
	return new StorageSpace<T>(collectionName, options);
}
