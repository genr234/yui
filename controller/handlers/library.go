package handlers

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"kiosk/controller/internal/library"
	"kiosk/controller/internal/store"
)

// Library titles (contracts §4, `library.*`). Titles live in the active
// account's store, collection `library`. They are kiosk-local for now: the
// server's sync allow-list does not include `library`, so no outbox entry is
// recorded. Reads and writes run under withStore, so a title is never read
// from one account and written to another.

func LibraryCommands() []Command {
	return []Command{
		LibraryPublishCommand{},
		LibraryPlayedCommand{},
		LibraryListCommand{},
		LibraryRemoveCommand{},
		LibraryRecentCommand{},
	}
}

func (r *Registry) libraryChanged(appID string) {
	r.Emit("library.changed", map[string]string{"app_id": appID})
}

func (r *Registry) withLibrary(fn func(c store.Collection) error) error {
	return r.withStore(func(db *store.DB, _ uint64) error {
		return fn(db.Collection(library.CollectionName))
	})
}

type LibraryPublishCommand struct{}

func (LibraryPublishCommand) Name() string { return "library.publish" }
func (LibraryPublishCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID   string          `json:"app_id"`
		Titles  []library.Input `json:"titles"`
		Replace bool            `json:"replace"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	var out []library.Title
	err := r.withLibrary(func(c store.Collection) error {
		var err error
		out, err = library.Publish(c, p.AppID, p.Titles, p.Replace, time.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	r.libraryChanged(p.AppID)
	return out, nil
}

type LibraryPlayedCommand struct{}

func (LibraryPlayedCommand) Name() string { return "library.played" }
func (LibraryPlayedCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID    string          `json:"app_id"`
		ID       string          `json:"id"`
		Progress *float64        `json:"progress"`
		Resume   json.RawMessage `json:"resume"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	var out library.Title
	err := r.withLibrary(func(c store.Collection) error {
		var err error
		out, err = library.Played(c, p.AppID, p.ID, library.PlayedPatch{Progress: p.Progress, Resume: p.Resume}, time.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	r.libraryChanged(p.AppID)
	return out, nil
}

type LibraryListCommand struct{}

func (LibraryListCommand) Name() string { return "library.list" }
func (LibraryListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID string `json:"app_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	var out []library.Title
	err := r.withLibrary(func(c store.Collection) error {
		var err error
		out, err = library.List(c, p.AppID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type LibraryRemoveCommand struct{}

func (LibraryRemoveCommand) Name() string { return "library.remove" }
func (LibraryRemoveCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID string   `json:"app_id"`
		IDs   []string `json:"ids"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	err := r.withLibrary(func(c store.Collection) error {
		return library.Remove(c, p.AppID, p.IDs)
	})
	if err != nil {
		return nil, err
	}
	r.libraryChanged(p.AppID)
	return nil, nil
}

// recentTitle is a library.recent entry: the stored title plus art_url, the
// art resolved to something the overlay can show (a data URI as is, an
// asset id through that asset's metadata.cover_url).
type recentTitle struct {
	library.Title
	ArtURL string `json:"art_url,omitempty"`
}

type LibraryRecentCommand struct{}

func (LibraryRecentCommand) Name() string { return "library.recent" }
func (LibraryRecentCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Limit int `json:"limit"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	var titles []library.Title
	err := r.withLibrary(func(c store.Collection) error {
		var err error
		titles, err = library.Recent(c, p.Limit)
		return err
	})
	if err != nil {
		return nil, err
	}
	covers := map[string]string{}
	for _, t := range titles {
		if t.ArtAsset {
			covers = r.assetCovers()
			break
		}
	}
	out := make([]recentTitle, 0, len(titles))
	for _, t := range titles {
		entry := recentTitle{Title: t}
		if t.ArtAsset {
			entry.ArtURL = covers[t.Art]
		} else {
			entry.ArtURL = t.Art
		}
		out = append(out, entry)
	}
	return out, nil
}

// assetCovers maps asset ids to their public http(s) metadata.cover_url.
func (r *Registry) assetCovers() map[string]string {
	covers := map[string]string{}
	items, err := r.AssetSyncer().List("")
	if err != nil {
		return covers
	}
	for _, item := range items {
		var meta struct {
			CoverURL string `json:"cover_url"`
		}
		if len(item.Metadata) == 0 || json.Unmarshal(item.Metadata, &meta) != nil || meta.CoverURL == "" {
			continue
		}
		u, err := url.Parse(meta.CoverURL)
		if err != nil || (!strings.EqualFold(u.Scheme, "https") && !strings.EqualFold(u.Scheme, "http")) {
			continue
		}
		covers[item.ID] = meta.CoverURL
	}
	return covers
}
