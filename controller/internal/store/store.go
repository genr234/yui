package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"
)

var ErrNotFound = errors.New("document not found")

type DB struct {
	backend Backend
}

type Collection struct {
	db   *DB
	name string
}

type Document struct {
	ID    string          `json:"id"`
	Value json.RawMessage `json:"value"`
}

type Backend interface {
	Close() error
	CollectionNames() ([]string, error)
	Put(collection string, id string, value []byte) error
	Create(collection string, value []byte) (Document, error)
	Get(collection string, id string) (Document, bool, error)
	Delete(collection string, id string) error
	List(collection string, opts ListOptions) ([]Document, error)
	Count(collection string, prefix string) (int, error)
	Merge(collection string, id string, patch map[string]any) (Document, error)
	Clear(collection string) error
}

func Open(path string) (*DB, error) {
	backend, err := OpenBoltBackend(path)
	if err != nil {
		return nil, err
	}
	return OpenWithBackend(backend), nil
}

func OpenWithBackend(backend Backend) *DB {
	return &DB{backend: backend}
}

type BoltBackend struct {
	db *bbolt.DB
}

func OpenBoltBackend(path string) (*BoltBackend, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}

	return &BoltBackend{db: db}, nil
}

func (d *DB) Close() error {
	if d == nil || d.backend == nil {
		return nil
	}
	return d.backend.Close()
}

func (d *DB) Collection(name string) Collection {
	return Collection{db: d, name: name}
}

func (d *DB) BucketNames() ([]string, error) {
	return d.CollectionNames()
}

func (d *DB) CollectionNames() ([]string, error) {
	return d.backend.CollectionNames()
}

func (b *BoltBackend) Close() error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Close()
}

func (b *BoltBackend) CollectionNames() ([]string, error) {
	var names []string
	err := b.db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
			names = append(names, string(name))
			return nil
		})
	})
	return names, err
}

func (c Collection) Put(id string, value any) error {
	if err := validateName("collection", c.name); err != nil {
		return err
	}
	if err := validateName("id", id); err != nil {
		return err
	}

	data, err := marshal(value)
	if err != nil {
		return err
	}

	return c.db.backend.Put(c.name, id, data)
}

func (b *BoltBackend) Put(collection string, id string, value []byte) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(collection))
		if err != nil {
			return err
		}
		return bucket.Put([]byte(id), value)
	})
}

func (c Collection) Create(value any) (Document, error) {
	if err := validateName("collection", c.name); err != nil {
		return Document{}, err
	}

	data, err := marshal(value)
	if err != nil {
		return Document{}, err
	}

	return c.db.backend.Create(c.name, data)
}

func (b *BoltBackend) Create(collection string, value []byte) (Document, error) {
	var id string
	err := b.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(collection))
		if err != nil {
			return err
		}
		nextID, err := bucket.NextSequence()
		if err != nil {
			return err
		}
		id = fmt.Sprintf("%d", nextID)
		return bucket.Put([]byte(id), value)
	})
	if err != nil {
		return Document{}, err
	}

	return Document{ID: id, Value: clone(value)}, nil
}

func (c Collection) Get(id string) (Document, bool, error) {
	if err := validateName("collection", c.name); err != nil {
		return Document{}, false, err
	}
	if err := validateName("id", id); err != nil {
		return Document{}, false, err
	}

	return c.db.backend.Get(c.name, id)
}

func (b *BoltBackend) Get(collection string, id string) (Document, bool, error) {
	var doc Document
	err := b.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(collection))
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte(id))
		if value == nil {
			return nil
		}
		doc = Document{ID: id, Value: clone(value)}
		return nil
	})
	return doc, doc.Value != nil, err
}

func (c Collection) Decode(id string, out any) (bool, error) {
	doc, ok, err := c.Get(id)
	if err != nil || !ok {
		return ok, err
	}
	return true, json.Unmarshal(doc.Value, out)
}

func (c Collection) Delete(id string) error {
	if err := validateName("collection", c.name); err != nil {
		return err
	}
	if err := validateName("id", id); err != nil {
		return err
	}

	return c.db.backend.Delete(c.name, id)
}

func (b *BoltBackend) Delete(collection string, id string) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(collection))
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(id))
	})
}

func (c Collection) List(opts ListOptions) ([]Document, error) {
	if err := validateName("collection", c.name); err != nil {
		return nil, err
	}

	return c.db.backend.List(c.name, opts)
}

func (b *BoltBackend) List(collection string, opts ListOptions) ([]Document, error) {
	var docs []Document
	err := b.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(collection))
		if bucket == nil {
			return nil
		}

		cursor := bucket.Cursor()
		prefix := []byte(opts.Prefix)
		limit := opts.Limit
		if limit < 0 {
			limit = 0
		}

		for key, value := first(cursor, prefix); key != nil; key, value = cursor.Next() {
			if len(prefix) > 0 && !hasPrefix(key, prefix) {
				break
			}
			docs = append(docs, Document{ID: string(key), Value: clone(value)})
			if limit > 0 && len(docs) >= limit {
				break
			}
		}
		return nil
	})
	return docs, err
}

func (c Collection) Count(prefix string) (int, error) {
	if err := validateName("collection", c.name); err != nil {
		return 0, err
	}

	return c.db.backend.Count(c.name, prefix)
}

func (b *BoltBackend) Count(collection string, prefix string) (int, error) {
	count := 0
	err := b.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(collection))
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		prefixBytes := []byte(prefix)
		for key, _ := first(cursor, prefixBytes); key != nil; key, _ = cursor.Next() {
			if len(prefixBytes) > 0 && !hasPrefix(key, prefixBytes) {
				break
			}
			count++
		}
		return nil
	})
	return count, err
}

func (c Collection) Merge(id string, patch map[string]any) (Document, error) {
	if err := validateName("collection", c.name); err != nil {
		return Document{}, err
	}
	if err := validateName("id", id); err != nil {
		return Document{}, err
	}

	return c.db.backend.Merge(c.name, id, patch)
}

func (b *BoltBackend) Merge(collection string, id string, patch map[string]any) (Document, error) {
	var doc Document
	err := b.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(collection))
		if err != nil {
			return err
		}

		value := map[string]any{}
		if current := bucket.Get([]byte(id)); current != nil {
			if err := json.Unmarshal(current, &value); err != nil {
				return fmt.Errorf("decode existing document: %w", err)
			}
		}
		for key, patchValue := range patch {
			value[key] = patchValue
		}

		data, err := marshal(value)
		if err != nil {
			return err
		}
		if err := bucket.Put([]byte(id), data); err != nil {
			return err
		}
		doc = Document{ID: id, Value: data}
		return nil
	})
	return doc, err
}

func (c Collection) Clear() error {
	if err := validateName("collection", c.name); err != nil {
		return err
	}
	return c.db.backend.Clear(c.name)
}

func (b *BoltBackend) Clear(collection string) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(collection)) == nil {
			return nil
		}
		return tx.DeleteBucket([]byte(collection))
	})
}

type ListOptions struct {
	Prefix string
	Limit  int
}

func marshal(value any) ([]byte, error) {
	if raw, ok := value.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("invalid json value")
		}
		return clone(raw), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func validateName(label string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	return nil
}

func first(cursor *bbolt.Cursor, prefix []byte) ([]byte, []byte) {
	if len(prefix) == 0 {
		return cursor.First()
	}
	return cursor.Seek(prefix)
}

func hasPrefix(value []byte, prefix []byte) bool {
	if len(prefix) > len(value) {
		return false
	}
	for i := range prefix {
		if value[i] != prefix[i] {
			return false
		}
	}
	return true
}

func clone(data []byte) []byte {
	copied := make([]byte, len(data))
	copy(copied, data)
	return copied
}
