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
	db *bbolt.DB
}

type Collection struct {
	db   *DB
	name string
}

type Document struct {
	ID    string          `json:"id"`
	Value json.RawMessage `json:"value"`
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}

	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

func (d *DB) Collection(name string) Collection {
	return Collection{db: d, name: name}
}

func (d *DB) BucketNames() ([]string, error) {
	var names []string
	err := d.db.View(func(tx *bbolt.Tx) error {
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

	return c.db.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(c.name))
		if err != nil {
			return err
		}
		return bucket.Put([]byte(id), data)
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

	var id string
	err = c.db.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(c.name))
		if err != nil {
			return err
		}
		nextID, err := bucket.NextSequence()
		if err != nil {
			return err
		}
		id = fmt.Sprintf("%d", nextID)
		return bucket.Put([]byte(id), data)
	})
	if err != nil {
		return Document{}, err
	}

	return Document{ID: id, Value: data}, nil
}

func (c Collection) Get(id string) (Document, bool, error) {
	if err := validateName("collection", c.name); err != nil {
		return Document{}, false, err
	}
	if err := validateName("id", id); err != nil {
		return Document{}, false, err
	}

	var doc Document
	err := c.db.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(c.name))
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

	return c.db.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(c.name))
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

	var docs []Document
	err := c.db.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(c.name))
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

	count := 0
	err := c.db.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(c.name))
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

	var doc Document
	err := c.db.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(c.name))
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
	return c.db.db.Update(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(c.name)) == nil {
			return nil
		}
		return tx.DeleteBucket([]byte(c.name))
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
