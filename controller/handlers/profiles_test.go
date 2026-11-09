package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/pad/protocol"
)

const profileApp = "dev.genr.party-quiz"

// profileRails is padRails plus the plan 10 endpoints, with a switchable
// state so each test can make Rails answer 409, 403, 413 and so on.
type profileRails struct {
	*padRails
	pmu     sync.Mutex
	linked  bool
	granted bool
	data    string
	version int
	// putStatus overrides the PUT answer (413 and so on).
	putStatus  int
	getCalls   int
	linkStatus int
}

func newProfileRails(t *testing.T) *profileRails {
	t.Helper()
	f := &profileRails{padRails: &padRails{}, linked: true, data: "null"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/kiosk/play_sessions", f.create)
	mux.HandleFunc("/api/kiosk/play_sessions/", f.member)
	mux.HandleFunc("/api/kiosk/play_sessions/sess-1/players/p1/profiles/", f.profile)
	mux.HandleFunc("/api/kiosk/phone_link_codes", f.linkCode)
	mux.HandleFunc("/cable", f.cable)
	f.srv = httptest.NewServer(mux)
	f.url = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

func (f *profileRails) set(fn func()) {
	f.pmu.Lock()
	defer f.pmu.Unlock()
	fn()
}

func (f *profileRails) gets() int {
	f.pmu.Lock()
	defer f.pmu.Unlock()
	return f.getCalls
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *profileRails) profile(w http.ResponseWriter, r *http.Request) {
	if !f.note(r) {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.pmu.Lock()
	defer f.pmu.Unlock()
	if !strings.HasSuffix(r.URL.Path, "/"+profileApp) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_app_id"})
		return
	}
	if !f.linked {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_linked"})
		return
	}
	if !f.granted {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not_granted"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		f.getCalls++
		writeJSON(w, http.StatusOK, map[string]any{
			"profile_id": "pid-123", "data": json.RawMessage(f.data), "version": f.version,
		})
	case http.MethodPut:
		if f.putStatus != 0 {
			body := map[string]any{"error": "too_large"}
			if f.putStatus == http.StatusConflict {
				body = map[string]any{"error": "version_conflict", "version": f.version}
			}
			writeJSON(w, f.putStatus, body)
			return
		}
		var body struct {
			Data    json.RawMessage `json:"data"`
			Version int             `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Version != f.version {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "version_conflict", "version": f.version})
			return
		}
		f.data = string(body.Data)
		f.version++
		writeJSON(w, http.StatusOK, map[string]any{"profile_id": "pid-123", "version": f.version})
	}
}

func (f *profileRails) linkCode(w http.ResponseWriter, r *http.Request) {
	if !f.note(r) {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.pmu.Lock()
	status := f.linkStatus
	f.pmu.Unlock()
	if status != 0 {
		http.Error(w, "no", status)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"code": "482913", "expires_at": "2026-11-09T10:15:00Z"})
}

type profileEnv struct {
	r     *Registry
	rails *profileRails
	sent  chan protocol.ProfileRequest
}

func newProfileEnv(t *testing.T, startSession bool) *profileEnv {
	t.Helper()
	rails := newProfileRails(t)
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "store.db"),
		ServerURL:       rails.url,
		ActiveAccountID: "acct",
		Accounts: []config.AccountConfig{{
			ID: "acct", Name: "Lobby", KioskID: "k1", DeviceToken: "device-token",
		}},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)

	r := NewRegistry(cfg)
	r.padDSUAddrs = [2]string{"127.0.0.1:0", "127.0.0.1:0"}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.BindPadLifecycle(ctx)
	t.Cleanup(func() { _ = r.Close() })

	env := &profileEnv{r: r, rails: rails, sent: make(chan protocol.ProfileRequest, 8)}
	r.profileSendHook = func(playerID string, msg any) error {
		req, ok := msg.(protocol.ProfileRequest)
		if !ok || playerID != "p1" {
			t.Errorf("sent %T to %s", msg, playerID)
			return nil
		}
		env.sent <- req
		return nil
	}
	if startSession {
		if _, err := dispatch(t, r, "pad.session.start", `{}`); err != nil {
			t.Fatal(err)
		}
		if _, err := dispatch(t, r, "pad.app.focus", `{"app_id":"`+profileApp+`","name":"Party Quiz","active":true}`); err != nil {
			t.Fatal(err)
		}
	}
	return env
}

func (e *profileEnv) request(t *testing.T) chan map[string]bool {
	t.Helper()
	out := make(chan map[string]bool, 1)
	go func() {
		res, err := dispatch(t, e.r, "pad.profile.request", `{"app_id":"`+profileApp+`","app_name":"Party Quiz","player_id":"p1"}`)
		if err != nil {
			t.Errorf("request: %v", err)
			out <- nil
			return
		}
		out <- res.(map[string]bool)
	}()
	return out
}

func (e *profileEnv) awaitSent(t *testing.T) protocol.ProfileRequest {
	t.Helper()
	select {
	case req := <-e.sent:
		return req
	case <-time.After(3 * time.Second):
		t.Fatal("no profile_request was sent")
		return protocol.ProfileRequest{}
	}
}

func awaitResult(t *testing.T, ch chan map[string]bool) map[string]bool {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(3 * time.Second):
		t.Fatal("pad.profile.request did not resolve")
		return nil
	}
}

func TestProfileRequestGranted(t *testing.T) {
	e := newProfileEnv(t, true)
	res := e.request(t)
	req := e.awaitSent(t)
	if req.T != "profile_request" || req.AppID != profileApp || req.AppName != "Party Quiz" {
		t.Fatalf("request %+v", req)
	}
	// The phone stores the grant with Rails, then tells the kiosk.
	e.rails.set(func() { e.rails.granted = true })
	e.r.profileReplied("p1", protocol.ProfileReply{T: "profile_reply", AppID: profileApp, Granted: true})
	if got := awaitResult(t, res); got == nil || !got["granted"] {
		t.Fatalf("result %v", got)
	}
}

func TestProfileRequestDeclined(t *testing.T) {
	e := newProfileEnv(t, true)
	res := e.request(t)
	e.awaitSent(t)
	e.r.profileReplied("p1", protocol.ProfileReply{T: "profile_reply", AppID: profileApp, Granted: false})
	if got := awaitResult(t, res); got == nil || got["granted"] {
		t.Fatalf("result %v", got)
	}
}

func TestProfileReplyWithoutARealGrantIsNotGranted(t *testing.T) {
	e := newProfileEnv(t, true)
	res := e.request(t)
	e.awaitSent(t)
	// A hostile or buggy phone says yes; Rails never recorded a grant.
	e.r.profileReplied("p1", protocol.ProfileReply{T: "profile_reply", AppID: profileApp, Granted: true})
	if got := awaitResult(t, res); got == nil || got["granted"] {
		t.Fatalf("result %v", got)
	}
}

func TestProfileReplyForAnotherAppOrPlayerIsIgnored(t *testing.T) {
	e := newProfileEnv(t, true)
	e.r.profiles.timeout = 200 * time.Millisecond
	res := e.request(t)
	e.awaitSent(t)
	e.rails.set(func() { e.rails.granted = true })
	e.r.profileReplied("p1", protocol.ProfileReply{T: "profile_reply", AppID: "dev.other.app", Granted: true})
	e.r.profileReplied("p2", protocol.ProfileReply{T: "profile_reply", AppID: profileApp, Granted: true})
	// Neither woke the wait, so it times out (even though Rails has a grant).
	if got := awaitResult(t, res); got == nil || got["granted"] {
		t.Fatalf("result %v", got)
	}
}

func TestProfileRequestTimesOut(t *testing.T) {
	e := newProfileEnv(t, true)
	e.r.profiles.timeout = 50 * time.Millisecond
	res := e.request(t)
	e.awaitSent(t)
	if got := awaitResult(t, res); got == nil || got["granted"] {
		t.Fatalf("result %v", got)
	}
	// The wait is gone: asking again sends a new request.
	res = e.request(t)
	e.awaitSent(t)
	awaitResult(t, res)
}

func TestProfileRequestsShareTheWait(t *testing.T) {
	e := newProfileEnv(t, true)
	first := e.request(t)
	e.awaitSent(t)
	second := e.request(t)
	select {
	case <-e.sent:
		t.Fatal("a second request for the same player and app asked the phone again")
	case <-time.After(150 * time.Millisecond):
	}
	e.rails.set(func() { e.rails.granted = true })
	e.r.profileReplied("p1", protocol.ProfileReply{T: "profile_reply", AppID: profileApp, Granted: true})
	for _, ch := range []chan map[string]bool{first, second} {
		if got := awaitResult(t, ch); got == nil || !got["granted"] {
			t.Fatalf("result %v", got)
		}
	}
}

func TestProfileRequestAlreadyGrantedSkipsThePhone(t *testing.T) {
	e := newProfileEnv(t, true)
	e.rails.set(func() { e.rails.granted = true })
	res := e.request(t)
	if got := awaitResult(t, res); got == nil || !got["granted"] {
		t.Fatalf("result %v", got)
	}
	select {
	case <-e.sent:
		t.Fatal("the phone was asked for a grant it already gave")
	default:
	}
}

func TestProfileCommandsMapErrors(t *testing.T) {
	e := newProfileEnv(t, true)
	get := `{"app_id":"` + profileApp + `","player_id":"p1"}`
	set := func(data string, version int) (any, error) {
		return dispatch(t, e.r, "pad.profile.set", `{"app_id":"`+profileApp+`","player_id":"p1","data":`+data+`,"version":`+itoa(version)+`}`)
	}

	e.rails.set(func() { e.rails.linked = false })
	if _, err := dispatch(t, e.r, "pad.profile.get", get); err == nil || !strings.HasPrefix(err.Error(), "not_linked") {
		t.Fatalf("not linked: %v", err)
	}
	if _, err := dispatch(t, e.r, "pad.profile.request", get); err == nil || !strings.HasPrefix(err.Error(), "not_linked") {
		t.Fatalf("request not linked: %v", err)
	}
	e.rails.set(func() { e.rails.linked = true })
	if _, err := dispatch(t, e.r, "pad.profile.get", get); err == nil || !strings.HasPrefix(err.Error(), "not_granted") {
		t.Fatalf("not granted get: %v", err)
	}
	if _, err := set(`{"best":1}`, 0); err == nil || !strings.HasPrefix(err.Error(), "not_granted") {
		t.Fatalf("not granted set: %v", err)
	}

	e.rails.set(func() { e.rails.granted = true })
	res, err := dispatch(t, e.r, "pad.profile.get", get)
	if err != nil {
		t.Fatal(err)
	}
	if m := res.(map[string]any); m["profile_id"] != "pid-123" || m["version"] != 0 || string(m["data"].(json.RawMessage)) != "null" {
		t.Fatalf("get %v", m)
	}
	res, err = set(`{"best":42}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]int)["version"] != 1 {
		t.Fatalf("set %v", res)
	}
	// A stale version: the error carries the current one.
	if _, err := set(`{"best":43}`, 0); err == nil || err.Error() != "version_conflict: current version 1" {
		t.Fatalf("conflict: %v", err)
	}
	e.rails.set(func() { e.rails.putStatus = http.StatusRequestEntityTooLarge })
	if _, err := set(`{"big":true}`, 1); err == nil || !strings.HasPrefix(err.Error(), "too_large") {
		t.Fatalf("too large: %v", err)
	}
	if _, err := dispatch(t, e.r, "pad.profile.set", `{"app_id":"`+profileApp+`","player_id":"p1","version":1}`); err == nil {
		t.Fatal("set without data")
	}
}

func TestProfileCommandsNeedTheCallingApp(t *testing.T) {
	e := newProfileEnv(t, true)
	e.rails.set(func() { e.rails.granted = true })
	// An app that does not hold the controllers cannot read or write any
	// profile, whatever app id it names.
	for _, method := range []string{"pad.profile.get", "pad.profile.set", "pad.profile.request"} {
		if _, err := dispatch(t, e.r, method, `{"app_id":"dev.other.app","player_id":"p1","data":{},"version":0}`); err == nil {
			t.Fatalf("%s accepted a background app", method)
		}
	}
	if _, err := dispatch(t, e.r, "pad.profile.get", `{"app_id":"`+profileApp+`"}`); err == nil {
		t.Fatal("get without a player")
	}
	if e.rails.gets() != 0 {
		t.Fatal("Rails was asked for a profile that was refused locally")
	}
}

func TestProfileCommandsNeedASession(t *testing.T) {
	e := newProfileEnv(t, false)
	if _, err := dispatch(t, e.r, "pad.app.focus", `{"app_id":"`+profileApp+`","active":true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch(t, e.r, "pad.profile.get", `{"app_id":"`+profileApp+`","player_id":"p1"}`); err == nil || !strings.Contains(err.Error(), "no active play session") {
		t.Fatalf("get without a session: %v", err)
	}
}

func TestAccountLinkCode(t *testing.T) {
	// No play session needed: Settings asks before anyone has joined.
	e := newProfileEnv(t, false)
	res, err := dispatch(t, e.r, "pad.account.link_code", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	code := res.(interface{})
	raw, _ := json.Marshal(code)
	if string(raw) != `{"code":"482913","expires_at":"2026-11-09T10:15:00Z"}` {
		t.Fatalf("link code %s", raw)
	}
	if !e.rails.saw("POST /api/kiosk/phone_link_codes") {
		t.Fatalf("calls %v", e.rails.calls)
	}
	e.rails.set(func() { e.rails.linkStatus = http.StatusUnauthorized })
	if _, err := dispatch(t, e.r, "pad.account.link_code", `{}`); err == nil {
		t.Fatal("Rails refusal was not an error")
	}
}

func TestAccountLinkCodeNeedsTheAdminPIN(t *testing.T) {
	e := newProfileEnv(t, false)
	pin, err := hashPIN("123456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.mutateConfig(func(cfg *config.Config) error {
		cfg.AdminPIN = pin
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.r.Authorize("pad.account.link_code", json.RawMessage(`{}`)); err == nil {
		t.Fatal("link code without the PIN token")
	}
	if _, err := e.r.DispatchAuthenticated("pad.account.link_code", json.RawMessage(`{}`)); err == nil {
		t.Fatal("link code dispatched without the PIN token")
	}
	if e.rails.saw("POST /api/kiosk/phone_link_codes") {
		t.Fatal("a code was made without the PIN")
	}
	verified, err := e.r.Dispatch("auth.verifyPin", json.RawMessage(`{"pin":"123456"}`))
	if err != nil {
		t.Fatal(err)
	}
	token := verified.(authResult).Token
	params := json.RawMessage(`{"_auth_token":"` + token + `"}`)
	if _, err := e.r.DispatchAuthenticated("pad.account.link_code", params); err != nil {
		t.Fatalf("with the token: %v", err)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
