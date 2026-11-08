package hub

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"kiosk/controller/internal/pad/protocol"
)

var (
	ErrNilProvider    = errors.New("hub: nil provider")
	ErrBadLayer       = errors.New("hub: unknown layer")
	ErrPopShell       = errors.New("hub: shell stays at the bottom of the focus stack")
	ErrLayerEmpty     = errors.New("hub: layer is empty")
	ErrNilAdapter     = errors.New("hub: nil adapter")
	ErrBadAdapter     = errors.New("hub: adapter name is empty")
	ErrUnknownAdapter = errors.New("hub: unknown adapter")
	ErrEmptyPlayer    = errors.New("hub: player id is empty")
	ErrReservedHome   = errors.New("hub: home is reserved")
	ErrBadRoute       = errors.New("hub: invalid route")
)

// Stack is the focus stack. Shell is installed by New and cannot be removed.
// Push on an occupied layer swaps that layer's provider. A call that has
// already chosen a provider finishes on that provider; later calls see the
// swap.
//
// Handlers run without the stack lock and may run at the same time, including
// while Push, Pop, or RegisterAdapter runs. Shared bookkeeping inside Stack
// is guarded. Providers and adapters guard their own state.
type Stack struct {
	mu          sync.Mutex
	layers      [3]Provider
	adapters    map[string]Adapter
	home        HomeFunc
	lastButtons map[string]uint32
}

// New installs shell as the bottom provider. home may be nil.
func New(shell Provider, home HomeFunc) (*Stack, error) {
	if shell == nil {
		return nil, ErrNilProvider
	}
	s := &Stack{
		adapters:    make(map[string]Adapter),
		home:        home,
		lastButtons: make(map[string]uint32),
	}
	s.layers[LayerShell] = shell
	return s, nil
}

// Push sets the provider for layer, replacing whatever was there.
// LayerShell may be replaced and may not be cleared.
func (s *Stack) Push(layer Layer, provider Provider) error {
	if !validLayer(layer) {
		return ErrBadLayer
	}
	if provider == nil {
		return ErrNilProvider
	}
	s.mu.Lock()
	s.layers[layer] = provider
	s.mu.Unlock()
	return nil
}

// Pop removes an app or native provider. The shell layer cannot be popped.
// Popping an empty app or native layer returns ErrLayerEmpty and changes nothing.
func (s *Stack) Pop(layer Layer) error {
	if !validLayer(layer) {
		return ErrBadLayer
	}
	if layer == LayerShell {
		return ErrPopShell
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.layers[layer] == nil {
		return ErrLayerEmpty
	}
	s.layers[layer] = nil
	return nil
}

// Top reports the highest occupied layer and its provider.
func (s *Stack) Top() (Layer, Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.top()
}

// At returns the provider installed on layer, or nil when that layer is empty.
func (s *Stack) At(layer Layer) Provider {
	if !validLayer(layer) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.layers[layer]
}

// RegisterAdapter adds an adapter, or replaces the adapter with the same name.
// A Deliver call already in progress keeps the previous value.
func (s *Stack) RegisterAdapter(adapter Adapter) error {
	if adapter == nil {
		return ErrNilAdapter
	}
	name := adapter.Name()
	if name == "" {
		return ErrBadAdapter
	}
	s.mu.Lock()
	s.adapters[name] = adapter
	s.mu.Unlock()
	return nil
}

// UnregisterAdapter removes a named adapter. A missing name is a no-op.
func (s *Stack) UnregisterAdapter(name string) {
	s.mu.Lock()
	delete(s.adapters, name)
	s.mu.Unlock()
}

// Layout asks the top provider for this player's layout tree.
func (s *Stack) Layout(player protocol.Player) protocol.Node {
	provider := s.snapshot()
	if provider == nil {
		return protocol.Node{}
	}
	return provider.Layout(player)
}

// Input delivers one input frame to the top provider and then to adapters
// named by that provider's profile. The Home bit is cleared first. A rising
// edge of Home notifies the platform and is not a profile output.
func (s *Stack) Input(playerID string, state protocol.InputState) error {
	if playerID == "" {
		return ErrEmptyPlayer
	}
	provider, edge := s.observeButtons(playerID, state.Buttons)
	if edge {
		s.notifyHome(playerID, HomeViaButton)
	}
	state.Buttons &^= protocol.ButtonHome
	if provider == nil {
		return nil
	}
	provider.HandleInput(playerID, state)
	return s.deliver(playerID, provider.Profile().outputsForInput(state))
}

// Event delivers a control event to the top provider, then mapped outputs.
func (s *Stack) Event(playerID string, ev protocol.Event) error {
	if playerID == "" {
		return ErrEmptyPlayer
	}
	provider := s.snapshot()
	if provider == nil {
		return nil
	}
	provider.HandleEvent(playerID, ev)
	return s.deliver(playerID, provider.Profile().outputsForEvent(ev))
}

// Text delivers a text op to the top provider, then text routes.
func (s *Stack) Text(playerID string, msg protocol.Text) error {
	if playerID == "" {
		return ErrEmptyPlayer
	}
	provider := s.snapshot()
	if provider == nil {
		return nil
	}
	provider.HandleText(playerID, msg)
	return s.deliver(playerID, provider.Profile().outputsForText(msg))
}

// System delivers a control system message. Action "home" goes to the
// platform and not to the provider. Any other action goes to the provider.
func (s *Stack) System(playerID string, msg protocol.System) error {
	if playerID == "" {
		return ErrEmptyPlayer
	}
	if msg.Action == "home" {
		s.notifyHome(playerID, HomeViaSystem)
		return nil
	}
	provider := s.snapshot()
	if provider == nil {
		return nil
	}
	provider.HandleSystem(playerID, msg)
	return nil
}

func validLayer(layer Layer) bool {
	return layer >= LayerShell && layer <= LayerNative
}

func (s *Stack) top() (Layer, Provider) {
	for layer := LayerNative; layer >= LayerShell; layer-- {
		if s.layers[layer] != nil {
			return layer, s.layers[layer]
		}
	}
	return LayerShell, nil
}

func (s *Stack) snapshot() Provider {
	s.mu.Lock()
	_, provider := s.top()
	s.mu.Unlock()
	return provider
}

// observeButtons records the button mask, including Home, and reports the
// provider that should receive this frame plus whether Home rose.
func (s *Stack) observeButtons(playerID string, buttons uint32) (Provider, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.lastButtons[playerID]
	s.lastButtons[playerID] = buttons
	_, provider := s.top()
	edge := buttons&protocol.ButtonHome != 0 && prev&protocol.ButtonHome == 0
	return provider, edge
}

func (s *Stack) notifyHome(playerID string, via HomeVia) {
	s.mu.Lock()
	home := s.home
	s.mu.Unlock()
	if home != nil {
		home(playerID, via)
	}
}

func (s *Stack) deliver(playerID string, outputs []Output) error {
	if len(outputs) == 0 {
		return nil
	}
	grouped := make(map[string][]Output)
	for _, output := range outputs {
		grouped[output.Adapter] = append(grouped[output.Adapter], output)
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	s.mu.Lock()
	found := make(map[string]Adapter, len(names))
	var unknown []string
	for _, name := range names {
		adapter, ok := s.adapters[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		found[name] = adapter
	}
	s.mu.Unlock()

	var errs []error
	for _, name := range names {
		adapter, ok := found[name]
		if !ok {
			continue
		}
		if err := adapter.Deliver(playerID, grouped[name]); err != nil {
			errs = append(errs, fmt.Errorf("hub: adapter %s: %w", name, err))
		}
	}
	for _, name := range unknown {
		errs = append(errs, fmt.Errorf("%w: %s", ErrUnknownAdapter, name))
	}
	return errors.Join(errs...)
}
