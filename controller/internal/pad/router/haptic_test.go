package router

import (
	"testing"

	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/session"
)

func TestHapticMsgNamedCustomAndInvalid(t *testing.T) {
	h := newHarness(t)
	h.sess.firePlayer(session.PlayerReady, player("p1", 1, true))
	h.sess.firePlayer(session.PlayerReady, player("p2", 2, false))
	h.sess.take()

	half := 0.5
	if err := h.router.HapticMsg("p1", protocol.Haptic{Name: "win", Intensity: &half}); err != nil {
		t.Fatal(err)
	}
	if err := h.router.HapticMsg("p2", protocol.Haptic{Pattern: []int{40, 60, 40}}); err != nil {
		t.Fatal(err)
	}
	if err := h.router.HapticMsg("p1", protocol.Haptic{Name: "zap"}); err == nil {
		t.Fatal("unknown name accepted")
	}
	if err := h.router.HapticMsg("p1", protocol.Haptic{Pattern: []int{5000}}); err == nil {
		t.Fatal("over-long step accepted")
	}

	var got []sent
	for _, m := range h.sess.take() {
		if _, ok := m.msg.(protocol.Haptic); ok {
			got = append(got, m)
		}
	}
	if len(got) != 2 {
		t.Fatalf("sent %d haptics, want 2", len(got))
	}
	first := got[0].msg.(protocol.Haptic)
	if got[0].to != "p1" || first.T != "haptic" || first.Name != "win" || first.Intensity == nil || *first.Intensity != 0.5 {
		t.Fatalf("first = %+v", got[0])
	}
	if second := got[1].msg.(protocol.Haptic); got[1].to != "p2" || len(second.Pattern) != 3 {
		t.Fatalf("second = %+v", got[1])
	}
	if ids := h.router.ReadyIDs(); len(ids) != 2 {
		t.Fatalf("ReadyIDs = %v", ids)
	}
}
