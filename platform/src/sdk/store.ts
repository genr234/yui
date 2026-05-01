import { bridge } from "./bridge";

export type StoreDocument<T> = {
	id: string;
	value: T;
};

export type ListOptions = {
	prefix?: string;
	limit?: number;
};

export class StoreCollection<T = unknown> {
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

	find(id: string): Promise<StoreDocument<T> | null> {
		return bridge.send<StoreDocument<T> | null>("store.get", {
			collection: this.name,
			id,
		});
	}

	put(id: string, value: T): Promise<StoreDocument<T>> {
		return bridge.send<StoreDocument<T>>("store.put", {
			collection: this.name,
			id,
			value,
		});
	}

	create(value: T): Promise<StoreDocument<T>> {
		return bridge.send<StoreDocument<T>>("store.create", {
			collection: this.name,
			value,
		});
	}

	update(id: string, patch: Partial<T>): Promise<StoreDocument<T>> {
		return bridge.send<StoreDocument<T>>("store.update", {
			collection: this.name,
			id,
			patch,
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

	async all(): Promise<T[]> {
		const docs = await this.list();
		return docs.map((doc) => doc.value);
	}

	async count(prefix = ""): Promise<number> {
		const result = await bridge.send<{ count: number }>("store.count", {
			collection: this.name,
			prefix,
		});
		return result.count;
	}

	clear(): Promise<void> {
		return bridge.send<void>("store.clear", {
			collection: this.name,
		});
	}
}

export const store = {
	collection<T = unknown>(name: string) {
		return new StoreCollection<T>(name);
	},
	collections: () => bridge.send<string[]>("store.collections"),
	get: <T = unknown>(collection: string, id: string) =>
		new StoreCollection<T>(collection).get(id),
	find: <T = unknown>(collection: string, id: string) =>
		new StoreCollection<T>(collection).find(id),
	put: <T = unknown>(collection: string, id: string, value: T) =>
		new StoreCollection<T>(collection).put(id, value),
	create: <T = unknown>(collection: string, value: T) =>
		new StoreCollection<T>(collection).create(value),
	update: <T = unknown>(collection: string, id: string, patch: Partial<T>) =>
		new StoreCollection<T>(collection).update(id, patch),
	delete: (collection: string, id: string) =>
		new StoreCollection(collection).delete(id),
	list: <T = unknown>(collection: string, options?: ListOptions) =>
		new StoreCollection<T>(collection).list(options),
};
