package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/apps"
	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/session"
	"kiosk/controller/internal/video"
)

// Pad video surfaces (contract C3): `pad.surfaces.*` and
// the `pad.surfaces` event. A surface is a window of the app's native
// session shown on one player's phone. The overlay checks the `surfaces`
// permission and attaches app_id.

// surfaceErrPrefix starts every error message the overlay maps to a code.
const surfaceErrPrefix = "YUI_SURFACES: "

// Runtime asset metadata that marks the capture/encode helper.
const videoHelperRuntime = "ffmpeg"

func SurfacesCommands() []Command {
	return []Command{
		padSurfacesAvailableCommand{},
		padSurfacesListCommand{},
		padSurfacesShowCommand{},
	}
}

// surfaceError carries a code the overlay passes to the app.
type surfaceError struct{ code string }

func (e *surfaceError) Error() string { return surfaceErrPrefix + e.code }

func surfaceErr(code string) error { return &surfaceError{code: code} }

// mapSurfaceError turns a video service error into `YUI_SURFACES: <code>`.
func mapSurfaceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, video.ErrNotReady):
		return surfaceErr("not_ready")
	case errors.Is(err, video.ErrNoVideo):
		return surfaceErr("no_video")
	case errors.Is(err, video.ErrNoVideoLink):
		return surfaceErr("no_video_link")
	case errors.Is(err, video.ErrUnknownSurface):
		return surfaceErr("unknown_surface")
	}
	return err
}

// videoState is the video service and what the asset syncer needs to know
// about its helper. Guarded by videoMu; never held while calling the service.
type videoState struct {
	svc *video.Service
	// helperAsset is the runtime asset the encoder was last taken from.
	helperAsset string
	// sessions counts native sessions with surfaces that have not ended.
	sessions int
}

// Video returns the video service, creating it on first use.
func (r *Registry) Video() *video.Service {
	r.videoMu.Lock()
	defer r.videoMu.Unlock()
	if r.video.svc != nil {
		return r.video.svc
	}
	r.video.svc = video.New(video.Config{
		Helper: r.videoHelper,
		Writer: func(playerID string) (video.RTPWriter, bool) {
			mgr := r.padManager()
			if mgr == nil {
				return nil, false
			}
			w, ok := mgr.VideoWriter(playerID)
			if !ok || w == nil {
				return nil, false
			}
			return w, true
		},
		CanShow: func(playerID string) bool {
			mgr := r.padManager()
			if mgr == nil {
				return false
			}
			caps, ok := mgr.Caps(playerID)
			return ok && caps.Video
		},
		Relayed: func(playerID string) bool {
			mgr := r.padManager()
			if mgr == nil {
				return false
			}
			relayed, ok := mgr.Relayed(playerID)
			return ok && relayed
		},
		Notify: func(playerID, surfaceID string, on bool, width, height int) {
			mgr := r.padManager()
			if mgr == nil {
				return
			}
			msg := protocol.Surface{T: "surface", ID: surfaceID, State: "off"}
			if on {
				msg.State, msg.Width, msg.Height = "on", width, height
			}
			if err := mgr.SendControl(playerID, msg); err != nil {
				log.Printf("surfaces: tell %s: %v", playerID, err)
			}
		},
		OnStatus: func(appID string, surfaces []video.Status) {
			if surfaces == nil {
				surfaces = []video.Status{}
			}
			r.Emit("pad.surfaces", map[string]any{"app_id": appID, "surfaces": surfaces})
		},
	})
	return r.video.svc
}

func (r *Registry) videoIfStarted() *video.Service {
	r.videoMu.Lock()
	defer r.videoMu.Unlock()
	return r.video.svc
}

func (r *Registry) closeVideo() {
	if svc := r.videoIfStarted(); svc != nil {
		svc.Close()
	}
}

// padManager returns the current play session's manager, or nil.
func (r *Registry) padManager() *session.Manager {
	r.padMu.Lock()
	defer r.padMu.Unlock()
	return r.padMgr
}

// videoAssetInUse reports whether the encoder asset is in use: a native
// session with surfaces is running and took the encoder from this asset.
func (r *Registry) videoAssetInUse(id string) bool {
	r.videoMu.Lock()
	defer r.videoMu.Unlock()
	return r.video.sessions > 0 && r.video.helperAsset != "" && r.video.helperAsset == id
}

// videoHelper finds the encoder: the newest ready runtime asset whose
// metadata names it, extracted through the native runtime store.
func (r *Registry) videoHelper() (string, error) {
	syncer := r.AssetSyncer()
	items, err := syncer.List("runtime")
	if err != nil {
		return "", err
	}
	type candidate struct{ id, version string }
	var found []candidate
	for _, item := range items {
		if item.State != "ready" || len(item.Metadata) == 0 {
			continue
		}
		var meta struct {
			Runtime string `json:"runtime"`
			Version string `json:"version"`
		}
		if json.Unmarshal(item.Metadata, &meta) != nil || meta.Runtime != videoHelperRuntime {
			continue
		}
		found = append(found, candidate{item.ID, meta.Version})
	}
	if len(found) == 0 {
		return "", errors.New("no encoder runtime on this kiosk")
	}
	sort.SliceStable(found, func(i, j int) bool {
		if c := compareVersions(found[i].version, found[j].version); c != 0 {
			return c > 0
		}
		return found[i].id > found[j].id
	})
	info, ok := registryAssets{r: r}.Asset(found[0].id)
	if !ok {
		return "", errors.New("encoder runtime is not ready")
	}
	dir, err := r.Native().Runtimes().Dir(info)
	if err != nil {
		return "", err
	}
	for _, rel := range []string{"ffmpeg.exe", "bin/ffmpeg.exe", "ffmpeg", "bin/ffmpeg"} {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			r.videoMu.Lock()
			r.video.helperAsset = info.ID
			r.videoMu.Unlock()
			return path, nil
		}
	}
	return "", errors.New("encoder not found in the runtime")
}

// compareVersions orders dotted versions numerically where it can ("8.10" >
// "8.9") and as text otherwise.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		x, y := "0", "0"
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn > yn {
					return 1
				}
				return -1
			}
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return 0
}

// surfaceSpecs converts a session's declared surfaces for the video service.
func surfaceSpecs(list []native.SurfaceSpec) []video.SurfaceSpec {
	out := make([]video.SurfaceSpec, 0, len(list))
	for _, s := range list {
		out = append(out, video.SurfaceSpec{ID: s.ID, Title: s.Title, Hidden: s.Hidden, Width: s.Width, Height: s.Height})
	}
	return out
}

// nativeSurfaceObserver registers a native session's surfaces with the video
// service while the session runs.
type nativeSurfaceObserver struct{ r *Registry }

func (o nativeSurfaceObserver) SessionStarted(info native.Info) {
	if len(info.Surfaces) == 0 {
		return
	}
	o.r.videoMu.Lock()
	o.r.video.sessions++
	o.r.videoMu.Unlock()
	o.r.Video().SessionStarted(info.AppID, info.PID, surfaceSpecs(info.Surfaces))
}

func (o nativeSurfaceObserver) SessionEnded(info native.Info, _ native.Status) {
	if len(info.Surfaces) == 0 {
		return
	}
	if svc := o.r.videoIfStarted(); svc != nil {
		svc.SessionEnded(info.AppID)
	}
	o.r.videoMu.Lock()
	if o.r.video.sessions > 0 {
		o.r.video.sessions--
	}
	o.r.videoMu.Unlock()
}

type padSurfacesAvailableCommand struct{}

func (padSurfacesAvailableCommand) Name() string { return "pad.surfaces.available" }
func (padSurfacesAvailableCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return r.Video().Available(), nil
}

type padSurfacesListCommand struct{}

func (padSurfacesListCommand) Name() string { return "pad.surfaces.list" }
func (padSurfacesListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID string `json:"app_id"`
	}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	svc := r.videoIfStarted()
	if svc == nil {
		return []video.Status{}, nil
	}
	list := svc.List(p.AppID)
	if list == nil {
		list = []video.Status{}
	}
	return list, nil
}

type padSurfacesShowCommand struct{}

func (padSurfacesShowCommand) Name() string { return "pad.surfaces.show" }
func (padSurfacesShowCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		AppID    string  `json:"app_id"`
		Surface  string  `json:"surface"`
		PlayerID *string `json:"player_id"`
	}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	ctl := r.padControllersIfStarted()
	if ctl == nil || !ctl.Owns(p.AppID) {
		return nil, apps.ErrNotOwner
	}
	if p.Surface == "" {
		return nil, surfaceErr("unknown_surface")
	}
	playerID := ""
	if p.PlayerID != nil && *p.PlayerID != "" {
		playerID = *p.PlayerID
		if !r.padHasPlayer(playerID) {
			return nil, surfaceErr("unknown_player")
		}
	}
	st, err := r.Video().Show(p.AppID, p.Surface, playerID)
	if err != nil {
		return nil, mapSurfaceError(err)
	}
	return st, nil
}

// padHasPlayer reports whether the play session's roster has the player.
func (r *Registry) padHasPlayer(id string) bool {
	for _, pl := range r.padSnapshot().Players {
		if pl.ID == id {
			return true
		}
	}
	return false
}
