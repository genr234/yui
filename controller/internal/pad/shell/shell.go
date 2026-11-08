// Package shell is the bottom provider of the focus stack: when no app owns
// the controllers, the host's phone is a remote for the kiosk page and the
// Yui overlay, and everyone else waits.
package shell

import (
	"sync"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// Control ids in the remote layout.
const (
	ControlTrackpad       = "trackpad"
	ControlKeyboard       = "keyboard"
	ControlKeyboardToggle = "keyboard_toggle"
	ControlBack           = "back"
	ControlHome           = "home"
	ControlOpenYui        = "open_yui"
)

// RemoteEvent is the bridge push event for the remote's buttons. Data is
// RemoteAction.
const RemoteEvent = "pad.remote"

// RemoteAction is the pad.remote payload. Action is "back", "home" or
// "open_yui".
type RemoteAction struct {
	PlayerID string `json:"player_id"`
	Action   string `json:"action"`
}

// Deps are what the shell provider drives. Nil funcs are skipped.
type Deps struct {
	// Pointer receives "pointer" and "text" outputs (the CDP adapter).
	Pointer hub.Adapter
	// IsHost reports whether a player holds the host role. Only the host
	// gets the remote; input from anyone else is ignored.
	IsHost func(playerID string) bool
	// HostName is shown on the waiting layout.
	HostName func() string
	// Emit pushes bridge events to the overlay.
	Emit func(event string, data any)
	// Refresh re-sends layouts after the provider's state changes.
	Refresh func()
	// Haptic buzzes a phone.
	Haptic func(playerID string, pattern []int)
	// NavigateHome loads the kiosk's start page (remote Home button).
	NavigateHome func()
}

// Provider implements hub.Provider.
type Provider struct {
	hub.NopProvider
	deps Deps

	mu       sync.Mutex
	keyboard map[string]bool
}

// New builds the shell provider.
func New(deps Deps) *Provider {
	return &Provider{deps: deps, keyboard: map[string]bool{}}
}

func (p *Provider) isHost(id string) bool {
	return p.deps.IsHost != nil && p.deps.IsHost(id)
}

// Layout implements hub.Provider.
func (p *Provider) Layout(player protocol.Player) protocol.Node {
	if !player.Host {
		return p.waiting()
	}
	p.mu.Lock()
	keyboard := p.keyboard[player.ID]
	p.mu.Unlock()
	return Remote(keyboard)
}

// Remote is the host's remote layout. keyboard adds the live keyboard.
func Remote(keyboard bool) protocol.Node {
	children := []protocol.Node{
		{Type: "trackpad", Props: map[string]any{
			"id": ControlTrackpad, "mode": "relative",
			"label": "Drag to move, tap to click, two fingers to scroll",
		}},
	}
	toggle := "Keyboard"
	if keyboard {
		toggle = "Hide keyboard"
		children = append(children, protocol.Node{Type: "keyboard", Props: map[string]any{
			"id": ControlKeyboard, "mode": "live", "placeholder": "Type on the screen",
		}})
	}
	children = append(children,
		protocol.Node{Type: "row", Props: map[string]any{"gap": 12}, Children: []protocol.Node{
			button(ControlBack, "Back", "secondary"),
			button(ControlHome, "Home", "secondary"),
			button(ControlOpenYui, "Open Yui", "primary"),
		}},
		button(ControlKeyboardToggle, toggle, "ghost"),
	)
	return protocol.Node{Type: "column", Props: map[string]any{"gap": 12}, Children: children}
}

func (p *Provider) waiting() protocol.Node {
	host := ""
	if p.deps.HostName != nil {
		host = p.deps.HostName()
	}
	title, body := "Waiting for the host", "The host is using the remote. When a game starts, your controls appear here."
	if host != "" {
		title = "Waiting for " + host
		body = host + " is using the remote. When a game starts, your controls appear here."
	}
	return protocol.Node{Type: "column", Props: map[string]any{"gap": 12}, Children: []protocol.Node{
		{Type: "spacer", Props: map[string]any{"flex": 1}},
		{Type: "text", Props: map[string]any{"text": title, "size": "large", "align": "center"}},
		{Type: "text", Props: map[string]any{"text": body, "size": "small", "align": "center"}},
		{Type: "spacer", Props: map[string]any{"flex": 1}},
	}}
}

func button(id, label, variant string) protocol.Node {
	return protocol.Node{Type: "button", Props: map[string]any{"id": id, "label": label, "variant": variant}}
}

// HandleEvent implements hub.Provider.
func (p *Provider) HandleEvent(playerID string, ev protocol.Event) {
	if !p.isHost(playerID) {
		return
	}
	switch ev.Control {
	case ControlTrackpad:
		if ev.Kind != "change" && ev.Kind != "tap" {
			return
		}
		p.deliver(playerID, hub.Output{Adapter: "cdp", Target: "pointer", Kind: ev.Kind, Value: ev.Value})
		if ev.Kind == "tap" && p.deps.Haptic != nil {
			p.deps.Haptic(playerID, []int{8})
		}
	case ControlBack, ControlHome, ControlOpenYui:
		if ev.Kind != "tap" {
			return
		}
		if p.deps.Emit != nil {
			p.deps.Emit(RemoteEvent, RemoteAction{PlayerID: playerID, Action: ev.Control})
		}
		if ev.Control == ControlHome && p.deps.NavigateHome != nil {
			p.deps.NavigateHome()
		}
	case ControlKeyboardToggle:
		if ev.Kind != "tap" {
			return
		}
		p.mu.Lock()
		p.keyboard[playerID] = !p.keyboard[playerID]
		p.mu.Unlock()
		if p.deps.Refresh != nil {
			p.deps.Refresh()
		}
	}
}

// HandleText implements hub.Provider: the host's live keyboard types into
// the focused element.
func (p *Provider) HandleText(playerID string, msg protocol.Text) {
	if !p.isHost(playerID) {
		return
	}
	p.deliver(playerID, hub.Output{Adapter: "cdp", Target: "text", Kind: hub.KindText, Value: msg})
}

// Forget drops per-player state (keyboard toggle) when a player leaves.
func (p *Provider) Forget(playerID string) {
	p.mu.Lock()
	delete(p.keyboard, playerID)
	p.mu.Unlock()
}

func (p *Provider) deliver(playerID string, out hub.Output) {
	if p.deps.Pointer != nil {
		_ = p.deps.Pointer.Deliver(playerID, []hub.Output{out})
	}
}
