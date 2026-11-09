package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"kiosk/controller/internal/appsaves"
	"kiosk/controller/internal/assets"
	"kiosk/controller/internal/config"
	"kiosk/controller/internal/pad/session"
	"kiosk/controller/internal/webapps"
)

const webTestPort = 26781

// newWebRegistry is a registry with a config directory, an account, and a
// web port (it never listens; tests call the server's handler directly).
func newWebRegistry(t *testing.T, serverURL string) *Registry {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath: filepath.Join(dir, "controller.json"),
		ConfigDir:  dir,
		StorePath:  filepath.Join(dir, "store.db"),
		WebPort:    webTestPort,
	}
	if serverURL != "" {
		cfg.ServerURL = serverURL
		cfg.ActiveAccountID = "acct"
		cfg.Accounts = []config.AccountConfig{{ID: "acct", Name: "Lobby", KioskID: "k1", DeviceToken: "device-token"}}
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)
	r := NewRegistry(cfg)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func originHost(t *testing.T, origin string) string {
	t.Helper()
	u, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func serve(r *Registry, host, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	r.WebApps().Handler().ServeHTTP(rec, req)
	return rec
}

func putAsset(t *testing.T, r *Registry, rec assets.Local) {
	t.Helper()
	rec.State = assets.StateReady
	db, err := r.Store()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Collection(assets.Collection).Put(rec.ID, rec); err != nil {
		t.Fatal(err)
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestWebAppsOriginCommand(t *testing.T) {
	r := newWebRegistry(t, "")
	out, err := dispatch(t, r, "webapps.origin", `{"app_id":"party.quiz"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(map[string]any)
	want := "http://" + webapps.AppKey("party.quiz") + ".apps.localhost:" + fmt.Sprint(webTestPort)
	if got["origin"] != want || got["has_bundle"] != false {
		t.Fatalf("origin result: %+v", got)
	}
	if _, err := dispatch(t, r, "webapps.origin", `{"app_id":"../x"}`); err == nil {
		t.Fatal("invalid app id accepted")
	}
	// Only apps that asked pass the Host check.
	if rec := serve(r, webapps.AppKey("other.app")+".apps.localhost:"+fmt.Sprint(webTestPort), "/_yui/embed.js"); rec.Code != 421 {
		t.Fatalf("unregistered app: %d", rec.Code)
	}
	if rec := serve(r, originHost(t, want), "/_yui/embed.js"); rec.Code != 200 {
		t.Fatalf("registered app: %d", rec.Code)
	}
}

func TestWebAppsDisabledByPortZero(t *testing.T) {
	r := NewRegistry(config.Config{StorePath: filepath.Join(t.TempDir(), "s.db")})
	if _, err := dispatch(t, r, "webapps.origin", `{"app_id":"party.quiz"}`); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("err = %v", err)
	}
	r.StartWebApps(context.Background(), nil) // must not listen or panic
}

func TestWebAppsServeInstalledAndDevBundles(t *testing.T) {
	r := newWebRegistry(t, "")
	r.web.dev = fstest.MapFS{"app-web/dev.app/index.html": {Data: []byte("embedded")}}
	dir := filepath.Join(r.appsWebRoot(), "inst.app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("installed"), 0o644); err != nil {
		t.Fatal(err)
	}
	for app, want := range map[string]string{"dev.app": "embedded", "inst.app": "installed"} {
		out, err := dispatch(t, r, "webapps.origin", fmt.Sprintf(`{"app_id":%q}`, app))
		if err != nil {
			t.Fatal(err)
		}
		m := out.(map[string]any)
		if m["has_bundle"] != true {
			t.Fatalf("%s: has_bundle false", app)
		}
		if rec := serve(r, originHost(t, m["origin"].(string)), "/"); rec.Body.String() != want {
			t.Fatalf("%s: %q", app, rec.Body.String())
		}
	}
}

func TestAssetsURLBindsTokenToAppAndRequiresReadyAsset(t *testing.T) {
	r := newWebRegistry(t, "")
	file := filepath.Join(t.TempDir(), "track.mp3")
	if err := os.WriteFile(file, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	putAsset(t, r, assets.Local{ID: "5", Kind: "media", Title: "Track", Filename: "track.mp3", Path: file})
	db, _ := r.Store()
	_ = db.Collection(assets.Collection).Put("6", assets.Local{ID: "6", Kind: "media", Filename: "x.mp3", State: assets.StateDownloading, Path: file})

	a, _ := dispatch(t, r, "webapps.origin", `{"app_id":"app.one"}`)
	b, _ := dispatch(t, r, "webapps.origin", `{"app_id":"app.two"}`)
	hostA := originHost(t, a.(map[string]any)["origin"].(string))
	hostB := originHost(t, b.(map[string]any)["origin"].(string))

	if _, err := dispatch(t, r, "assets.url", `{"app_id":"app.one","asset_id":"missing"}`); err == nil || err.Error() != "asset is not available on this kiosk" {
		t.Fatalf("missing asset: %v", err)
	}
	if _, err := dispatch(t, r, "assets.url", `{"app_id":"app.one","asset_id":"6"}`); err == nil {
		t.Fatal("asset that is still downloading must not get a URL")
	}
	out, err := dispatch(t, r, "assets.url", `{"app_id":"app.one","asset_id":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	res := out.(map[string]string)
	if _, err := time.Parse(time.RFC3339, res["expires_at"]); err != nil {
		t.Fatalf("expires_at: %v", err)
	}
	if !strings.HasPrefix(res["url"], a.(map[string]any)["origin"].(string)+"/_asset/") {
		t.Fatalf("url = %q", res["url"])
	}
	path := strings.TrimPrefix(res["url"], a.(map[string]any)["origin"].(string))
	if rec := serve(r, hostA, path); rec.Code != 200 || rec.Body.String() != "0123456789" {
		t.Fatalf("own origin: %d %q", rec.Code, rec.Body.String())
	}
	if rec := serve(r, hostB, path); rec.Code != 404 {
		t.Fatalf("other app's origin: %d", rec.Code)
	}
}

func TestWebAppsRuntimeMountsNewestMatchingRuntime(t *testing.T) {
	r := newWebRegistry(t, "")
	tmp := t.TempDir()
	mkZip := func(name, body string) string {
		p := filepath.Join(tmp, name)
		if err := os.WriteFile(p, zipBytes(t, map[string]string{"core.js": body}), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	putAsset(t, r, assets.Local{ID: "3", Kind: "runtime", Filename: "a.zip", Path: mkZip("a.zip", "v1"), Metadata: json.RawMessage(`{"runtime":"engine","version":"1.0"}`)})
	putAsset(t, r, assets.Local{ID: "10", Kind: "runtime", Filename: "b.zip", Path: mkZip("b.zip", "v2"), Metadata: json.RawMessage(`{"runtime":"engine","version":"2.0"}`)})
	putAsset(t, r, assets.Local{ID: "11", Kind: "runtime", Filename: "c.zip", Path: mkZip("c.zip", "other"), Metadata: json.RawMessage(`{"runtime":"another","version":"9"}`)})
	putAsset(t, r, assets.Local{ID: "12", Kind: "media", Filename: "d.zip", Path: mkZip("d.zip", "media"), Metadata: json.RawMessage(`{"runtime":"engine","version":"9"}`)})

	// Without asking for its origin first an app is registered by the call.
	out, err := dispatch(t, r, "webapps.runtime", `{"app_id":"app.one","name":"engine"}`)
	if err != nil {
		t.Fatal(err)
	}
	res := out.(map[string]string)
	origin := "http://" + webapps.AppKey("app.one") + ".apps.localhost:" + fmt.Sprint(webTestPort)
	if res["url"] != origin+"/_runtime/engine/" || res["version"] != "2.0" {
		t.Fatalf("result: %+v", res)
	}
	if rec := serve(r, originHost(t, origin), "/_runtime/engine/core.js"); rec.Code != 200 || rec.Body.String() != "v2" {
		t.Fatalf("mounted file: %d %q", rec.Code, rec.Body.String())
	}
	// Another app never mounted it.
	if _, err := dispatch(t, r, "webapps.origin", `{"app_id":"app.two"}`); err != nil {
		t.Fatal(err)
	}
	other := "http://" + webapps.AppKey("app.two") + ".apps.localhost:" + fmt.Sprint(webTestPort)
	if rec := serve(r, originHost(t, other), "/_runtime/engine/core.js"); rec.Code != 404 {
		t.Fatalf("other app: %d", rec.Code)
	}
	if _, err := dispatch(t, r, "webapps.runtime", `{"app_id":"app.one","name":"missing"}`); err == nil || err.Error() != "runtime is not available on this kiosk" {
		t.Fatalf("missing runtime: %v", err)
	}
	if _, err := dispatch(t, r, "webapps.runtime", `{"app_id":"app.one","name":"../engine"}`); err == nil {
		t.Fatal("path-like runtime name accepted")
	}
}

// --- installer ---

type webInstallEnv struct {
	r       *Registry
	catalog string
	id      string
}

// installWithWeb publishes an app whose manifest declares sha for web.zip and
// installs it. zipBody is what the server returns for web.zip.
func installWithWeb(t *testing.T, r *Registry, version string, declared string, zipBody []byte, zipStatus int) (error, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var out error
	var catalogID string
	withAppTestServer(t, func(server *httptest.Server) {
		webLine := ""
		if declared != "" {
			webLine = fmt.Sprintf("web: { sha256: %q },", declared)
		}
		source := fmt.Sprintf(`export default {
  schema: "yui.simple-js.v0",
  id: "signed.test",
  name: "Signed Test",
  version: %q,
  %s
  permissions: ["storage"],
  mount(ctx) { return () => ctx.ui.text("ok") }
}`, version, webLine)
		entry := signedCatalogEntry(publicKey, privateKey, source).App
		entry.Version = version
		entry.SourceURL = server.URL + "/apps/signed/app.yui.js"
		catalog := catalogDocument{
			Schema: "yui.catalog.v0", Name: "Tests", Publisher: "Yui Tests",
			SigningKeys: []string{base64.StdEncoding.EncodeToString(publicKey)}, Apps: []catalogAppRecord{entry},
		}
		server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/apps/signed/app.yui.js":
				_, _ = w.Write([]byte(source))
			case "/apps/signed/web.zip":
				if zipStatus != 0 {
					w.WriteHeader(zipStatus)
					return
				}
				_, _ = w.Write(zipBody)
			default:
				_ = json.NewEncoder(w).Encode(catalog)
			}
		})
		added, err := AppSourcesAddCommand{}.Handle(r, mustJSON(t, map[string]string{"url": server.URL + "/catalog.json"}))
		if err != nil {
			t.Fatal(err)
		}
		src := added.(appSourceRecord)
		if _, err := (AppSourcesRefreshCommand{}).Handle(r, mustJSON(t, map[string]string{"id": src.ID})); err != nil {
			t.Fatal(err)
		}
		catalogID = catalogEntryID(src.ID, "signed.test", version)
		_, out = (AppInstallCommand{}).Handle(r, mustJSON(t, map[string]string{"catalogId": catalogID}))
	})
	return out, catalogID
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestInstallExtractsVerifiedWebBundleAndUninstallRemovesIt(t *testing.T) {
	r := newWebRegistry(t, "")
	z := zipBytes(t, map[string]string{"index.html": "v1", "js/app.js": "x"})
	if err, _ := installWithWeb(t, r, "1.0.0", sha(z), z, 0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.appsWebRoot(), "signed.test")
	if b, err := os.ReadFile(filepath.Join(dir, "index.html")); err != nil || string(b) != "v1" {
		t.Fatalf("index: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "js", "app.js")); err != nil {
		t.Fatal(err)
	}
	// The installed bundle is what the server serves.
	out, _ := dispatch(t, r, "webapps.origin", `{"app_id":"signed.test"}`)
	m := out.(map[string]any)
	if m["has_bundle"] != true {
		t.Fatal("has_bundle should be true after install")
	}
	if rec := serve(r, originHost(t, m["origin"].(string)), "/js/app.js"); rec.Body.String() != "x" {
		t.Fatalf("served: %q", rec.Body.String())
	}
	// No staging leftovers.
	left, _ := os.ReadDir(filepath.Join(r.appsWebRoot(), webStagingDir))
	if len(left) != 0 {
		t.Fatalf("staging not cleaned: %v", left)
	}

	// A new version replaces the bundle completely.
	z2 := zipBytes(t, map[string]string{"index.html": "v2"})
	if err, _ := installWithWeb(t, r, "1.1.0", sha(z2), z2, 0); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(b) != "v2" {
		t.Fatalf("not re-extracted: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "js", "app.js")); !os.IsNotExist(err) {
		t.Fatal("old files must be gone after an update")
	}

	if _, err := (AppUninstallCommand{}).Handle(r, mustJSON(t, map[string]string{"id": "signed.test"})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("web bundle should be removed on uninstall")
	}
}

func TestInstallRefusesWebBundleHashMismatch(t *testing.T) {
	r := newWebRegistry(t, "")
	z := zipBytes(t, map[string]string{"index.html": "evil"})
	err, _ := installWithWeb(t, r, "1.0.0", sha([]byte("something else")), z, 0)
	if err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(r.appsWebRoot(), "signed.test")); !os.IsNotExist(statErr) {
		t.Fatal("nothing may be extracted on a hash mismatch")
	}
	installed, _ := AppInstalledListCommand{}.Handle(r, nil)
	if n := len(installed.([]installedAppRecord)); n != 0 {
		t.Fatalf("app recorded as installed despite refusal (%d)", n)
	}
}

func TestInstallRefusesUnsafeAndOversizedWebBundles(t *testing.T) {
	r := newWebRegistry(t, "")
	evil := zipBytes(t, map[string]string{"../escape.txt": "x"})
	if err, _ := installWithWeb(t, r, "1.0.0", sha(evil), evil, 0); err == nil {
		t.Fatal("traversal entry accepted")
	}
	if _, err := os.Stat(filepath.Join(r.appsWebRoot(), "..", "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("escaped the root")
	}
	many := map[string]string{}
	for i := 0; i <= webBundleMaxFiles; i++ {
		many[fmt.Sprintf("f%d.txt", i)] = "x"
	}
	z := zipBytes(t, many)
	if err, _ := installWithWeb(t, r, "1.0.0", sha(z), z, 0); err == nil {
		t.Fatal("2001 files accepted")
	}
	if err, _ := installWithWeb(t, r, "1.0.0", sha([]byte("x")), nil, http.StatusNotFound); err == nil {
		t.Fatal("missing web.zip accepted")
	}
}

func TestInstallWithoutWebBundleSkipsQuietly(t *testing.T) {
	r := newWebRegistry(t, "")
	if err, _ := installWithWeb(t, r, "1.0.0", "", nil, http.StatusNotFound); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.appsWebRoot()); !os.IsNotExist(err) {
		t.Fatal("no web directory expected for an app without a bundle")
	}
}

func TestWebSha256ManifestForms(t *testing.T) {
	h := strings.Repeat("ab", 32)
	cases := map[string]string{
		`web: { sha256: "` + h + `" }`:                                h,
		`web: {sha256:'` + strings.ToUpper(h) + `', runtimes: ["x"]}`: h,
		`web: { runtimes: ["x"], sha256: "` + h + `" }`:               h,
		`webSha256: "` + h + `"`:                                      h,
		`web: "./web/"`:                                               "",
		`sha256: "` + h + `"`:                                         "",
		`web: { sha256: "tooshort" }`:                                 "",
	}
	for src, want := range cases {
		if got := webSha256FromSource(src); got != want {
			t.Errorf("%q: got %q, want %q", src, got, want)
		}
	}
}

// --- saves commands ---

type saveRails struct {
	mu    sync.Mutex
	blobs map[string][]byte
	srv   *httptest.Server
	hits  int
}

func newSaveRails(t *testing.T) *saveRails {
	t.Helper()
	f := &saveRails{blobs: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits++
		if r.Header.Get("Authorization") != "Bearer device-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/kiosk/app_saves/")
		parts := strings.SplitN(rest, "/", 2)
		slot := r.URL.Query().Get("player_id") + "|"
		if len(parts) == 1 {
			var list []map[string]any
			for k, v := range f.blobs {
				if strings.HasPrefix(k, slot) {
					list = append(list, map[string]any{"key": strings.TrimPrefix(k, slot), "size": len(v), "updated_at": "2026-11-09T10:00:00.000Z"})
				}
			}
			writeJSON(w, 200, map[string]any{"saves": list})
			return
		}
		slot += parts[1]
		switch r.Method {
		case http.MethodGet:
			v, ok := f.blobs[slot]
			if !ok {
				writeJSON(w, 404, map[string]string{"error": "not_found"})
				return
			}
			w.Header().Set("X-Updated-At", "2026-11-09T10:00:00.000Z")
			_, _ = w.Write(v)
		case http.MethodPut:
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(r.Body)
			f.blobs[slot] = buf.Bytes()
			writeJSON(w, 200, map[string]any{"key": parts[1], "size": buf.Len(), "updated_at": "2026-11-09T10:00:00.000Z"})
		case http.MethodDelete:
			delete(f.blobs, slot)
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestSavesCommandsRoundTripAndMapErrors(t *testing.T) {
	rails := newSaveRails(t)
	r := newWebRegistry(t, rails.srv.URL)
	r.web.profileIDHook = func(_ context.Context, appID, playerID string) (string, error) {
		switch playerID {
		case "guest":
			return "", session.ErrNotLinked
		case "nogrant":
			return "", session.ErrNotGranted
		}
		return "prof-" + playerID, nil
	}
	data := base64.StdEncoding.EncodeToString([]byte("save bytes"))
	app := "party.quiz"

	out, err := dispatch(t, r, "saves.put", fmt.Sprintf(`{"app_id":%q,"key":"slot.1","data":%q}`, app, data))
	if err != nil {
		t.Fatal(err)
	}
	if res := out.(appsaves.Result); res.Key != "slot.1" || res.Size != 10 || res.Queued {
		t.Fatalf("put: %+v", res)
	}
	out, err = dispatch(t, r, "saves.get", fmt.Sprintf(`{"app_id":%q,"key":"slot.1"}`, app))
	if err != nil {
		t.Fatal(err)
	}
	got := out.(map[string]any)
	if got["data"] != data || got["size"] != 10 || got["key"] != "slot.1" {
		t.Fatalf("get: %+v", got)
	}
	if out, err := dispatch(t, r, "saves.get", fmt.Sprintf(`{"app_id":%q,"key":"nothing"}`, app)); err != nil || out != nil {
		t.Fatalf("missing key: %v %v", out, err)
	}
	out, err = dispatch(t, r, "saves.list", fmt.Sprintf(`{"app_id":%q}`, app))
	if err != nil || len(out.([]map[string]any)) != 1 {
		t.Fatalf("list: %v %v", out, err)
	}

	// Player scope: stored apart from the kiosk's, named by the profile id.
	if _, err := dispatch(t, r, "saves.put", fmt.Sprintf(`{"app_id":%q,"key":"slot.1","data":%q,"player_id":"p1"}`, app, data)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.snapshotConfig().ConfigDir, "app-saves", "acct", app, "player-prof-p1", "slot.1")); err != nil {
		t.Fatalf("player local copy: %v", err)
	}
	for player, prefix := range map[string]string{"guest": "not_linked: ", "nogrant": "not_granted: "} {
		for _, method := range []string{"saves.put", "saves.get", "saves.list", "saves.delete"} {
			_, err := dispatch(t, r, method, fmt.Sprintf(`{"app_id":%q,"key":"slot.1","data":%q,"player_id":%q}`, app, data, player))
			if err == nil || !strings.HasPrefix(err.Error(), prefix) {
				t.Errorf("%s as %s: %v", method, player, err)
			}
		}
	}

	if _, err := dispatch(t, r, "saves.put", fmt.Sprintf(`{"app_id":%q,"key":"BAD KEY","data":%q}`, app, data)); err == nil || !strings.HasPrefix(err.Error(), "invalid_key: ") {
		t.Fatalf("bad key: %v", err)
	}
	big := base64.StdEncoding.EncodeToString(make([]byte, appsaves.MaxBlobBytes+1))
	if _, err := dispatch(t, r, "saves.put", fmt.Sprintf(`{"app_id":%q,"key":"big","data":%q}`, app, big)); err == nil || !strings.HasPrefix(err.Error(), "too_large: ") {
		t.Fatalf("too large: %v", err)
	}
	if _, err := dispatch(t, r, "saves.put", fmt.Sprintf(`{"app_id":%q,"key":"k","data":"!!not base64"}`, app)); err == nil {
		t.Fatal("bad base64 accepted")
	}

	if _, err := dispatch(t, r, "saves.delete", fmt.Sprintf(`{"app_id":%q,"key":"slot.1"}`, app)); err != nil {
		t.Fatal(err)
	}
	if out, _ := dispatch(t, r, "saves.get", fmt.Sprintf(`{"app_id":%q,"key":"slot.1"}`, app)); out != nil {
		t.Fatalf("after delete: %v", out)
	}
}

func TestSavesWithoutAccountAreOffline(t *testing.T) {
	r := newWebRegistry(t, "")
	_, err := dispatch(t, r, "saves.put", `{"app_id":"party.quiz","key":"k","data":"eA=="}`)
	if err == nil || !strings.HasPrefix(err.Error(), "offline: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestSavesQueueWhileRailsIsDownAndFlushOnDemand(t *testing.T) {
	rails := newSaveRails(t)
	r := newWebRegistry(t, rails.srv.URL)
	rails.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	})
	out, err := dispatch(t, r, "saves.put", `{"app_id":"party.quiz","key":"k","data":"eA=="}`)
	if err != nil || !out.(appsaves.Result).Queued {
		t.Fatalf("put while down: %+v %v", out, err)
	}
	got, err := dispatch(t, r, "saves.get", `{"app_id":"party.quiz","key":"k"}`)
	if err != nil || got.(map[string]any)["data"] != "eA==" {
		t.Fatalf("get while down: %v %v", got, err)
	}

}

func TestSaveFlushLoopRunsOnKick(t *testing.T) {
	rails := newSaveRails(t)
	r := newWebRegistry(t, rails.srv.URL)
	// Queue a blob through the service with a remote that cannot be reached,
	// then give the service the working remote back.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	svc := r.AppSaves()
	real := svc.Remote
	svc.Remote = &appsaves.HTTPRemote{Endpoint: func() (string, string, bool) { return deadURL, "device-token", true }, Client: &http.Client{Timeout: time.Second}}
	res, err := svc.Put(context.Background(), appsaves.Scope{AppID: "party.quiz"}, "k", []byte("queued"))
	if err != nil || !res.Queued {
		t.Fatalf("queue: %+v %v", res, err)
	}
	svc.Remote = real

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartSaveSync(ctx)
	r.kickSaveFlush()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rails.mu.Lock()
		got := string(rails.blobs["|k"])
		rails.mu.Unlock()
		if got == "queued" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("queued save was not uploaded after a kick")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
