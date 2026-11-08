// Package library keeps the titles apps publish for the shell's "Jump back
// in" row (ctx.library, contracts §4). A title is something inside an app a
// person can resume: a game, a playlist, a document. Titles are stored per
// account in the `library` collection, keyed "<app_id>/<title_id>".
package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"kiosk/controller/internal/store"
)

// CollectionName is the account-store collection holding titles.
const CollectionName = "library"

// Limits on what an app may publish.
const (
	MaxTitlesPerApp  = 500
	MaxPublishBatch  = 200
	MaxTitleLength   = 200
	MaxArtBytes      = 256 << 10
	MaxResumeBytes   = 16 << 10
	MaxPlayers       = 16
	DefaultRecent    = 12
	MaxRecent        = 50
	keySeparator     = "/"
	assetArtMaxIDLen = 64
)

var (
	appIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,126}[a-z0-9_-]$`)
	titleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	assetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	dataArtPattern = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp|avif|svg\+xml);base64,[A-Za-z0-9+/]+=*$`)

	// ErrNotPublished means the app has no title with that id.
	ErrNotPublished = errors.New("title is not published")
)

// Title is one stored entry. Art is either a data:image URI or an asset id;
// ArtAsset tells which.
type Title struct {
	AppID       string          `json:"app_id"`
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Art         string          `json:"art,omitempty"`
	ArtAsset    bool            `json:"art_asset,omitempty"`
	Players     int             `json:"players,omitempty"`
	Resume      json.RawMessage `json:"resume,omitempty"`
	Progress    *float64        `json:"progress,omitempty"`
	PublishedAt string          `json:"published_at"`
	PlayedAt    string          `json:"played_at,omitempty"`
}

// Input is what an app publishes for one title.
type Input struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Art      string          `json:"art"`
	Players  int             `json:"players"`
	Resume   json.RawMessage `json:"resume"`
	Progress *float64        `json:"progress"`
}

// PlayedPatch optionally updates a title when it is marked played.
type PlayedPatch struct {
	Progress *float64        `json:"progress"`
	Resume   json.RawMessage `json:"resume"`
}

// Collection is the slice of store.Collection the library uses.
type Collection interface {
	Get(id string) (store.Document, bool, error)
	Put(id string, value any) error
	Delete(id string) error
	List(opts store.ListOptions) ([]store.Document, error)
}

// ValidAppID reports whether id is a well-formed app id.
func ValidAppID(id string) bool { return appIDPattern.MatchString(id) }

func key(appID, id string) string { return appID + keySeparator + id }

func validateProgress(p *float64) error {
	if p == nil {
		return nil
	}
	if math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0 || *p > 1 {
		return errors.New("progress must be between 0 and 1")
	}
	return nil
}

func validateResume(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if len(trimmed) > MaxResumeBytes {
		return nil, fmt.Errorf("resume is larger than %d bytes", MaxResumeBytes)
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, errors.New("resume must be JSON")
	}
	return json.RawMessage(trimmed), nil
}

// validate checks one input and returns its stored form (timestamps unset).
func validate(appID string, in Input) (Title, error) {
	if !titleIDPattern.MatchString(in.ID) {
		return Title{}, fmt.Errorf("invalid title id %q", in.ID)
	}
	name := strings.TrimSpace(in.Title)
	if name == "" || utf8.RuneCountInString(name) > MaxTitleLength || !utf8.ValidString(name) {
		return Title{}, fmt.Errorf("title %q needs a name of 1-%d characters", in.ID, MaxTitleLength)
	}
	t := Title{AppID: appID, ID: in.ID, Title: name}
	switch {
	case in.Art == "":
	case strings.HasPrefix(in.Art, "data:"):
		if len(in.Art) > MaxArtBytes || !dataArtPattern.MatchString(in.Art) {
			return Title{}, fmt.Errorf("title %q: art must be a base64 data:image URI under %d bytes", in.ID, MaxArtBytes)
		}
		t.Art = in.Art
	case assetIDPattern.MatchString(in.Art):
		t.Art = in.Art
		t.ArtAsset = true
	default:
		return Title{}, fmt.Errorf("title %q: art must be a data:image URI or an asset id", in.ID)
	}
	if in.Players < 0 || in.Players > MaxPlayers {
		return Title{}, fmt.Errorf("title %q: players must be 0-%d", in.ID, MaxPlayers)
	}
	t.Players = in.Players
	if err := validateProgress(in.Progress); err != nil {
		return Title{}, fmt.Errorf("title %q: %w", in.ID, err)
	}
	t.Progress = in.Progress
	resume, err := validateResume(in.Resume)
	if err != nil {
		return Title{}, fmt.Errorf("title %q: %w", in.ID, err)
	}
	t.Resume = resume
	return t, nil
}

func decode(doc store.Document) (Title, bool) {
	var t Title
	if err := json.Unmarshal(doc.Value, &t); err != nil || t.AppID == "" || t.ID == "" {
		return Title{}, false
	}
	return t, true
}

// List returns one app's titles in id order.
func List(c Collection, appID string) ([]Title, error) {
	if !ValidAppID(appID) {
		return nil, errors.New("invalid app id")
	}
	docs, err := c.List(store.ListOptions{Prefix: appID + keySeparator})
	if err != nil {
		return nil, err
	}
	out := make([]Title, 0, len(docs))
	for _, doc := range docs {
		if t, ok := decode(doc); ok && t.AppID == appID {
			out = append(out, t)
		}
	}
	return out, nil
}

// Publish upserts an app's titles. A republished title keeps its PlayedAt.
// With replace, the app's titles missing from inputs are removed.
func Publish(c Collection, appID string, inputs []Input, replace bool, now time.Time) ([]Title, error) {
	if !ValidAppID(appID) {
		return nil, errors.New("invalid app id")
	}
	if len(inputs) > MaxPublishBatch {
		return nil, fmt.Errorf("publish at most %d titles at a time", MaxPublishBatch)
	}
	titles := make([]Title, 0, len(inputs))
	seen := map[string]bool{}
	for _, in := range inputs {
		t, err := validate(appID, in)
		if err != nil {
			return nil, err
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("title %q listed twice", t.ID)
		}
		seen[t.ID] = true
		titles = append(titles, t)
	}

	existing, err := List(c, appID)
	if err != nil {
		return nil, err
	}
	previous := make(map[string]Title, len(existing))
	for _, t := range existing {
		previous[t.ID] = t
	}
	count := len(existing)
	for _, t := range titles {
		if _, ok := previous[t.ID]; !ok {
			count++
		}
	}
	if replace {
		count = len(titles)
	}
	if count > MaxTitlesPerApp {
		return nil, fmt.Errorf("an app can publish at most %d titles", MaxTitlesPerApp)
	}

	stamp := now.UTC().Format(time.RFC3339Nano)
	for i := range titles {
		titles[i].PublishedAt = stamp
		if old, ok := previous[titles[i].ID]; ok {
			titles[i].PlayedAt = old.PlayedAt
		}
		if err := c.Put(key(appID, titles[i].ID), titles[i]); err != nil {
			return nil, err
		}
	}
	if replace {
		for id := range previous {
			if !seen[id] {
				if err := c.Delete(key(appID, id)); err != nil {
					return nil, err
				}
			}
		}
	}
	return titles, nil
}

// Played marks a published title as played now, optionally updating its
// progress and resume point.
func Played(c Collection, appID, id string, patch PlayedPatch, now time.Time) (Title, error) {
	if !ValidAppID(appID) {
		return Title{}, errors.New("invalid app id")
	}
	if !titleIDPattern.MatchString(id) {
		return Title{}, fmt.Errorf("invalid title id %q", id)
	}
	doc, ok, err := c.Get(key(appID, id))
	if err != nil {
		return Title{}, err
	}
	t, decoded := decode(doc)
	if !ok || !decoded {
		return Title{}, ErrNotPublished
	}
	if err := validateProgress(patch.Progress); err != nil {
		return Title{}, err
	}
	if patch.Progress != nil {
		t.Progress = patch.Progress
	}
	resume, err := validateResume(patch.Resume)
	if err != nil {
		return Title{}, err
	}
	if resume != nil {
		t.Resume = resume
	}
	t.PlayedAt = now.UTC().Format(time.RFC3339Nano)
	if err := c.Put(key(appID, id), t); err != nil {
		return Title{}, err
	}
	return t, nil
}

// Remove deletes titles of one app. Unknown ids are ignored.
func Remove(c Collection, appID string, ids []string) error {
	if !ValidAppID(appID) {
		return errors.New("invalid app id")
	}
	for _, id := range ids {
		if !titleIDPattern.MatchString(id) {
			return fmt.Errorf("invalid title id %q", id)
		}
		if err := c.Delete(key(appID, id)); err != nil {
			return err
		}
	}
	return nil
}

// Recent returns titles from every app, most recent first (see Rank).
func Recent(c Collection, limit int) ([]Title, error) {
	docs, err := c.List(store.ListOptions{})
	if err != nil {
		return nil, err
	}
	titles := make([]Title, 0, len(docs))
	for _, doc := range docs {
		if t, ok := decode(doc); ok {
			titles = append(titles, t)
		}
	}
	return Rank(titles, limit), nil
}

// Rank orders titles for "Jump back in": played titles by last played,
// newest first, then never-played titles by when they were published,
// newest first. Ties break on app id, then title id, so the order is stable.
// limit <= 0 means DefaultRecent; it is capped at MaxRecent.
func Rank(titles []Title, limit int) []Title {
	if limit <= 0 {
		limit = DefaultRecent
	}
	if limit > MaxRecent {
		limit = MaxRecent
	}
	out := append([]Title(nil), titles...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ap, bp := parseTime(a.PlayedAt), parseTime(b.PlayedAt)
		if !ap.IsZero() || !bp.IsZero() {
			if ap.IsZero() != bp.IsZero() {
				return !ap.IsZero()
			}
			if !ap.Equal(bp) {
				return ap.After(bp)
			}
		} else {
			apub, bpub := parseTime(a.PublishedAt), parseTime(b.PublishedAt)
			if !apub.Equal(bpub) {
				return apub.After(bpub)
			}
		}
		if a.AppID != b.AppID {
			return a.AppID < b.AppID
		}
		return a.ID < b.ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
