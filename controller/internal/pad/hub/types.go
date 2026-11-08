// Package hub is the focus stack that decides which layout provider owns each
// player's controls, and the adapter surface those providers write to.
//
// The stack is always shell, then app, then native session. The highest
// occupied layer supplies layouts and receives input. Home (button bit 10 and
// the control action "home") is reserved for the platform: providers and
// adapters never see it.
//
// Names here are generic positions and roles. Device-specific button labels
// belong to the app that builds the layout, not to this package.
package hub

import "kiosk/controller/internal/pad/protocol"

// Layer is one step of the focus stack, from the bottom up.
type Layer int

const (
	LayerShell Layer = iota
	LayerApp
	LayerNative
)

func (l Layer) String() string {
	switch l {
	case LayerShell:
		return "shell"
	case LayerApp:
		return "app"
	case LayerNative:
		return "native"
	default:
		return "unknown"
	}
}

// HomeVia says which path raised the reserved Home action.
type HomeVia string

const (
	HomeViaButton HomeVia = "button" // input bit 10, rising edge
	HomeViaSystem HomeVia = "system" // control message action "home"
)

// HomeFunc receives reserved Home input. playerID is the pad that raised it.
// A nil func is not called. Home is still stripped from provider input.
//
// The func runs without the stack lock held. It may Push, Pop, register
// adapters, or call Input, Event, Text, or System. A re-entered call runs
// to completion before the outer handler continues.
type HomeFunc func(playerID string, via HomeVia)

// Provider is one focus-stack layer. The top provider supplies each player's
// layout and receives that player's input.
//
// Methods may run concurrently, including a call that already started on a
// provider which has since been replaced. HandleInput's button mask never
// has the Home bit set.
type Provider interface {
	Layout(player protocol.Player) protocol.Node
	Profile() Profile
	HandleInput(playerID string, state protocol.InputState)
	HandleEvent(playerID string, ev protocol.Event)
	HandleText(playerID string, msg protocol.Text)
	HandleSystem(playerID string, msg protocol.System)
}

// NopProvider is an empty Provider. Embed it and override the methods a
// layer actually uses.
type NopProvider struct{}

func (NopProvider) Layout(protocol.Player) protocol.Node    { return protocol.Node{} }
func (NopProvider) Profile() Profile                        { return Profile{} }
func (NopProvider) HandleInput(string, protocol.InputState) {}
func (NopProvider) HandleEvent(string, protocol.Event)      {}
func (NopProvider) HandleText(string, protocol.Text)        {}
func (NopProvider) HandleSystem(string, protocol.System)    {}
