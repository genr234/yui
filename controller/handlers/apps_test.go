package handlers

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"kiosk/controller/internal/config"
)

const signedTestAppSource = `export default {
  schema: "yui.simple-js.v0",
  id: "signed.test",
  name: "Signed Test",
  version: "1.0.0",
  permissions: ["storage"],
  mount(ctx) {
    return () => ctx.ui.text("ok")
  }
}`

func TestAppSourcesRejectHTTP(t *testing.T) {
	if err := requireHTTPS("http://example.com/catalog.json"); err == nil {
		t.Fatal("expected non-https app source to be rejected")
	}
}

func TestFetchCatalogRejectsMalformedCatalog(t *testing.T) {
	withAppTestServer(t, func(server *httptest.Server) {
		server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"schema":"nope"}`))
		})
		if _, err := fetchCatalog(server.URL); err == nil {
			t.Fatal("expected malformed catalog to fail")
		}
	})
}

func TestMetadataFromAppSourceRejectsMalformedSource(t *testing.T) {
	if _, err := metadataFromAppSource(`export default { schema: "yui.simple-js.v0" }`); err == nil {
		t.Fatal("expected malformed app source to fail")
	}
}

func TestVerifyCatalogAppRejectsHashMismatch(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entry := signedCatalogEntry(publicKey, privateKey, signedTestAppSource)
	entry.App.SourceSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := verifyCatalogApp(entry, []string{base64.StdEncoding.EncodeToString(publicKey)}, []byte(signedTestAppSource)); err == nil {
		t.Fatal("expected hash mismatch")
	}
}

func TestVerifyCatalogAppRejectsInvalidSignature(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entry := signedCatalogEntry(publicKey, otherPrivateKey, signedTestAppSource)
	if err := verifyCatalogApp(entry, []string{base64.StdEncoding.EncodeToString(publicKey)}, []byte(signedTestAppSource)); err == nil {
		t.Fatal("expected invalid signature")
	}
}

func TestAppInstallStoresSignedApp(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	withAppTestServer(t, func(server *httptest.Server) {
		catalog := catalogDocument{
			Schema:      "yui.catalog.v0",
			Name:        "Tests",
			Publisher:   "Yui Tests",
			SigningKeys: []string{base64.StdEncoding.EncodeToString(publicKey)},
			Apps: []catalogAppRecord{
				signedCatalogEntry(publicKey, privateKey, signedTestAppSource).App,
			},
		}
		catalog.Apps[0].SourceURL = server.URL + "/app.yui.js"
		server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app.yui.js" {
				_, _ = w.Write([]byte(signedTestAppSource))
				return
			}
			_ = json.NewEncoder(w).Encode(catalog)
		})

		registry := testRegistry(t)
		added, err := AppSourcesAddCommand{}.Handle(registry, mustJSON(t, map[string]string{"url": server.URL + "/catalog.json"}))
		if err != nil {
			t.Fatal(err)
		}
		source := added.(appSourceRecord)
		if _, err := (AppSourcesRefreshCommand{}).Handle(registry, mustJSON(t, map[string]string{"id": source.ID})); err != nil {
			t.Fatal(err)
		}
		catalogID := catalogEntryID(source.ID, "signed.test", "1.0.0")
		if _, err := (AppInstallCommand{}).Handle(registry, mustJSON(t, map[string]string{"catalogId": catalogID})); err != nil {
			t.Fatal(err)
		}
		installed, err := AppInstalledListCommand{}.Handle(registry, nil)
		if err != nil {
			t.Fatal(err)
		}
		apps := installed.([]installedAppRecord)
		if len(apps) != 1 || apps[0].ID != "signed.test" || apps[0].Source == "" {
			t.Fatalf("unexpected installed apps: %+v", apps)
		}
	})
}

func TestAppUninstallDoesNotClearAppStorage(t *testing.T) {
	registry := testRegistry(t)
	db, err := registry.Store()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Collection(installedAppsCollection).Put("signed.test", installedAppRecord{ID: "signed.test"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Collection("app-storage").Put("signed.test:key", map[string]string{"value": "kept"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (AppUninstallCommand{}).Handle(registry, mustJSON(t, map[string]string{"id": "signed.test"})); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.Collection(installedAppsCollection).Get("signed.test"); err != nil || ok {
		t.Fatalf("installed app was not removed: ok=%t err=%v", ok, err)
	}
	if _, ok, err := db.Collection("app-storage").Get("signed.test:key"); err != nil || !ok {
		t.Fatalf("app storage was cleared unexpectedly: ok=%t err=%v", ok, err)
	}
}

func signedCatalogEntry(publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, source string) catalogEntryRecord {
	hash := sha256.Sum256([]byte(source))
	signature := ed25519.Sign(privateKey, []byte(source))
	return catalogEntryRecord{
		SourceID: "source",
		App: catalogAppRecord{
			ID:           "signed.test",
			Name:         "Signed Test",
			Version:      "1.0.0",
			Permissions:  []string{"storage"},
			SourceURL:    "https://example.com/app.yui.js",
			SourceSHA256: hex.EncodeToString(hash[:]),
			Signature:    base64.StdEncoding.EncodeToString(signature),
		},
		Verified:  true,
		Publisher: base64.StdEncoding.EncodeToString(publicKey),
	}
}

func withAppTestServer(t *testing.T, run func(server *httptest.Server)) {
	t.Helper()
	previousClient := appHTTPClient
	server := httptest.NewTLSServer(http.NewServeMux())
	appHTTPClient = server.Client()
	t.Cleanup(func() {
		appHTTPClient = previousClient
		server.Close()
	})
	run(server)
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(config.Config{StorePath: filepath.Join(t.TempDir(), "store.db")})
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
