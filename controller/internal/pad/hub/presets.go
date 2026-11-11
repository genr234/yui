package hub

import (
	"sort"

	"kiosk/controller/internal/pad/protocol"
)

// Preset names the pad knows how to expand (contracts §3 "Control props").
const (
	PresetGamepad      = "gamepad"
	PresetMotionRemote = "motion-remote"
)

// buttonBits maps contract button names to input bits. Home and Recenter are
// system buttons and cannot be labelled or hidden.
var buttonBits = map[string]uint32{
	"South":  protocol.ButtonSouth,
	"East":   protocol.ButtonEast,
	"West":   protocol.ButtonWest,
	"North":  protocol.ButtonNorth,
	"L1":     protocol.ButtonL1,
	"R1":     protocol.ButtonR1,
	"L2":     protocol.ButtonL2,
	"R2":     protocol.ButtonR2,
	"Select": protocol.ButtonSelect,
	"Start":  protocol.ButtonStart,
	"L3":     protocol.ButtonL3,
	"R3":     protocol.ButtonR3,
	"Up":     protocol.ButtonUp,
	"Down":   protocol.ButtonDown,
	"Left":   protocol.ButtonLeft,
	"Right":  protocol.ButtonRight,
}

// ButtonBit returns the input bit for a contract button name ("South",
// "R2", "Up", ...). Home and Recenter are not names a layout can use.
func ButtonBit(name string) (uint32, bool) {
	bit, ok := buttonBits[name]
	return bit, ok
}

// ButtonNames lists the names ButtonBit accepts, sorted.
func ButtonNames() []string {
	out := make([]string, 0, len(buttonBits))
	for name := range buttonBits {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Preset builds a `preset` node. labels maps button names to the text the
// pad draws on them (for example {"South": "A", "R2": "B"}); hide lists
// buttons to leave out. Unknown button names are dropped, so a preset never
// carries Home or Recenter. The pad expands the node, picks the orientation,
// and puts every press into the input frame bits.
func Preset(name string, labels map[string]string, hide []string) protocol.Node {
	return PresetWithActions(name, labels, hide, nil)
}

// PresetWithActions is Preset plus `actions`: what each button does in this
// app (for example {"South": "Accelerate", "East": "Drift"}), shown in the
// pad's button guide. Keys are button names, or "Dpad" and "Stick" for those
// controls. It is optional: empty actions leave the node exactly as Preset
// builds it.
func PresetWithActions(name string, labels map[string]string, hide []string, actions map[string]string) protocol.Node {
	props := map[string]any{"name": name}
	if len(labels) > 0 {
		clean := make(map[string]any, len(labels))
		for button, label := range labels {
			if _, ok := buttonBits[button]; ok && label != "" {
				clean[button] = label
			}
		}
		if len(clean) > 0 {
			props["labels"] = clean
		}
	}
	var hidden []any
	seen := map[string]bool{}
	for _, button := range hide {
		if _, ok := buttonBits[button]; ok && !seen[button] {
			seen[button] = true
			hidden = append(hidden, button)
		}
	}
	if len(hidden) > 0 {
		props["hide"] = hidden
	}
	if len(actions) > 0 {
		clean := make(map[string]any, len(actions))
		for control, action := range actions {
			_, button := buttonBits[control]
			if (button || control == "Dpad" || control == "Stick") && action != "" {
				clean[control] = action
			}
		}
		if len(clean) > 0 {
			props["actions"] = clean
		}
	}
	return protocol.Node{Type: "preset", Props: props}
}

// GamepadPreset is a left stick, d-pad, four face buttons, shoulders and
// Select/Start.
func GamepadPreset(labels map[string]string, hide ...string) protocol.Node {
	return Preset(PresetGamepad, labels, hide)
}

// MotionRemotePreset is a pointing remote: upright, pointer motion with
// South, R2, d-pad, West/North and Select/Start; held sideways, d-pad with
// West and North. Add a `motion` node next to it to stream accel/gyro.
func MotionRemotePreset(labels map[string]string, hide ...string) protocol.Node {
	return Preset(PresetMotionRemote, labels, hide)
}

// MotionNode turns on accel/gyro streaming (and pointer axes when pointer is
// true) for the layout it is part of.
func MotionNode(id string, pointer bool) protocol.Node {
	return protocol.Node{Type: "motion", Props: map[string]any{"id": id, "pointer": pointer}}
}

// ContainsType reports whether the tree has a node of the given type.
func ContainsType(n protocol.Node, typ string) bool {
	if n.Type == typ {
		return true
	}
	for _, c := range n.Children {
		if ContainsType(c, typ) {
			return true
		}
	}
	return false
}

// PadRoute sends a player's whole input frame to the virtual pad adapter.
// Target "player" means the virtual pad numbered by the player's slot.
func PadRoute(adapter, target string) Route {
	return Route{Kind: KindState, Adapter: adapter, Target: target}
}
