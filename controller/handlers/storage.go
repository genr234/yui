package handlers

import (
	"encoding/json"
	"fmt"

	"kiosk/controller/internal/store"
)

const defaultStorageCollection = "storage"

func StorageCommands() []Command {
	return []Command{
		StorageGetCommand{},
		StorageSetCommand{},
		StorageDeleteCommand{},
		StoreGetCommand{},
		StorePutCommand{},
		StoreCreateCommand{},
		StoreUpdateCommand{},
		StoreDeleteCommand{},
		StoreListCommand{},
		StoreCountCommand{},
		StoreClearCommand{},
		StoreCollectionsCommand{},
	}
}

type StorageGetCommand struct{}

func (StorageGetCommand) Name() string { return "storage.get" }

func (StorageGetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	doc, ok, err := db.Collection(defaultStorageCollection).Get(p.Key)
	if err != nil || !ok {
		return nil, err
	}
	var value string
	if err := json.Unmarshal(doc.Value, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

type StorageSetCommand struct{}

func (StorageSetCommand) Name() string { return "storage.set" }

func (StorageSetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	return nil, db.Collection(defaultStorageCollection).Put(p.Key, p.Value)
}

type StorageDeleteCommand struct{}

func (StorageDeleteCommand) Name() string { return "storage.delete" }

func (StorageDeleteCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	return nil, db.Collection(defaultStorageCollection).Delete(p.Key)
}

type StoreGetCommand struct{}

func (StoreGetCommand) Name() string { return "store.get" }

func (StoreGetCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p documentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	doc, ok, err := collection.Get(p.ID)
	if err != nil || !ok {
		return nil, err
	}
	return decodeDocument(doc)
}

type StorePutCommand struct{}

func (StorePutCommand) Name() string { return "store.put" }

func (StorePutCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p valueParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	if err := collection.Put(p.ID, p.Value); err != nil {
		return nil, err
	}
	doc, _, err := collection.Get(p.ID)
	if err != nil {
		return nil, err
	}
	return decodeDocument(doc)
}

type StoreCreateCommand struct{}

func (StoreCreateCommand) Name() string { return "store.create" }

func (StoreCreateCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Collection string          `json:"collection"`
		Value      json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	doc, err := collection.Create(p.Value)
	if err != nil {
		return nil, err
	}
	return decodeDocument(doc)
}

type StoreUpdateCommand struct{}

func (StoreUpdateCommand) Name() string { return "store.update" }

func (StoreUpdateCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Collection string         `json:"collection"`
		ID         string         `json:"id"`
		Patch      map[string]any `json:"patch"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	doc, err := collection.Merge(p.ID, p.Patch)
	if err != nil {
		return nil, err
	}
	return decodeDocument(doc)
}

type StoreDeleteCommand struct{}

func (StoreDeleteCommand) Name() string { return "store.delete" }

func (StoreDeleteCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p documentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	return nil, collection.Delete(p.ID)
}

type StoreListCommand struct{}

func (StoreListCommand) Name() string { return "store.list" }

func (StoreListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Collection string `json:"collection"`
		Prefix     string `json:"prefix"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	docs, err := collection.List(store.ListOptions{Prefix: p.Prefix, Limit: p.Limit})
	if err != nil {
		return nil, err
	}
	return decodeDocuments(docs)
}

type StoreCountCommand struct{}

func (StoreCountCommand) Name() string { return "store.count" }

func (StoreCountCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Collection string `json:"collection"`
		Prefix     string `json:"prefix"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	count, err := collection.Count(p.Prefix)
	if err != nil {
		return nil, err
	}
	return map[string]int{"count": count}, nil
}

type StoreClearCommand struct{}

func (StoreClearCommand) Name() string { return "store.clear" }

func (StoreClearCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Collection string `json:"collection"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	collection, err := collection(r, p.Collection)
	if err != nil {
		return nil, err
	}
	return nil, collection.Clear()
}

type StoreCollectionsCommand struct{}

func (StoreCollectionsCommand) Name() string { return "store.collections" }

func (StoreCollectionsCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	db, err := r.Store()
	if err != nil {
		return nil, err
	}
	return db.BucketNames()
}

type documentParams struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
}

type valueParams struct {
	Collection string          `json:"collection"`
	ID         string          `json:"id"`
	Value      json.RawMessage `json:"value"`
}

func collection(r *Registry, name string) (store.Collection, error) {
	if name == "" {
		return store.Collection{}, fmt.Errorf("collection is required")
	}
	db, err := r.Store()
	if err != nil {
		return store.Collection{}, err
	}
	return db.Collection(name), nil
}

func decodeDocument(doc store.Document) (map[string]any, error) {
	var value any
	if err := json.Unmarshal(doc.Value, &value); err != nil {
		return nil, err
	}
	return map[string]any{"id": doc.ID, "value": value}, nil
}

func decodeDocuments(docs []store.Document) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		decoded, err := decodeDocument(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, decoded)
	}
	return result, nil
}
