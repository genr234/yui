package handlers

import (
	"testing"

	"kiosk/controller/internal/native"
)

func TestNativeWindowStateKeepsTheLastEvent(t *testing.T) {
	var s nativeWindowState
	if s.event() != nil || s.get() != nil {
		t.Fatal("empty state should have no event")
	}
	rect := native.Rect{X: 0, Y: 656, W: 1080, H: 608}
	s.set(native.WindowEvent{AppID: "dev.genr.wii-arcade", Rect: &rect, Screen: native.Screen{W: 1080, H: 1920}})
	rect.Y = 1 // the stored event is a copy
	ev := s.event()
	if ev == nil || ev.AppID != "dev.genr.wii-arcade" || ev.Rect == nil || ev.Rect.Y != 656 || ev.Screen.H != 1920 {
		t.Fatalf("event = %+v", ev)
	}
	ev.Rect.Y = 2 // and so is what it hands out
	if got := s.get(); got == nil || got.Y != 656 {
		t.Fatalf("get = %+v", got)
	}
	s.set(native.WindowEvent{AppID: "dev.genr.wii-arcade", Rect: nil})
	if s.get() != nil {
		t.Fatal("a nil-rect event clears the rect")
	}
}

func TestNativeWindowCommandIsNullWithoutARunningSession(t *testing.T) {
	r := testRegistry(t)
	rect := native.Rect{W: 10, H: 10}
	r.nativeWin.set(native.WindowEvent{AppID: "a.b.c", Rect: &rect})
	got, err := NativeWindowCommand{}.Handle(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil while no native session runs", got)
	}
}
