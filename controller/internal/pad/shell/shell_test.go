package shell

import (
	"encoding/json"
	"sync"
	"testing"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

type recAdapter struct {
	mu  sync.Mutex
	out []hub.Output
}

func (r *recAdapter) Name() string { return "cdp" }
func (r *recAdapter) Deliver(_ string, outs []hub.Output) error {
	r.mu.Lock()
	r.out = append(r.out, outs...)
	r.mu.Unlock()
	return nil
}

func findByID(n protocol.Node, id string) (protocol.Node, bool) {
	if n.Props["id"] == id {
		return n, true
	}
	for _, c := range n.Children {
		if f, ok := findByID(c, id); ok {
			return f, true
		}
	}
	return protocol.Node{}, false
}

func TestRemoteLayoutFollowsContract(t *testing.T) {
	p := New(Deps{HostName: func() string { return "Ada" }})
	host := p.Layout(protocol.Player{ID: "p1", Host: true})
	raw, _ := json.Marshal(protocol.Layout{T: "layout", Version: 1, Tree: host})
	if _, err := protocol.ParseControl(raw); err != nil {
		t.Fatal(err)
	}
	pad, ok := findByID(host, ControlTrackpad)
	if !ok || pad.Type != "trackpad" || pad.Props["mode"] != "relative" {
		t.Fatalf("trackpad %+v", pad)
	}
	for _, id := range []string{ControlBack, ControlHome, ControlOpenYui, ControlKeyboardToggle} {
		if n, ok := findByID(host, id); !ok || n.Type != "button" || n.Props["bit"] != nil {
			t.Fatalf("%s %+v", id, n)
		}
	}
	if _, ok := findByID(host, ControlKeyboard); ok {
		t.Fatal("keyboard shown before the toggle")
	}
	kb, ok := findByID(Remote(true), ControlKeyboard)
	if !ok || kb.Type != "keyboard" || kb.Props["mode"] != "live" {
		t.Fatalf("keyboard %+v", kb)
	}

	guest := p.Layout(protocol.Player{ID: "p2"})
	if hub.ContainsType(guest, "trackpad") || guest.Children[1].Props["text"] != "Waiting for Ada" {
		t.Fatalf("waiting %+v", guest)
	}
}

func TestOnlyHostDrivesThePage(t *testing.T) {
	rec := &recAdapter{}
	p := New(Deps{Pointer: rec, IsHost: func(id string) bool { return id == "host" }})
	ev := protocol.Event{T: "event", Control: ControlTrackpad, Kind: "change", Value: map[string]any{"dx": 1.0}}
	p.HandleEvent("guest", ev)
	p.HandleText("guest", protocol.Text{T: "text", Op: "insert", Text: "x"})
	p.HandleEvent("host", ev)
	p.HandleEvent("host", protocol.Event{T: "event", Control: ControlTrackpad, Kind: "press"})
	p.HandleText("host", protocol.Text{T: "text", Op: "enter"})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.out) != 2 || rec.out[0].Target != "pointer" || rec.out[1].Target != "text" || rec.out[1].Kind != hub.KindText {
		t.Fatalf("outputs %+v", rec.out)
	}
}
