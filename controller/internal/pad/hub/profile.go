package hub

import (
	"errors"
	"fmt"

	"kiosk/controller/internal/pad/protocol"
)

// Output kinds a profile can name. Event routes use the event's own kind
// when Route.Kind is empty ("press", "release", "tap", "change", "submit",
// "swipe").
const (
	// KindButton is the level of one button bit. Value is bool.
	KindButton = "button"
	// KindState is the whole input frame with Home already cleared.
	// Value is protocol.InputState.
	KindState = "state"
	// KindText is one text op. Value is protocol.Text.
	KindText = "text"
)

// Output is one mapped sample for an adapter. Adapter matches Adapter.Name.
type Output struct {
	Adapter string
	Target  string
	Kind    string
	Value   any
}

// Adapter is a sink for profile outputs. CDP, virtual pads, and app events
// implement this outside the hub. Deliver may run concurrently and may
// overlap UnregisterAdapter; a call already started finishes on the adapter
// value it captured.
type Adapter interface {
	Name() string
	Deliver(playerID string, outputs []Output) error
}

// Route maps one control id, one button bit, a text op, or the raw input
// frame onto an adapter target. Home cannot be routed.
type Route struct {
	// Control is a layout control id. Set for event routes and optional on
	// text routes. Empty for button and state routes.
	Control string
	// Button is one protocol button bit. Zero except on button routes.
	Button uint32
	// Adapter is the registered adapter name.
	Adapter string
	// Target is the adapter-specific output name (key, pad control, event).
	Target string
	// Kind is KindButton, KindState, KindText, or an event kind. Empty on a
	// control route means "use the event's kind".
	Kind string
}

// Profile is the input map for one provider. The hub applies it after the
// provider's Handle* method returns. Invalid routes are ignored when input
// is delivered; call Validate to see them.
type Profile struct {
	Routes []Route
}

// Validate reports routes the hub ignores when it delivers input.
func (p Profile) Validate() error {
	var errs []error
	for i, r := range p.Routes {
		if err := r.validate(); err != nil {
			errs = append(errs, fmt.Errorf("hub: route %d: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

func (r Route) ok() bool {
	if r.Adapter == "" || r.Target == "" || r.Button&protocol.ButtonHome != 0 {
		return false
	}
	switch {
	case r.Kind == KindState:
		return r.Button == 0 && r.Control == ""
	case r.Kind == KindText:
		return r.Button == 0
	case r.Button != 0:
		return r.Button&(r.Button-1) == 0 && r.Control == ""
	default:
		return r.Control != ""
	}
}

func (r Route) validate() error {
	if r.ok() {
		return nil
	}
	if r.Adapter == "" || r.Target == "" {
		return fmt.Errorf("%w: adapter and target are required", ErrBadRoute)
	}
	if r.Button&protocol.ButtonHome != 0 {
		return ErrReservedHome
	}
	switch {
	case r.Kind == KindState:
		return fmt.Errorf("%w: a state route has no button or control", ErrBadRoute)
	case r.Kind == KindText:
		return fmt.Errorf("%w: a text route has no button", ErrBadRoute)
	case r.Button != 0:
		if r.Button&(r.Button-1) != 0 {
			return fmt.Errorf("%w: a button route is a single bit", ErrBadRoute)
		}
		return fmt.Errorf("%w: a button route has no control id", ErrBadRoute)
	default:
		return fmt.Errorf("%w: control id is required", ErrBadRoute)
	}
}

func (p Profile) outputsForInput(state protocol.InputState) []Output {
	var out []Output
	for _, r := range p.Routes {
		if !r.ok() {
			continue
		}
		switch {
		case r.Kind == KindState:
			out = append(out, Output{
				Adapter: r.Adapter,
				Target:  r.Target,
				Kind:    KindState,
				Value:   state,
			})
		case r.Button != 0:
			kind := r.Kind
			if kind == "" {
				kind = KindButton
			}
			out = append(out, Output{
				Adapter: r.Adapter,
				Target:  r.Target,
				Kind:    kind,
				Value:   state.Buttons&r.Button != 0,
			})
		}
	}
	return out
}

func (p Profile) outputsForEvent(ev protocol.Event) []Output {
	var out []Output
	for _, r := range p.Routes {
		if !r.ok() || r.Button != 0 || r.Kind == KindState || r.Kind == KindText {
			continue
		}
		if r.Control != ev.Control {
			continue
		}
		kind := r.Kind
		if kind == "" {
			kind = ev.Kind
		}
		out = append(out, Output{
			Adapter: r.Adapter,
			Target:  r.Target,
			Kind:    kind,
			Value:   ev.Value,
		})
	}
	return out
}

func (p Profile) outputsForText(msg protocol.Text) []Output {
	var out []Output
	for _, r := range p.Routes {
		if !r.ok() || r.Kind != KindText {
			continue
		}
		out = append(out, Output{
			Adapter: r.Adapter,
			Target:  r.Target,
			Kind:    KindText,
			Value:   msg,
		})
	}
	return out
}
