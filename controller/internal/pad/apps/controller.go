// Package apps connects Yui apps and native sessions to the input hub
// (plans 05 and 07). It is generic: nothing here knows about a console or a
// program.
//
//   - App focus: an open app that declares `controllers` in its manifest is
//     pushed onto the focus stack's app layer. Its phone layouts come from the
//     overlay (ctx.controllers.layout); events go back to the overlay as
//     `pad.app.input` through a per-app adapter. A manifest keymap instead
//     maps phone controls to keys and mouse input in the page (CDP).
//   - Native sessions: a running native program gets the native layer. Its
//     layouts are the owning app's; input frames go to virtual pads (DSU)
//     through the app's attachments (ctx.virtualPads). A watchdog stops the
//     session when no host phone has been connected for a while.
package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"sync"
	"time"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/cdp"
	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// AppInputEvent is the bridge event for input routed to an app. Data is
// Input.
const AppInputEvent = "pad.app.input"

// KeymapAdapterName is the adapter keymap routes name; it forwards to CDP.
const KeymapAdapterName = "keymap"

// VirtualPadsAdapterName is the adapter native-session routes name; it
// resolves attachments and forwards to the virtual pads.
const VirtualPadsAdapterName = "vpads"

// Input is the pad.app.input payload. Kind is an event kind, "state" (Value
// is the input frame), "text" (Value is the text op) or "system" (Target is
// the action).
type Input struct {
	AppID    string `json:"app_id"`
	PlayerID string `json:"player_id"`
	Target   string `json:"target"`
	Kind     string `json:"kind"`
	Value    any    `json:"value,omitempty"`
}

// Stack is the part of *hub.Stack the controller uses.
type Stack interface {
	Push(hub.Layer, hub.Provider) error
	Pop(hub.Layer) error
	At(hub.Layer) hub.Provider
	RegisterAdapter(hub.Adapter) error
	UnregisterAdapter(string)
}

// PadSink is the virtual pad adapter (pads.Adapter).
type PadSink interface {
	hub.Adapter
	SetSystemButton(bit uint32, down bool)
}

// Config holds the controller's dependencies. Nil funcs are skipped.
type Config struct {
	Stack   Stack
	Refresh func()
	Emit    func(event string, data any)
	// IsHost and HostName describe the play session's host.
	IsHost   func(playerID string) bool
	HostName func() string
	// CDP receives keymap output (the page's keyboard and mouse).
	CDP hub.Adapter
	// Pads receives native-session input (virtual pads).
	Pads PadSink
	// Resolver names virtual pads (addresses in attachments).
	Resolver DSUResolver
	// Watchdog: a native session is stopped after NoHostTimeout without a
	// connected host (default 3 minutes), checked every WatchdogTick
	// (default 5 s).
	HasHost       func() bool
	StopNative    func(appID string) error
	NoHostTimeout time.Duration
	WatchdogTick  time.Duration
	Now           func() time.Time
	// ticks replaces the watchdog's ticker in tests.
	ticks func() (<-chan time.Time, func())
}

// Focus is a pad.app.focus request.
type Focus struct {
	AppID  string
	Name   string
	Active bool
	Keymap *Keymap
	// Frames forwards whole input frames to the app (ctx.controllers "input").
	Frames bool
	// Region is the embedded game's frame for an absolute-trackpad keymap.
	Region *cdp.Rect
}

// Attachment ties a player to a virtual pad for an app's native sessions.
type Attachment struct {
	PlayerID     string `json:"player_id"`
	Backend      string `json:"backend"`
	Slot         int    `json:"slot"`
	PointerStick bool   `json:"pointer_stick,omitempty"`
	Touch        string `json:"touch,omitempty"` // "", "surface", "pointer"
	Address      string `json:"address"`
}

// Errors returned to the overlay.
var (
	ErrBadAppID   = errors.New("invalid app id")
	ErrNotOwner   = errors.New("the app does not own the controllers")
	ErrBadLayout  = errors.New("invalid layout")
	ErrBadBackend = errors.New("virtual pad backend must be \"dsu\"")
	ErrBadSlot    = fmt.Errorf("virtual pad slot must be 0-%d", DSUSlots-1)
	ErrBadButton  = errors.New("unknown button")
	ErrBadTouch   = errors.New("virtual pad touch must be \"surface\" or \"pointer\"")
)

var appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,126}[a-z0-9_-]$`)

// Layout limits.
const (
	maxLayoutEntries = 32
	maxLayoutBytes   = 64 << 10
	maxLayoutDepth   = 24
	maxAppsTracked   = 16
)

// Controller owns the app and native layers of the focus stack.
type Controller struct {
	cfg Config

	// opMu serializes focus and session transitions (it is held across
	// Push and Pop). Providers never take it.
	opMu sync.Mutex

	mu      sync.Mutex
	app     *appProvider
	native  *nativeProvider
	layouts map[string]map[string]protocol.Node // app → player id or "*" → tree
	attach  map[string]map[string]Attachment    // app → player id → attachment
	dogStop chan struct{}
}

// New builds a controller and registers its keymap and virtual pad adapters.
func New(cfg Config) (*Controller, error) {
	if cfg.Stack == nil {
		return nil, errors.New("apps: no focus stack")
	}
	if cfg.NoHostTimeout <= 0 {
		cfg.NoHostTimeout = 3 * time.Minute
	}
	if cfg.WatchdogTick <= 0 {
		cfg.WatchdogTick = 5 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	c := &Controller{
		cfg:     cfg,
		layouts: map[string]map[string]protocol.Node{},
		attach:  map[string]map[string]Attachment{},
	}
	if err := cfg.Stack.RegisterAdapter(keymapAdapter{c: c}); err != nil {
		return nil, err
	}
	if err := cfg.Stack.RegisterAdapter(vpadsAdapter{c: c}); err != nil {
		return nil, err
	}
	return c, nil
}

func validAppID(id string) bool { return appIDPattern.MatchString(id) }

// SetFocus pushes, updates, or pops the app layer.
func (c *Controller) SetFocus(f Focus) error {
	if f.Active || f.AppID != "" {
		if !validAppID(f.AppID) {
			return ErrBadAppID
		}
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if !f.Active {
		return c.blurLocked(f.AppID)
	}

	var km *compiledKeymap
	if f.Keymap != nil {
		compiled, err := compileKeymap(*f.Keymap, f.Region, KeymapAdapterName)
		if err != nil {
			return err
		}
		km = &compiled
	}

	c.mu.Lock()
	cur := c.app
	if cur != nil && cur.appID == f.AppID && c.cfg.Stack.At(hub.LayerApp) == hub.Provider(cur) {
		// Same app: update in place, no focus change.
		cur.name = f.Name
		cur.keymap = km
		cur.frames = f.Frames
		c.rebuildRoutesLocked()
		c.mu.Unlock()
		c.refresh()
		return nil
	}
	next := &appProvider{c: c, appID: f.AppID, name: f.Name, keymap: km, frames: f.Frames}
	c.app = next
	c.rebuildRoutesLocked()
	c.mu.Unlock()

	if err := c.cfg.Stack.RegisterAdapter(appAdapter{c: c, appID: f.AppID}); err != nil {
		return err
	}
	if err := c.cfg.Stack.Push(hub.LayerApp, next); err != nil {
		log.Printf("pad apps: focus %s: %v", f.AppID, err)
	}
	if cur != nil && cur.appID != f.AppID {
		c.release(cur.appID)
	}
	// A push below the native layer does not change focus, so the stack
	// does not refresh; refresh anyway (layouts may differ per layer).
	c.refresh()
	return nil
}

// blurLocked pops the app layer when it belongs to appID ("" = any app).
// opMu is held.
func (c *Controller) blurLocked(appID string) error {
	c.mu.Lock()
	cur := c.app
	if cur == nil || (appID != "" && cur.appID != appID) {
		c.mu.Unlock()
		return nil
	}
	c.app = nil
	c.mu.Unlock()
	if c.cfg.Stack.At(hub.LayerApp) == hub.Provider(cur) {
		if err := c.cfg.Stack.Pop(hub.LayerApp); err != nil && !errors.Is(err, hub.ErrLayerEmpty) {
			log.Printf("pad apps: blur %s: %v", cur.appID, err)
		}
	}
	c.release(cur.appID)
	c.refresh()
	return nil
}

// release forgets an app's adapter, layouts and attachments once neither
// layer belongs to it. It runs after the pop, so the neutral frames sent on
// the focus change still reach the adapter.
func (c *Controller) release(appID string) {
	c.mu.Lock()
	if c.ownsLocked(appID) {
		c.mu.Unlock()
		return
	}
	delete(c.layouts, appID)
	delete(c.attach, appID)
	c.mu.Unlock()
	c.cfg.Stack.UnregisterAdapter(appAdapterName(appID))
}

// Owns reports whether the app is focused or owns the native session.
func (c *Controller) Owns(appID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ownsLocked(appID)
}

func (c *Controller) ownsLocked(appID string) bool {
	return (c.app != nil && c.app.appID == appID) || (c.native != nil && c.native.appID == appID)
}

// Focused returns the app on the app layer, or "".
func (c *Controller) Focused() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.app == nil {
		return ""
	}
	return c.app.appID
}

// SetLayouts replaces an app's phone layouts. Keys are player ids, or "*"
// for players without their own. A null or empty tree removes the entry.
func (c *Controller) SetLayouts(appID string, layouts map[string]*protocol.Node) error {
	if !validAppID(appID) {
		return ErrBadAppID
	}
	if len(layouts) > maxLayoutEntries {
		return fmt.Errorf("%w: at most %d entries", ErrBadLayout, maxLayoutEntries)
	}
	clean := make(map[string]protocol.Node, len(layouts))
	for player, tree := range layouts {
		if player == "" || len(player) > 128 {
			return fmt.Errorf("%w: bad player id", ErrBadLayout)
		}
		if tree == nil || tree.Type == "" {
			continue
		}
		if err := checkTree(*tree, 0); err != nil {
			return err
		}
		raw, err := json.Marshal(tree)
		if err != nil || len(raw) > maxLayoutBytes {
			return fmt.Errorf("%w: a layout is larger than %d KiB", ErrBadLayout, maxLayoutBytes>>10)
		}
		clean[player] = *tree
	}
	c.mu.Lock()
	if _, ok := c.layouts[appID]; !ok && len(c.layouts) >= maxAppsTracked && !c.ownsLocked(appID) {
		c.mu.Unlock()
		return ErrNotOwner
	}
	c.layouts[appID] = clean
	active := c.ownsLocked(appID)
	if active {
		c.rebuildRoutesLocked()
	}
	c.mu.Unlock()
	if active {
		c.refresh()
	}
	return nil
}

func checkTree(n protocol.Node, depth int) error {
	if depth > maxLayoutDepth {
		return fmt.Errorf("%w: nested deeper than %d", ErrBadLayout, maxLayoutDepth)
	}
	if n.Type == "" {
		return fmt.Errorf("%w: a node has no type", ErrBadLayout)
	}
	for _, child := range n.Children {
		if err := checkTree(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// layoutFor returns the app's layout for a player, if it sent one.
func (c *Controller) layoutFor(appID string, player protocol.Player) (protocol.Node, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	set := c.layouts[appID]
	if tree, ok := set[player.ID]; ok {
		return tree, true
	}
	tree, ok := set["*"]
	return tree, ok
}

// rebuildRoutesLocked recomputes the providers' cached routes. c.mu held.
func (c *Controller) rebuildRoutesLocked() {
	if p := c.app; p != nil {
		var routes []hub.Route
		if p.keymap != nil {
			routes = append(routes, p.keymap.routes...)
		}
		routes = append(routes, appRoutes(p.appID, c.layouts[p.appID], p.frames)...)
		p.routes = routes
	}
	if p := c.native; p != nil {
		routes := []hub.Route{
			hub.PadRoute(VirtualPadsAdapterName, "pad"),
			{Kind: hub.KindTouch, Adapter: VirtualPadsAdapterName, Target: "pad"},
		}
		p.routes = append(routes, appRoutes(p.appID, c.layouts[p.appID], false)...)
	}
}

// appRoutes routes every control id in the app's layouts, and text, to the
// app's adapter; with frames, the whole input frame too.
func appRoutes(appID string, layouts map[string]protocol.Node, frames bool) []hub.Route {
	name := appAdapterName(appID)
	ids := map[string]bool{}
	for _, tree := range layouts {
		collectIDs(tree, ids)
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	routes := make([]hub.Route, 0, len(sorted)+2)
	for _, id := range sorted {
		routes = append(routes, hub.Route{Control: id, Adapter: name, Target: id})
	}
	routes = append(routes, hub.Route{Kind: hub.KindText, Adapter: name, Target: "text"})
	if frames {
		routes = append(routes, hub.Route{Kind: hub.KindState, Adapter: name, Target: "state"})
	}
	return routes
}

func collectIDs(n protocol.Node, ids map[string]bool) {
	if id, ok := n.Props["id"].(string); ok && id != "" {
		ids[id] = true
	}
	for _, child := range n.Children {
		collectIDs(child, ids)
	}
}

// Haptic and Sound targets are checked against ownership by the caller.

// SessionStarted implements native.Observer: the session gets the native
// layer and a no-host watchdog.
func (c *Controller) SessionStarted(info native.Info) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	p := &nativeProvider{c: c, appID: info.AppID, sessionID: info.ID}
	c.mu.Lock()
	prev := c.native
	c.native = p
	c.rebuildRoutesLocked()
	if c.dogStop != nil {
		close(c.dogStop)
	}
	stop := make(chan struct{})
	c.dogStop = stop
	c.mu.Unlock()

	if err := c.cfg.Stack.RegisterAdapter(appAdapter{c: c, appID: info.AppID}); err != nil {
		log.Printf("pad apps: native %s: %v", info.AppID, err)
	}
	if err := c.cfg.Stack.Push(hub.LayerNative, p); err != nil {
		log.Printf("pad apps: native %s: %v", info.AppID, err)
	}
	if prev != nil && prev.appID != info.AppID {
		c.release(prev.appID)
	}
	go c.watchdog(info.AppID, stop)
}

// SessionEnded implements native.Observer: the native layer is popped.
func (c *Controller) SessionEnded(info native.Info, _ native.Status) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.mu.Lock()
	p := c.native
	if p == nil || p.sessionID != info.ID {
		c.mu.Unlock()
		return
	}
	if c.dogStop != nil {
		close(c.dogStop)
		c.dogStop = nil
	}
	c.mu.Unlock()
	// Pop while the session is still current, so the neutral frames sent on
	// the focus change reach the attached pads.
	if c.cfg.Stack.At(hub.LayerNative) == hub.Provider(p) {
		if err := c.cfg.Stack.Pop(hub.LayerNative); err != nil && !errors.Is(err, hub.ErrLayerEmpty) {
			log.Printf("pad apps: native end %s: %v", info.AppID, err)
		}
	}
	c.mu.Lock()
	if c.native == p {
		c.native = nil
	}
	c.mu.Unlock()
	c.release(info.AppID)
	c.refresh()
}

// NativeApp returns the app that owns the native layer, or "".
func (c *Controller) NativeApp() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.native == nil {
		return ""
	}
	return c.native.appID
}

// watchdog stops the native session once no host has been connected for
// NoHostTimeout. A host connecting resets the clock.
func (c *Controller) watchdog(appID string, stop <-chan struct{}) {
	var ticks <-chan time.Time
	if c.cfg.ticks != nil {
		ch, done := c.cfg.ticks()
		defer done()
		ticks = ch
	} else {
		ticker := time.NewTicker(c.cfg.WatchdogTick)
		defer ticker.Stop()
		ticks = ticker.C
	}
	lastHost := c.cfg.Now()
	for {
		select {
		case <-stop:
			return
		case <-ticks:
		}
		now := c.cfg.Now()
		if c.cfg.HasHost != nil && c.cfg.HasHost() {
			lastHost = now
			continue
		}
		if now.Sub(lastHost) < c.cfg.NoHostTimeout {
			continue
		}
		log.Printf("pad apps: no host phone for %s; stopping %s's native session", c.cfg.NoHostTimeout, appID)
		if c.cfg.StopNative != nil {
			if err := c.cfg.StopNative(appID); err != nil {
				log.Printf("pad apps: watchdog stop %s: %v", appID, err)
			}
		}
		return
	}
}

// Attach ties a player to a virtual pad for the app's native sessions. A
// slot belongs to one player per app: attaching another player moves it.
func (c *Controller) Attach(appID string, a Attachment) (Attachment, error) {
	if !validAppID(appID) {
		return Attachment{}, ErrBadAppID
	}
	if a.PlayerID == "" || len(a.PlayerID) > 128 {
		return Attachment{}, errors.New("player_id is required")
	}
	if a.Backend == "" {
		a.Backend = "dsu"
	}
	if a.Backend != "dsu" {
		return Attachment{}, ErrBadBackend
	}
	if a.Slot < 0 || a.Slot >= DSUSlots {
		return Attachment{}, ErrBadSlot
	}
	if a.Touch != "" && a.Touch != "surface" && a.Touch != "pointer" {
		return Attachment{}, ErrBadTouch
	}
	addr, err := c.cfg.Resolver.VirtualPad(appID, a.Slot)
	if err != nil {
		return Attachment{}, err
	}
	a.Address = addr
	c.mu.Lock()
	set := c.attach[appID]
	if set == nil {
		if len(c.attach) >= maxAppsTracked && !c.ownsLocked(appID) {
			c.mu.Unlock()
			return Attachment{}, ErrNotOwner
		}
		set = map[string]Attachment{}
		c.attach[appID] = set
	}
	var moved []Attachment
	for id, other := range set {
		if id != a.PlayerID && other.Slot == a.Slot {
			delete(set, id)
			moved = append(moved, other)
		}
	}
	prev, had := set[a.PlayerID]
	set[a.PlayerID] = a
	c.mu.Unlock()
	if had && prev.Slot != a.Slot {
		c.neutral(prev)
	}
	for _, m := range moved {
		c.neutral(m)
	}
	return a, nil
}

// Detach removes a player's attachment and releases their pad.
func (c *Controller) Detach(appID, playerID string) error {
	if !validAppID(appID) {
		return ErrBadAppID
	}
	c.mu.Lock()
	a, ok := c.attach[appID][playerID]
	delete(c.attach[appID], playerID)
	c.mu.Unlock()
	if ok {
		c.neutral(a)
	}
	return nil
}

// Attachments lists the app's attachments by slot.
func (c *Controller) Attachments(appID string) []Attachment {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Attachment, 0, len(c.attach[appID]))
	for _, a := range c.attach[appID] {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// neutral releases everything on an attachment's pad.
func (c *Controller) neutral(a Attachment) {
	if c.cfg.Pads == nil {
		return
	}
	target := fmt.Sprintf("slot:%d", a.Slot)
	outs := []hub.Output{
		{Adapter: c.cfg.Pads.Name(), Target: target, Kind: hub.KindState, Value: protocol.InputState{}},
	}
	if a.Touch == "surface" {
		// A neutral state frame already drops pointer touch; surface touch
		// is separate state and needs its own release.
		outs = append(outs, hub.Output{Adapter: c.cfg.Pads.Name(), Target: target, Kind: hub.KindTouch, Value: protocol.TouchState{}})
	}
	_ = c.cfg.Pads.Deliver(a.PlayerID, outs)
}

// SystemButton presses, releases or taps a button on the system pad. The
// app must own the controllers.
func (c *Controller) SystemButton(appID, button, action string) error {
	if !c.Owns(appID) {
		return ErrNotOwner
	}
	bit, ok := hub.ButtonBit(button)
	if !ok {
		return ErrBadButton
	}
	if c.cfg.Pads == nil {
		return errors.New("virtual pads are not available")
	}
	switch action {
	case "press":
		c.cfg.Pads.SetSystemButton(bit, true)
	case "release":
		c.cfg.Pads.SetSystemButton(bit, false)
	case "tap", "":
		c.cfg.Pads.SetSystemButton(bit, true)
		// Long enough for a program polling at 60 Hz to see it.
		time.AfterFunc(120*time.Millisecond, func() { c.cfg.Pads.SetSystemButton(bit, false) })
	default:
		return errors.New("action must be press, release or tap")
	}
	return nil
}

func (c *Controller) refresh() {
	if c.cfg.Refresh != nil {
		c.cfg.Refresh()
	}
}

func (c *Controller) emit(event string, data any) {
	if c.cfg.Emit != nil {
		c.cfg.Emit(event, data)
	}
}

func (c *Controller) isHost(playerID string) bool {
	return c.cfg.IsHost != nil && c.cfg.IsHost(playerID)
}

func (c *Controller) hostName() string {
	if c.cfg.HostName == nil {
		return ""
	}
	return c.cfg.HostName()
}

// keymapPlayers returns the focused app's keymap player mode, or "".
func (c *Controller) keymapPlayers() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.app == nil || c.app.keymap == nil {
		return ""
	}
	return c.app.keymap.players
}
