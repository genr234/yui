package apps

import (
	"fmt"
	"strings"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// appProvider is the app layer: an open app that declared controllers.
type appProvider struct {
	hub.NopProvider
	c      *Controller
	appID  string
	name   string
	keymap *compiledKeymap
	frames bool
	routes []hub.Route // guarded by c.mu
}

func (p *appProvider) Layout(player protocol.Player) protocol.Node {
	if tree, ok := p.c.layoutFor(p.appID, player); ok {
		return tree
	}
	p.c.mu.Lock()
	km, name := p.keymap, p.name
	p.c.mu.Unlock()
	if km != nil {
		if km.players == PlayersHost && !player.Host {
			return waiting(p.c.hostName(), name)
		}
		return km.tree
	}
	return starting(name)
}

func (p *appProvider) Profile() hub.Profile {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	return hub.Profile{Routes: p.routes}
}

func (p *appProvider) HandleSystem(playerID string, msg protocol.System) {
	p.c.emitSystem(p.appID, playerID, msg)
}

// nativeProvider is the native layer: a program an app started.
type nativeProvider struct {
	hub.NopProvider
	c         *Controller
	appID     string
	sessionID string
	routes    []hub.Route // guarded by c.mu
}

func (p *nativeProvider) Layout(player protocol.Player) protocol.Node {
	if tree, ok := p.c.layoutFor(p.appID, player); ok {
		return tree
	}
	// Until the app sends one: a generic gamepad.
	return hub.GamepadPreset(nil)
}

func (p *nativeProvider) Profile() hub.Profile {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	return hub.Profile{Routes: p.routes}
}

func (p *nativeProvider) HandleSystem(playerID string, msg protocol.System) {
	p.c.emitSystem(p.appID, playerID, msg)
}

// emitSystem forwards a Home-sheet action (resume, quit, save_state, …) to
// the app that owns the layer.
func (c *Controller) emitSystem(appID, playerID string, msg protocol.System) {
	c.emit(AppInputEvent, Input{AppID: appID, PlayerID: playerID, Target: msg.Action, Kind: "system", Value: msg.Value})
}

func waiting(host, app string) protocol.Node {
	title := "Waiting for the host"
	body := "The host is playing."
	if host != "" {
		title = "Waiting for " + host
		body = host + " is playing."
	}
	if app != "" {
		body = strings.TrimSuffix(body, ".") + " " + app + "."
	}
	return textScreen(title, body)
}

func starting(app string) protocol.Node {
	if app == "" {
		return textScreen("Getting ready", "Your controls appear here in a moment.")
	}
	return textScreen(app, "Your controls appear here in a moment.")
}

func textScreen(title, body string) protocol.Node {
	return protocol.Node{Type: "column", Props: map[string]any{"gap": 12}, Children: []protocol.Node{
		{Type: "spacer", Props: map[string]any{"flex": 1}},
		{Type: "text", Props: map[string]any{"text": title, "size": "large", "align": "center"}},
		{Type: "text", Props: map[string]any{"text": body, "size": "small", "align": "center"}},
		{Type: "spacer", Props: map[string]any{"flex": 1}},
	}}
}

func appAdapterName(appID string) string { return "apps:" + appID }

// appAdapter forwards an app's routed outputs to the overlay.
type appAdapter struct {
	c     *Controller
	appID string
}

func (a appAdapter) Name() string { return appAdapterName(a.appID) }

func (a appAdapter) Deliver(playerID string, outputs []hub.Output) error {
	for _, out := range outputs {
		a.c.emit(AppInputEvent, Input{AppID: a.appID, PlayerID: playerID, Target: out.Target, Kind: out.Kind, Value: out.Value})
	}
	return nil
}

// keymapAdapter forwards keymap outputs to CDP. In "host" mode only the
// host's presses get through; releases always do, so nothing sticks when
// the host role moves.
type keymapAdapter struct{ c *Controller }

func (keymapAdapter) Name() string { return KeymapAdapterName }

func (a keymapAdapter) Deliver(playerID string, outputs []hub.Output) error {
	if a.c.cfg.CDP == nil {
		return nil
	}
	hostOnly := a.c.keymapPlayers() != PlayersAll
	host := !hostOnly || a.c.isHost(playerID)
	fwd := make([]hub.Output, 0, len(outputs))
	for _, out := range outputs {
		if !host && !isRelease(out) {
			continue
		}
		out.Adapter = a.c.cfg.CDP.Name()
		fwd = append(fwd, out)
	}
	if len(fwd) == 0 {
		return nil
	}
	return a.c.cfg.CDP.Deliver(playerID, fwd)
}

func isRelease(out hub.Output) bool {
	switch out.Kind {
	case hub.KindButton:
		v, ok := out.Value.(bool)
		return ok && !v
	case "release":
		return true
	case hub.KindState:
		st, ok := out.Value.(protocol.InputState)
		return ok && st == (protocol.InputState{})
	}
	return false
}

// vpadsAdapter sends native-session frames to the virtual pads. With no
// attachments a player drives the pad of their own slot; once the app
// attaches players, only attached players drive pads.
type vpadsAdapter struct{ c *Controller }

func (vpadsAdapter) Name() string { return VirtualPadsAdapterName }

func (a vpadsAdapter) Deliver(playerID string, outputs []hub.Output) error {
	pads := a.c.cfg.Pads
	if pads == nil {
		return nil
	}
	a.c.mu.Lock()
	appID := ""
	if a.c.native != nil {
		appID = a.c.native.appID
	}
	set := a.c.attach[appID]
	att, attached := set[playerID]
	anyAttached := len(set) > 0
	a.c.mu.Unlock()

	fwd := make([]hub.Output, 0, len(outputs))
	for _, out := range outputs {
		switch {
		case out.Target == "pad" && out.Kind == hub.KindTouch:
			// Surface touches reach only a pad whose attachment asked for them.
			if !attached || att.Touch != "surface" {
				continue
			}
			out.Target = fmt.Sprintf("slot:%d", att.Slot)
		case out.Target == "pad":
			switch {
			case attached:
				out.Target = fmt.Sprintf("slot:%d", att.Slot)
				if att.PointerStick {
					out.Target += "/pointer-stick"
				}
				if att.Touch == "pointer" {
					out.Target += "/touch-pointer"
				}
			case anyAttached:
				continue // a spectator: the app attached other players
			default:
				out.Target = "player"
			}
		case strings.HasPrefix(out.Target, "system:"):
		default:
			continue
		}
		out.Adapter = pads.Name()
		fwd = append(fwd, out)
	}
	if len(fwd) == 0 {
		return nil
	}
	return pads.Deliver(playerID, fwd)
}

// DSUSlots is how many player pads the DSU backend serves.
const DSUSlots = 4

// DSUResolver names DSU pads as "host:port/slot". It implements
// native.VirtualPadResolver ({{virtualPad:n}}) and native.SystemPadResolver
// ({{systemPad}}).
type DSUResolver struct {
	Players string // player pads server, e.g. 127.0.0.1:26760
	System  string // system pad server, e.g. 127.0.0.1:26761
}

// VirtualPad returns pad n's address.
func (r DSUResolver) VirtualPad(_ string, n int) (string, error) {
	if n < 0 || n >= DSUSlots {
		return "", fmt.Errorf("virtual pad %d does not exist (DSU pads are 0-%d)", n, DSUSlots-1)
	}
	if r.Players == "" {
		return "", fmt.Errorf("virtual pads are not available")
	}
	return fmt.Sprintf("%s/%d", r.Players, n), nil
}

// SystemPad returns the system pad's address.
func (r DSUResolver) SystemPad(string) (string, error) {
	if r.System == "" {
		return "", fmt.Errorf("the system pad is not available")
	}
	return r.System + "/0", nil
}
