package handlers

import (
	"context"
	"errors"
	"image"
	"testing"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/router"
	"kiosk/controller/internal/remotecontrol"
)

func testSource(active bool, rect *native.Rect) (snapSource, *[]string, *image.Rectangle) {
	var calls []string
	var got image.Rectangle
	return snapSource{
		nativeActive: func() bool { return active },
		nativeRect:   func() *native.Rect { return rect },
		desktop: func(r image.Rectangle, _, _ int) (remotecontrol.Shot, error) {
			calls = append(calls, "desktop")
			got = r
			return remotecontrol.Shot{JPEG: []byte{1}, W: 1, H: 1}, nil
		},
		page: func(context.Context, int, int) (remotecontrol.Shot, error) {
			calls = append(calls, "page")
			return remotecontrol.Shot{JPEG: []byte{2}, W: 2, H: 2}, nil
		},
	}, &calls, &got
}

func TestSnapNativeWindowCropsTheDesktopToItsRect(t *testing.T) {
	src, calls, got := testSource(true, &native.Rect{X: 0, Y: 656, W: 1080, H: 608})
	img, err := src.capture(context.Background())
	if err != nil || len(*calls) != 1 || (*calls)[0] != "desktop" || img.JPEG[0] != 1 {
		t.Fatalf("%v %v %+v", err, *calls, img)
	}
	if *got != image.Rect(0, 656, 1080, 1264) {
		t.Fatalf("rect %v", *got)
	}
}

func TestSnapNativeWithoutPlacementTakesTheWholeDesktop(t *testing.T) {
	src, calls, got := testSource(true, nil)
	if _, err := src.capture(context.Background()); err != nil || (*calls)[0] != "desktop" || got.Dx() < 10000 {
		t.Fatalf("%v %v %v", err, *calls, *got)
	}
}

func TestSnapWebGameUsesThePage(t *testing.T) {
	src, calls, _ := testSource(false, nil)
	img, err := src.capture(context.Background())
	if err != nil || (*calls)[0] != "page" || img.JPEG[0] != 2 {
		t.Fatalf("%v %v", err, *calls)
	}
}

func TestSnapUnsupportedDesktopIsAClearError(t *testing.T) {
	src, _, _ := testSource(true, nil)
	src.desktop = func(image.Rectangle, int, int) (remotecontrol.Shot, error) {
		return remotecontrol.Shot{}, remotecontrol.ErrDesktopUnsupported
	}
	_, err := src.capture(context.Background())
	var ue router.UserError
	if !errors.As(err, &ue) || ue != "Not supported here" {
		t.Fatalf("%v", err)
	}
}

func TestNativeWindowStateTracksTheRect(t *testing.T) {
	var s nativeWindowState
	if s.get() != nil {
		t.Fatal("starts empty")
	}
	s.set(native.WindowEvent{Rect: &native.Rect{X: 1, Y: 2, W: 3, H: 4}})
	if r := s.get(); r == nil || r.H != 4 {
		t.Fatalf("%v", r)
	}
	s.set(native.WindowEvent{Rect: nil})
	if s.get() != nil {
		t.Fatal("cleared by a nil rect")
	}
}
