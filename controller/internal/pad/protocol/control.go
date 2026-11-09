package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Player mirrors the Rails Player JSON.
type Player struct {
	ID        string `json:"id"`
	Slot      *int   `json:"slot"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Host      bool   `json:"host"`
	Connected bool   `json:"connected"`
	// AccountLinked: this player is playing as a Yui account this session.
	// Never the account itself (plan 10, P5).
	AccountLinked bool `json:"account_linked"`
	// Avatar is an emoji or a small data:image/jpeg|webp;base64 selfie
	// (contracts §2). Rails clears it when the session ends.
	Avatar string `json:"avatar,omitempty"`
}

// Node is a layout tree node (same shape as ctx.ui).
type Node struct {
	Type     string         `json:"type"`
	Props    map[string]any `json:"props,omitempty"`
	Children []Node         `json:"children,omitempty"`
}

type Caps struct {
	Motion   bool `json:"motion"`
	Haptics  bool `json:"haptics"`
	WakeLock bool `json:"wakeLock"`
}

type Screen struct {
	W           float64 `json:"w"`
	H           float64 `json:"h"`
	Orientation string  `json:"orientation"`
}

type Hello struct {
	T      string `json:"t"`
	V      int    `json:"v"`
	Caps   Caps   `json:"caps"`
	Screen Screen `json:"screen"`
}

type Welcome struct {
	T         string `json:"t"`
	V         int    `json:"v"`
	Player    Player `json:"player"`
	KioskName string `json:"kiosk_name"`
}

type Layout struct {
	T       string `json:"t"`
	Version int    `json:"version"`
	Tree    Node   `json:"tree"`
}

type Event struct {
	T       string `json:"t"`
	Control string `json:"control"`
	Kind    string `json:"kind"`
	Value   any    `json:"value,omitempty"`
}

type Text struct {
	T    string `json:"t"`
	Op   string `json:"op"`
	Text string `json:"text,omitempty"`
}

// Haptic carries a named pattern or a custom on/off ms pattern, never both.
type Haptic struct {
	T         string   `json:"t"`
	Pattern   []int    `json:"pattern,omitempty"`
	Name      string   `json:"name,omitempty"`
	Intensity *float64 `json:"intensity,omitempty"`
}

type Sound struct {
	T      string   `json:"t"`
	URL    string   `json:"url"`
	Volume *float64 `json:"volume,omitempty"`
}

type PlayerMsg struct {
	T      string `json:"t"`
	Player Player `json:"player"`
}

type System struct {
	T      string `json:"t"`
	Action string `json:"action"`
	Value  any    `json:"value,omitempty"`
}

// ProfileRequest (kiosk → pad) asks the phone's player to let a game keep a
// profile on their Yui account. AppName is shown to the player.
type ProfileRequest struct {
	T       string `json:"t"`
	AppID   string `json:"app_id"`
	AppName string `json:"app_name"`
}

// ProfileReply (pad → kiosk) says what the player chose. It only wakes the
// kiosk: Rails decides whether the grant exists.
type ProfileReply struct {
	T       string `json:"t"`
	AppID   string `json:"app_id"`
	Granted bool   `json:"granted"`
}

// Ping and Pong share a shape; ID may be a number or string on the wire.
type Ping struct {
	T  string `json:"t"`
	ID any    `json:"id"`
	TS int64  `json:"ts"`
}

type Pong = Ping

var (
	eventKinds    = set("press", "release", "tap", "change", "submit", "swipe")
	textOps       = set("insert", "backspace", "enter")
	systemActions = set("home", "resume", "quit", "save_state", "load_state", "be_speaker", "volume", "sensitive")
)

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

func decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	return dec.Decode(v)
}

func checkPlayer(raw map[string]json.RawMessage, key string) error {
	p, ok := raw[key]
	if !ok {
		return fmt.Errorf("missing %s", key)
	}
	var f map[string]json.RawMessage
	if err := json.Unmarshal(p, &f); err != nil {
		return fmt.Errorf("%s must be an object", key)
	}
	for _, k := range []string{"id", "slot", "name", "color", "host", "connected"} {
		if _, ok := f[k]; !ok {
			return fmt.Errorf("%s.%s missing", key, k)
		}
	}
	// account_linked is a bool when present (null is not a bool).
	if v, ok := f["account_linked"]; ok {
		if t := bytes.TrimSpace(v); !bytes.Equal(t, []byte("true")) && !bytes.Equal(t, []byte("false")) {
			return fmt.Errorf("%s.account_linked must be a boolean", key)
		}
	}
	var pl Player
	if err := json.Unmarshal(p, &pl); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

func checkNode(n Node, depth int) error {
	if depth > 32 {
		return errors.New("layout: tree too deep")
	}
	if n.Type == "" {
		return errors.New("layout: node.type required")
	}
	for _, c := range n.Children {
		if err := checkNode(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// ParseControl validates a control frame and returns the typed message
// (Hello, Welcome, Layout, Event, Text, Haptic, Sound, PlayerMsg, System, Ping).
// Pong frames are returned as Ping values with T == "pong".
func ParseControl(data []byte) (any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("control: %w", err)
	}
	var t string
	if rt, ok := raw["t"]; !ok || json.Unmarshal(rt, &t) != nil || t == "" {
		return nil, errors.New("control: missing t")
	}
	wrap := func(err error) error { return fmt.Errorf("control: %s: %w", t, err) }
	need := func(keys ...string) error {
		for _, k := range keys {
			if _, ok := raw[k]; !ok {
				return fmt.Errorf("missing %s", k)
			}
		}
		return nil
	}

	switch t {
	case "hello":
		if err := need("v", "caps", "screen"); err != nil {
			return nil, wrap(err)
		}
		var m Hello
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		var caps map[string]json.RawMessage
		_ = json.Unmarshal(raw["caps"], &caps)
		for _, k := range []string{"motion", "haptics", "wakeLock"} {
			if _, ok := caps[k]; !ok {
				return nil, wrap(fmt.Errorf("caps.%s missing", k))
			}
		}
		return m, nil
	case "welcome":
		if err := need("v", "kiosk_name"); err != nil {
			return nil, wrap(err)
		}
		if err := checkPlayer(raw, "player"); err != nil {
			return nil, wrap(err)
		}
		var m Welcome
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		return m, nil
	case "layout":
		if err := need("version", "tree"); err != nil {
			return nil, wrap(err)
		}
		var m Layout
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if err := checkNode(m.Tree, 0); err != nil {
			return nil, wrap(err)
		}
		return m, nil
	case "event":
		var m Event
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if m.Control == "" || !eventKinds[m.Kind] {
			return nil, wrap(errors.New("bad control or kind"))
		}
		return m, nil
	case "text":
		var m Text
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if !textOps[m.Op] {
			return nil, wrap(errors.New("bad op"))
		}
		return m, nil
	case "haptic":
		var m Haptic
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if _, ok := raw["pattern"]; ok && m.Pattern == nil {
			m.Pattern = []int{} // an explicit [] is invalid, not "no pattern"
		}
		if err := m.Validate(); err != nil {
			return nil, wrap(err)
		}
		return m, nil
	case "sound":
		var m Sound
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if m.URL == "" {
			return nil, wrap(errors.New("url required"))
		}
		return m, nil
	case "player":
		if err := checkPlayer(raw, "player"); err != nil {
			return nil, wrap(err)
		}
		var m PlayerMsg
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		return m, nil
	case "system":
		var m System
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if !systemActions[m.Action] {
			return nil, wrap(errors.New("bad action"))
		}
		return m, nil
	case "profile_request":
		if err := need("app_id", "app_name"); err != nil {
			return nil, wrap(err)
		}
		var m ProfileRequest
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if m.AppID == "" {
			return nil, wrap(errors.New("app_id required"))
		}
		return m, nil
	case "profile_reply":
		if err := need("app_id", "granted"); err != nil {
			return nil, wrap(err)
		}
		var m ProfileReply
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		if m.AppID == "" {
			return nil, wrap(errors.New("app_id required"))
		}
		return m, nil
	case "ping", "pong":
		if err := need("id", "ts"); err != nil {
			return nil, wrap(err)
		}
		var m Ping
		if err := decode(data, &m); err != nil {
			return nil, wrap(err)
		}
		switch m.ID.(type) {
		case float64, string:
		default:
			return nil, wrap(errors.New("id must be number or string"))
		}
		return m, nil
	}
	return nil, fmt.Errorf("control: unknown t %q", t)
}
