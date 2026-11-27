package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestPlatformAssetsServesBuiltFilesCrossOrigin(t *testing.T) {
	fsys := fstest.MapFS{
		"static/assets/atlas-abc.png": {Data: []byte("png")},
		"static/platform.js":          {Data: []byte("js")},
	}
	handler := platformAssets(fsys)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/atlas-abc.png", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "png" {
		t.Fatalf("got %d %q, want 200 png", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing CORS header: %v", rec.Header())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type %q, want image/png", ct)
	}

	for _, path := range []string{"/assets/missing.png", "/assets/", "/assets/../platform.js"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusOK {
			t.Fatalf("%s: got 200, want an error", path)
		}
	}
}
