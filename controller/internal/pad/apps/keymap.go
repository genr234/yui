package apps

import (
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"kiosk/controller/internal/pad/cdp"
	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// Keymap is a manifest's `controllers.keymap`: phone controls for an
// embedded web game, mapped to keyboard and mouse input in the page with no
// app code (contracts §4 `pad.app.focus`).
type Keymap struct {
	// Players is "host" (default: only the host plays, everyone else sees a
	// waiting screen) or "all".
	Players  string          `json:"players,omitempty"`
	Controls []KeymapControl `json:"controls"`
}

// KeymapControl is one phone control.
//
//	swipe     trackpad; swipes tap Keys[up|down|left|right], Tap is tapped on a one-finger tap
//	dpad      d-pad; Keys[up|down|left|right] are held while pressed
//	button    a button; holds Key, or the left mouse button with Mouse "left"
//	trackpad  Mode "absolute" with Mouse "drag": the touch point drags the
//	          left mouse button inside the game's frame; Mode "relative":
//	          a cursor (drag to move, tap to click)
type KeymapControl struct {
	Type  string            `json:"type"`
	Label string            `json:"label,omitempty"`
	Keys  map[string]string `json:"keys,omitempty"`
	Tap   string            `json:"tap,omitempty"`
	Key   string            `json:"key,omitempty"`
	Mouse string            `json:"mouse,omitempty"`
	Mode  string            `json:"mode,omitempty"`
}

// Keymap limits.
const (
	maxKeymapControls = 8
	maxKeymapButtons  = 10
	maxLabelRunes     = 40
)

// Players modes.
const (
	PlayersHost = "host"
	PlayersAll  = "all"
)

// buttonBitsInOrder are the bits keymap buttons are given, in order.
var buttonBitsInOrder = []uint32{
	protocol.ButtonSouth, protocol.ButtonEast, protocol.ButtonWest, protocol.ButtonNorth,
	protocol.ButtonL1, protocol.ButtonR1, protocol.ButtonL2, protocol.ButtonR2,
	protocol.ButtonSelect, protocol.ButtonStart,
}

var dpadBits = map[string]uint32{
	"up": protocol.ButtonUp, "down": protocol.ButtonDown,
	"left": protocol.ButtonLeft, "right": protocol.ButtonRight,
}

func bitIndex(bit uint32) int {
	for i := 0; i < 32; i++ {
		if bit == 1<<i {
			return i
		}
	}
	return -1
}

// compiledKeymap is a keymap turned into a layout and hub routes. Routes
// name adapter, which forwards to the CDP adapter.
type compiledKeymap struct {
	players string
	tree    protocol.Node
	routes  []hub.Route
}

// ErrBadKeymap wraps every keymap validation error.
var ErrBadKeymap = errors.New("invalid keymap")

func keymapErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadKeymap, fmt.Sprintf(format, args...))
}

// ValidateKeymap reports the first problem with km, or nil.
func ValidateKeymap(km Keymap) error {
	_, err := compileKeymap(km, nil, KeymapAdapterName)
	return err
}

// compileKeymap builds the layout and routes. region limits an absolute
// trackpad to the game's frame (CSS pixels); nil means the whole viewport.
func compileKeymap(km Keymap, region *cdp.Rect, adapter string) (compiledKeymap, error) {
	out := compiledKeymap{players: km.Players}
	switch km.Players {
	case "":
		out.players = PlayersHost
	case PlayersHost, PlayersAll:
	default:
		return out, keymapErr("players must be %q or %q", PlayersHost, PlayersAll)
	}
	if len(km.Controls) == 0 || len(km.Controls) > maxKeymapControls {
		return out, keymapErr("1 to %d controls", maxKeymapControls)
	}

	var children []protocol.Node
	var buttons []protocol.Node // consecutive buttons share a row
	flush := func() {
		if len(buttons) > 0 {
			children = append(children, protocol.Node{Type: "row", Props: map[string]any{"gap": 12}, Children: buttons})
			buttons = nil
		}
	}
	seen := map[string]bool{}
	nButtons := 0
	route := func(r hub.Route) {
		r.Adapter = adapter
		out.routes = append(out.routes, r)
	}

	for i, c := range km.Controls {
		if utf8.RuneCountInString(c.Label) > maxLabelRunes {
			return out, keymapErr("control %d: label is longer than %d characters", i, maxLabelRunes)
		}
		once := func(kind string) error {
			if seen[kind] {
				return keymapErr("control %d: only one %s", i, kind)
			}
			seen[kind] = true
			return nil
		}
		switch c.Type {
		case "swipe":
			if err := once("swipe"); err != nil {
				return out, err
			}
			target, err := cdp.SwipeTarget(c.Keys)
			if err != nil {
				return out, keymapErr("control %d: %v", i, err)
			}
			label := c.Label
			if label == "" {
				label = "Swipe to move"
			}
			flush()
			children = append(children, protocol.Node{Type: "trackpad", Props: map[string]any{
				"id": "swipe", "mode": "relative", "label": label,
			}})
			route(hub.Route{Control: "swipe", Kind: "change", Target: target})
			route(hub.Route{Control: "swipe", Kind: "swipe", Target: target})
			if c.Tap != "" {
				if !cdp.ValidKey(c.Tap) {
					return out, keymapErr("control %d: unknown key %q", i, c.Tap)
				}
				route(hub.Route{Control: "swipe", Kind: "tap", Target: "key:" + c.Tap})
			}
		case "dpad":
			if err := once("dpad"); err != nil {
				return out, err
			}
			if len(c.Keys) == 0 {
				return out, keymapErr("control %d: a dpad needs keys", i)
			}
			for _, dir := range sortedKeys(c.Keys) {
				key := c.Keys[dir]
				bit, ok := dpadBits[dir]
				if !ok {
					return out, keymapErr("control %d: unknown direction %q", i, dir)
				}
				if !cdp.ValidKey(key) {
					return out, keymapErr("control %d: unknown key %q", i, key)
				}
				route(hub.Route{Button: bit, Target: "key:" + key})
			}
			flush()
			children = append(children, protocol.Node{Type: "dpad", Props: map[string]any{"id": "dpad"}})
		case "button":
			if nButtons >= maxKeymapButtons {
				return out, keymapErr("at most %d buttons", maxKeymapButtons)
			}
			if c.Label == "" {
				return out, keymapErr("control %d: a button needs a label", i)
			}
			bit := buttonBitsInOrder[nButtons]
			nButtons++
			switch {
			case c.Key != "" && c.Mouse == "":
				if !cdp.ValidKey(c.Key) {
					return out, keymapErr("control %d: unknown key %q", i, c.Key)
				}
				route(hub.Route{Button: bit, Target: "key:" + c.Key})
			case c.Mouse == "left" && c.Key == "":
				route(hub.Route{Button: bit, Target: "click"})
			default:
				return out, keymapErr("control %d: a button needs key or mouse \"left\"", i)
			}
			buttons = append(buttons, protocol.Node{Type: "button", Props: map[string]any{
				"id": fmt.Sprintf("button-%d", nButtons), "label": c.Label, "variant": "primary", "bit": bitIndex(bit),
			}})
		case "trackpad":
			if err := once("trackpad"); err != nil {
				return out, err
			}
			flush()
			switch c.Mode {
			case "absolute":
				if c.Mouse != "drag" {
					return out, keymapErr("control %d: an absolute trackpad needs mouse \"drag\"", i)
				}
				label := c.Label
				if label == "" {
					label = "Drag to play"
				}
				target := cdp.MouseTarget(region)
				children = append(children, protocol.Node{Type: "trackpad", Props: map[string]any{
					"id": "drag", "mode": "absolute", "label": label,
				}})
				route(hub.Route{Kind: hub.KindState, Target: target})
				route(hub.Route{Control: "drag", Kind: "press", Target: target})
				route(hub.Route{Control: "drag", Kind: "release", Target: target})
			case "relative", "":
				if c.Mouse != "" && c.Mouse != "cursor" {
					return out, keymapErr("control %d: a relative trackpad moves the cursor", i)
				}
				label := c.Label
				if label == "" {
					label = "Drag to move, tap to click"
				}
				children = append(children, protocol.Node{Type: "trackpad", Props: map[string]any{
					"id": "pointer", "mode": "relative", "label": label,
				}})
				route(hub.Route{Control: "pointer", Target: "pointer"})
			default:
				return out, keymapErr("control %d: trackpad mode must be absolute or relative", i)
			}
		default:
			return out, keymapErr("control %d: unknown type %q", i, c.Type)
		}
	}
	flush()
	out.tree = protocol.Node{Type: "column", Props: map[string]any{"gap": 12}, Children: children}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
