package hub

import (
	"encoding/json"
	"testing"

	"kiosk/controller/internal/pad/protocol"
)

func TestPresetNodeShape(t *testing.T) {
	node := MotionRemotePreset(map[string]string{
		"South":    "A",
		"R2":       "B",
		"West":     "1",
		"North":    "2",
		"Home":     "H", // reserved: dropped
		"Recenter": "R", // reserved: dropped
		"Bogus":    "?",
	}, "L3", "Home", "L3")
	raw, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"preset","props":{"hide":["L3"],"labels":{"North":"2","R2":"B","South":"A","West":"1"},"name":"motion-remote"}}`
	if string(raw) != want {
		t.Fatalf("preset\n got %s\nwant %s", raw, want)
	}
	// A layout with the preset passes the control parser.
	frame, _ := json.Marshal(protocol.Layout{T: "layout", Version: 1, Tree: protocol.Node{
		Type: "column", Children: []protocol.Node{node, MotionNode("motion", true)},
	}})
	if _, err := protocol.ParseControl(frame); err != nil {
		t.Fatal(err)
	}

	plain := GamepadPreset(nil)
	if plain.Props["name"] != PresetGamepad || len(plain.Props) != 1 {
		t.Fatalf("gamepad %+v", plain.Props)
	}
}

func TestButtonBitNames(t *testing.T) {
	if bit, ok := ButtonBit("South"); !ok || bit != protocol.ButtonSouth {
		t.Fatal("South")
	}
	for _, reserved := range []string{"Home", "Recenter"} {
		if _, ok := ButtonBit(reserved); ok {
			t.Fatalf("%s must not be a layout button", reserved)
		}
	}
	if len(ButtonNames()) != 16 {
		t.Fatalf("names %v", ButtonNames())
	}
}

func TestContainsType(t *testing.T) {
	tree := protocol.Node{Type: "column", Children: []protocol.Node{
		{Type: "row", Children: []protocol.Node{{Type: "keyboard"}}},
	}}
	if !ContainsType(tree, "keyboard") || ContainsType(tree, "trackpad") {
		t.Fatal("ContainsType")
	}
}

func TestPresetActionsAreOptionalAndFiltered(t *testing.T) {
	node := PresetWithActions(PresetGamepad, nil, nil, map[string]string{"South": "Accelerate", "Dpad": "Steer", "Bogus": "x", "East": ""})
	actions, ok := node.Props["actions"].(map[string]any)
	if !ok || len(actions) != 2 || actions["South"] != "Accelerate" || actions["Dpad"] != "Steer" {
		t.Fatalf("actions = %#v", node.Props["actions"])
	}
	if plain := Preset(PresetGamepad, nil, nil); len(plain.Props) != 1 {
		t.Fatalf("plain preset gained props: %#v", plain.Props)
	}
}
