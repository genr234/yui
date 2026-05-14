package handlers

import "testing"

func TestStoreCommandsRejectPlatformCollections(t *testing.T) {
	registry := testRegistry(t)

	_, err := StorePutCommand{}.Handle(registry, mustJSON(t, map[string]any{
		"collection": "installed-apps",
		"id":         "terminal",
		"value":      map[string]any{"name": "Terminal"},
	}))
	if err == nil {
		t.Fatal("expected generic store command to reject platform collection")
	}
}

func TestStoreCommandsAllowScopedStorageCollections(t *testing.T) {
	registry := testRegistry(t)

	if _, err := (StorePutCommand{}).Handle(registry, mustJSON(t, map[string]any{
		"collection": "app-storage",
		"id":         "demo:score",
		"value":      42,
	})); err != nil {
		t.Fatalf("put app storage: %v", err)
	}

	value, err := StoreGetCommand{}.Handle(registry, mustJSON(t, map[string]string{
		"collection": "app-storage",
		"id":         "demo:score",
	}))
	if err != nil {
		t.Fatalf("get app storage: %v", err)
	}

	doc := value.(map[string]any)
	if doc["id"] != "demo:score" || doc["value"] != float64(42) {
		t.Fatalf("unexpected document: %+v", doc)
	}
}
