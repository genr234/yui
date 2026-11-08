import { bridge } from "./bridge";

export type StoreDocument<T> = {
	id: string;
	value: T;
	store_epoch?: number;
};

export type ScopedValue<T> = {
	value: T | null;
	epoch: number;
};

export type ListOptions = {
	prefix?: string;
	limit?: number;
};

class StoreCollection<T = unknown> {
	readonly name: string;

	constructor(name: string) {
		if (!name) {
			throw new Error("collection name is required");
		}
		this.name = name;
	}

	async get(id: string): Promise<T | null> {
		const doc = await bridge.send<StoreDocument<T> | null>("store.get", {
			collection: this.name,
			id,
		});
		return doc?.value ?? null;
	}

	/**
	 * Read a document together with the account-store epoch. Missing
	 * documents still carry an epoch so a later put can be refused if the
	 * account changes. Throws when the controller does not report an epoch.
	 */
	async getScoped(id: string): Promise<ScopedValue<T>> {
		const doc = await bridge.send<StoreDocument<T | null> | null>("store.get", {
			collection: this.name,
			id,
		});
		if (!doc || typeof doc.store_epoch !== "number") {
			throw new Error("store scope unavailable");
		}
		return { value: doc.value ?? null, epoch: doc.store_epoch };
	}

	put(id: string, value: T): Promise<StoreDocument<T>> {
		return bridge.send<StoreDocument<T>>("store.put", {
			collection: this.name,
			id,
			value,
		});
	}

	putScoped(id: string, value: T, epoch: number): Promise<StoreDocument<T>> {
		return bridge.send<StoreDocument<T>>("store.put", {
			collection: this.name,
			id,
			value,
			store_epoch: epoch,
		});
	}

	delete(id: string): Promise<void> {
		return bridge.send<void>("store.delete", {
			collection: this.name,
			id,
		});
	}

	list(options: ListOptions = {}): Promise<Array<StoreDocument<T>>> {
		return bridge.send<Array<StoreDocument<T>>>("store.list", {
			collection: this.name,
			prefix: options.prefix ?? "",
			limit: options.limit ?? 0,
		});
	}

	keys(options: ListOptions = {}): Promise<string[]> {
		return bridge.send<string[]>("store.keys", {
			collection: this.name,
			prefix: options.prefix ?? "",
			limit: options.limit ?? 0,
		});
	}
}

export const internalStore = {
	collection<T = unknown>(name: string) {
		return new StoreCollection<T>(name);
	},
};
