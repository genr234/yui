package store

import "testing"

func TestCollectionPutGetListMergeDelete(t *testing.T) {
	db, err := Open(t.TempDir() + "/store.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	apps := db.Collection("apps")
	if err := apps.Put("terminal", map[string]any{"name": "Terminal", "enabled": true}); err != nil {
		t.Fatalf("put: %v", err)
	}

	var app struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	ok, err := apps.Decode("terminal", &app)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ok || app.Name != "Terminal" || !app.Enabled {
		t.Fatalf("unexpected app: ok=%t app=%+v", ok, app)
	}

	if _, err := apps.Merge("terminal", map[string]any{"enabled": false}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	ok, err = apps.Decode("terminal", &app)
	if err != nil {
		t.Fatalf("decode merged: %v", err)
	}
	if !ok || app.Enabled {
		t.Fatalf("merge did not update app: ok=%t app=%+v", ok, app)
	}

	docs, err := apps.List(ListOptions{Prefix: "term"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "terminal" {
		t.Fatalf("unexpected docs: %+v", docs)
	}

	count, err := apps.Count("")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("unexpected count: %d", count)
	}

	if err := apps.Delete("terminal"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := apps.Get("terminal"); err != nil || ok {
		t.Fatalf("expected document to be gone: ok=%t err=%v", ok, err)
	}
}

func TestCollectionCreateAllocatesIDs(t *testing.T) {
	db, err := Open(t.TempDir() + "/store.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	settings := db.Collection("settings")
	first, err := settings.Create(map[string]any{"name": "one"})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := settings.Create(map[string]any{"name": "two"})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Fatalf("unexpected ids: first=%q second=%q", first.ID, second.ID)
	}
}
