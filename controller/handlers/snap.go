package handlers

import (
	"context"
	"errors"
	"image"
	"sync"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/router"
	"kiosk/controller/internal/remotecontrol"
)

// Snap the screen. A phone's "snap" lands in the router, which calls
// Registry.snapGameArea to get the JPEG. Which area is captured:
//
//  1. a native session is running: the desktop (GDI, Windows only) cropped to
//     the rect the controller placed the window at (the whole desktop when
//     the program asked for no placement);
//  2. otherwise the Chrome page clipped to the game rect the overlay publishes
//     on window.__yuiGameRect (read over CDP at snap time, so it is never
//     stale), or the whole page when the overlay publishes none.
const (
	snapMaxSide = 1280
	snapQuality = 80
)

// snapSource holds what the capture choice depends on, so it can be tested.
type snapSource struct {
	nativeActive func() bool
	nativeRect   func() *native.Rect
	desktop      func(rect image.Rectangle, maxSide, quality int) (remotecontrol.Shot, error)
	page         func(ctx context.Context, maxSide, quality int) (remotecontrol.Shot, error)
}

func (s snapSource) capture(ctx context.Context) (router.SnapImage, error) {
	var shot remotecontrol.Shot
	var err error
	if s.nativeActive() {
		rect := image.Rect(-1<<15, -1<<15, 1<<15, 1<<15) // whole desktop; clamped
		if nr := s.nativeRect(); nr != nil {
			rect = image.Rect(nr.X, nr.Y, nr.X+nr.W, nr.Y+nr.H)
		}
		shot, err = s.desktop(rect, snapMaxSide, snapQuality)
		if errors.Is(err, remotecontrol.ErrDesktopUnsupported) {
			return router.SnapImage{}, router.UserError("Not supported here")
		}
	} else {
		shot, err = s.page(ctx, snapMaxSide, snapQuality)
	}
	if err != nil {
		return router.SnapImage{}, err
	}
	return router.SnapImage{JPEG: shot.JPEG, W: shot.W, H: shot.H}, nil
}

// nativeWindowState remembers the rect of the placed native window, from the
// same native.window events the overlay gets (nil when none applies).
type nativeWindowState struct {
	mu   sync.Mutex
	rect *native.Rect
}

func (s *nativeWindowState) set(ev native.WindowEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rect = nil
	if ev.Rect != nil {
		r := *ev.Rect
		s.rect = &r
	}
}

func (s *nativeWindowState) get() *native.Rect {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rect == nil {
		return nil
	}
	r := *s.rect
	return &r
}

// snapGameArea is router.Config.Snap.
func (r *Registry) snapGameArea(ctx context.Context) (router.SnapImage, error) {
	return snapSource{
		nativeActive: func() bool {
			mgr := r.nativeIfStarted()
			return mgr != nil && mgr.Status().Active()
		},
		nativeRect: r.nativeWin.get,
		desktop:    remotecontrol.CaptureDesktopArea,
		page: func(ctx context.Context, maxSide, quality int) (remotecontrol.Shot, error) {
			return remotecontrol.CaptureGameArea(ctx, r.snapshotConfig().PlatformDebugPort, maxSide, quality)
		},
	}.capture(ctx)
}
