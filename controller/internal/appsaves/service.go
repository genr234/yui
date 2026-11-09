package appsaves

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxKeys is the number of blobs one app may keep per scope.
const MaxKeys = 64

var (
	keyPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,79}$`)
	appIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,126}[a-z0-9_-]$`)
	profilePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	scopeDirRe     = regexp.MustCompile(`^(kiosk|player-[A-Za-z0-9_-]{1,64})$`)
)

// Scope says whose blobs a call is about. PlayerID is "" for the kiosk's
// account. For a player, ProfileID names the player's account locally (the
// local copy lives in player-<ProfileID>), PlayerID goes to Rails.
type Scope struct {
	AppID     string
	PlayerID  string
	ProfileID string
}

func (sc Scope) dirName() string {
	if sc.PlayerID == "" {
		return "kiosk"
	}
	return "player-" + sc.ProfileID
}

func (sc Scope) valid() error {
	if !appIDPattern.MatchString(sc.AppID) {
		return errors.New("invalid app id")
	}
	if sc.PlayerID != "" && !profilePattern.MatchString(sc.ProfileID) {
		return errors.New("invalid profile id")
	}
	return nil
}

// Result is what put reports.
type Result struct {
	Key       string `json:"key"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updated_at"`
	Queued    bool   `json:"queued,omitempty"`
}

// Blob is what get returns.
type Blob struct {
	Key       string
	Data      []byte
	UpdatedAt string
}

type localMeta struct {
	UpdatedAt string `json:"updated_at"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Queued    bool   `json:"queued,omitempty"`
	PlayerID  string `json:"player_id,omitempty"`
}

// Service is the local copy and upload queue in front of a Remote.
type Service struct {
	// Root is <ConfigDir>/app-saves.
	Root   string
	Remote Remote
	// Account returns the active account's id, or "" when there is none.
	Account func() string
	// Now replaces time.Now in tests.
	Now func() time.Time

	mu sync.Mutex // local files only; never held across a Remote call
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) account() (string, error) {
	acct := s.Account()
	if acct == "" || !profilePattern.MatchString(acct) {
		return "", ErrOffline
	}
	return acct, nil
}

func (s *Service) dir(acct string, sc Scope) string {
	return filepath.Join(s.Root, acct, sc.AppID, sc.dirName())
}

func parseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// refusal reports whether err is one of Rails' definite answers (as opposed
// to "could not reach it").
func refusal(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code != ErrOffline.Code
}

// remoteError turns a Remote failure that must be reported into the error
// the caller sees. unreachable is true when Rails could not be reached, so the
// caller may fall back to the local copy.
func remoteError(sc Scope, err error) (out error, unreachable bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, ErrNoAccount):
		return ErrOffline, false
	case errors.Is(err, ErrNewer):
		return &Error{"newer_save_exists", "the account holds a newer save"}, false
	case errors.Is(err, ErrNotFound):
		if sc.PlayerID != "" {
			return &Error{"not_linked", "this player is not in the kiosk's play session"}, false
		}
		return err, false
	case refusal(err):
		return err, false
	}
	return err, true
}

// Put stores data under key. When Rails cannot be reached the blob is kept
// locally, Queued is true, and Flush uploads it later.
func (s *Service) Put(ctx context.Context, sc Scope, key string, data []byte) (Result, error) {
	if err := sc.valid(); err != nil {
		return Result{}, err
	}
	if !keyPattern.MatchString(key) {
		return Result{}, ErrInvalidKey
	}
	if len(data) > MaxBlobBytes {
		return Result{}, ErrTooLarge
	}
	acct, err := s.account()
	if err != nil {
		return Result{}, err
	}
	now := s.now()
	meta, err := s.Remote.Put(ctx, sc.AppID, key, sc.PlayerID, data, now)
	if errors.Is(err, ErrNewer) {
		// The account's copy is newer than this kiosk's clock says "now" is:
		// the user just saved, so let the server stamp it.
		meta, err = s.Remote.Put(ctx, sc.AppID, key, sc.PlayerID, data, time.Time{})
	}
	if err == nil {
		sum := sha256.Sum256(data)
		updated := meta.UpdatedAt
		if updated == "" {
			updated = now.Format(TimeFormat)
		}
		if err := s.writeLocal(acct, sc, key, data, localMeta{UpdatedAt: updated, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}); err != nil {
			return Result{}, err
		}
		return Result{Key: key, Size: int64(len(data)), UpdatedAt: updated}, nil
	}
	out, unreachable := remoteError(sc, err)
	if !unreachable {
		return Result{}, out
	}

	// Offline: keep it and upload later.
	if !s.hasLocal(acct, sc, key) && s.countLocal(acct, sc) >= MaxKeys {
		return Result{}, ErrTooMany
	}
	sum := sha256.Sum256(data)
	updated := now.Format(TimeFormat)
	if err := s.writeLocal(acct, sc, key, data, localMeta{
		UpdatedAt: updated, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), Queued: true, PlayerID: sc.PlayerID,
	}); err != nil {
		return Result{}, err
	}
	return Result{Key: key, Size: int64(len(data)), UpdatedAt: updated, Queued: true}, nil
}

// Get returns the blob, or nil when there is none.
func (s *Service) Get(ctx context.Context, sc Scope, key string) (*Blob, error) {
	if err := sc.valid(); err != nil {
		return nil, err
	}
	if !keyPattern.MatchString(key) {
		return nil, ErrInvalidKey
	}
	acct, err := s.account()
	if err != nil {
		return nil, err
	}
	local, haveLocal := s.readLocal(acct, sc, key)
	data, meta, found, err := s.Remote.Get(ctx, sc.AppID, key, sc.PlayerID)
	if err != nil {
		out, unreachable := remoteError(sc, err)
		if !unreachable {
			return nil, out
		}
		if haveLocal {
			return local, nil
		}
		return nil, nil
	}
	if !found {
		if haveLocal {
			if info, ok := s.readMeta(acct, sc, key); ok && info.Queued {
				return local, nil
			}
			s.removeLocal(acct, sc, key) // deleted elsewhere
		}
		return nil, nil
	}
	if haveLocal {
		if info, ok := s.readMeta(acct, sc, key); ok && info.Queued && !parseTime(info.UpdatedAt).Before(parseTime(meta.UpdatedAt)) {
			return local, nil // our newer write is waiting to upload
		}
	}
	if err := s.writeLocal(acct, sc, key, data, localMeta{UpdatedAt: meta.UpdatedAt, SHA256: meta.SHA256, Size: meta.Size}); err != nil {
		return nil, err
	}
	return &Blob{Key: key, Data: data, UpdatedAt: meta.UpdatedAt}, nil
}

// List merges Rails' list with blobs waiting to upload; with Rails out of
// reach it lists every local copy.
func (s *Service) List(ctx context.Context, sc Scope) ([]Meta, error) {
	if err := sc.valid(); err != nil {
		return nil, err
	}
	acct, err := s.account()
	if err != nil {
		return nil, err
	}
	local := s.listLocal(acct, sc)
	remote, err := s.Remote.List(ctx, sc.AppID, sc.PlayerID)
	reachable := err == nil
	if err != nil {
		out, unreachable := remoteError(sc, err)
		if !unreachable {
			return nil, out
		}
	}
	merged := map[string]Meta{}
	for _, m := range remote {
		merged[m.Key] = m
	}
	for key, info := range local {
		switch {
		case !reachable, info.Queued:
			if prev, ok := merged[key]; ok && parseTime(prev.UpdatedAt).After(parseTime(info.UpdatedAt)) {
				continue
			}
			merged[key] = Meta{Key: key, Size: info.Size, SHA256: info.SHA256, UpdatedAt: info.UpdatedAt}
		}
	}
	out := make([]Meta, 0, len(merged))
	for _, m := range merged {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Delete removes the blob from Rails and the local copy. It needs Rails: a
// delete that only removed the local copy would come back on the next get.
func (s *Service) Delete(ctx context.Context, sc Scope, key string) error {
	if err := sc.valid(); err != nil {
		return err
	}
	if !keyPattern.MatchString(key) {
		return ErrInvalidKey
	}
	acct, err := s.account()
	if err != nil {
		return err
	}
	if err := s.Remote.Delete(ctx, sc.AppID, key, sc.PlayerID); err != nil {
		out, unreachable := remoteError(sc, err)
		if unreachable {
			return ErrOffline
		}
		return out
	}
	s.removeLocal(acct, sc, key)
	return nil
}

// Flush uploads queued blobs of the active account. It stops at the first
// failure that means Rails is out of reach and reports how many it sent.
func (s *Service) Flush(ctx context.Context) (int, error) {
	acct, err := s.account()
	if err != nil {
		return 0, nil
	}
	sent := 0
	apps, _ := os.ReadDir(filepath.Join(s.Root, acct))
	for _, app := range apps {
		scopes, _ := os.ReadDir(filepath.Join(s.Root, acct, app.Name()))
		for _, scopeEntry := range scopes {
			if !scopeEntry.IsDir() || !scopeDirRe.MatchString(scopeEntry.Name()) {
				continue
			}
			sc := Scope{AppID: app.Name()}
			if strings.HasPrefix(scopeEntry.Name(), "player-") {
				sc.ProfileID = strings.TrimPrefix(scopeEntry.Name(), "player-")
			}
			if sc.ProfileID == "" && scopeEntry.Name() != "kiosk" {
				continue
			}
			for key, info := range s.listLocal(acct, sc) {
				if !info.Queued {
					continue
				}
				if ctx.Err() != nil {
					return sent, ctx.Err()
				}
				sc.PlayerID = info.PlayerID
				if (info.PlayerID == "") != (scopeEntry.Name() == "kiosk") {
					continue
				}
				ok, err := s.flushOne(ctx, acct, sc, key, info)
				if err != nil {
					return sent, err
				}
				if ok {
					sent++
				}
			}
		}
	}
	return sent, nil
}

func (s *Service) flushOne(ctx context.Context, acct string, sc Scope, key string, info localMeta) (bool, error) {
	data, err := os.ReadFile(filepath.Join(s.dir(acct, sc), key))
	if err != nil {
		s.removeLocal(acct, sc, key)
		return false, nil
	}
	meta, err := s.Remote.Put(ctx, sc.AppID, key, sc.PlayerID, data, parseTime(info.UpdatedAt))
	switch {
	case err == nil:
		s.mu.Lock()
		defer s.mu.Unlock()
		if cur, ok := s.readMetaLocked(acct, sc, key); ok && cur.UpdatedAt == info.UpdatedAt {
			cur.Queued = false
			if meta.UpdatedAt != "" {
				cur.UpdatedAt = meta.UpdatedAt
			}
			_ = s.writeMetaLocked(acct, sc, key, cur)
		}
		return true, nil
	case errors.Is(err, ErrNewer), errors.Is(err, ErrTooLarge), errors.Is(err, ErrTooMany), errors.Is(err, ErrInvalidKey):
		// Rails will never take this one; the account's copy wins.
		s.removeLocal(acct, sc, key)
		return false, nil
	case errors.Is(err, ErrNotLinked), errors.Is(err, ErrNotGranted), errors.Is(err, ErrNotFound):
		// The player is not here right now; try again later.
		return false, nil
	case errors.Is(err, ErrNoAccount):
		return false, ErrOffline
	}
	return false, err
}

// --- local files ---

func (s *Service) metaPath(acct string, sc Scope, key string) string {
	return filepath.Join(s.dir(acct, sc), ".meta", key+".json")
}

func (s *Service) writeLocal(acct string, sc Scope, key string, data []byte, meta localMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.dir(acct, sc)
	if err := os.MkdirAll(filepath.Join(dir, ".meta"), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(dir, key), data); err != nil {
		return err
	}
	return s.writeMetaLocked(acct, sc, key, meta)
}

func (s *Service) writeMetaLocked(acct string, sc Scope, key string, meta localMeta) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return writeAtomic(s.metaPath(acct, sc, key), raw)
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("write save: %w", err)
	}
	return nil
}

func (s *Service) readMeta(acct string, sc Scope, key string) (localMeta, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readMetaLocked(acct, sc, key)
}

func (s *Service) readMetaLocked(acct string, sc Scope, key string) (localMeta, bool) {
	raw, err := os.ReadFile(s.metaPath(acct, sc, key))
	if err != nil {
		return localMeta{}, false
	}
	var m localMeta
	if json.Unmarshal(raw, &m) != nil {
		return localMeta{}, false
	}
	return m, true
}

func (s *Service) readLocal(acct string, sc Scope, key string) (*Blob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.readMetaLocked(acct, sc, key)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(s.dir(acct, sc), key))
	if err != nil {
		return nil, false
	}
	return &Blob{Key: key, Data: data, UpdatedAt: meta.UpdatedAt}, true
}

func (s *Service) hasLocal(acct string, sc Scope, key string) bool {
	_, ok := s.readMeta(acct, sc, key)
	return ok
}

func (s *Service) countLocal(acct string, sc Scope) int { return len(s.listLocal(acct, sc)) }

func (s *Service) removeLocal(acct string, sc Scope, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(filepath.Join(s.dir(acct, sc), key))
	_ = os.Remove(s.metaPath(acct, sc, key))
}

func (s *Service) listLocal(acct string, sc Scope) map[string]localMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]localMeta{}
	entries, err := os.ReadDir(filepath.Join(s.dir(acct, sc), ".meta"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		key := strings.TrimSuffix(name, ".json")
		if !keyPattern.MatchString(key) {
			continue
		}
		if meta, ok := s.readMetaLocked(acct, sc, key); ok {
			out[key] = meta
		}
	}
	return out
}
