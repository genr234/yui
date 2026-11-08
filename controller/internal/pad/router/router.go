// Package router wires a play session to the input hub: player input,
// events, text and system messages go into the focus stack; layouts, haptics
// and system notices go back to the phones; Home goes to the overlay.
//
// One Router lives as long as the controller's bridge (it survives play
// sessions), so providers that apps push stay in place when a session ends
// and a new one starts. A session is attached while it runs and detached
// when it ends; detaching forgets every player.
package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"sync"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
	"kiosk/controller/internal/pad/session"
	"kiosk/controller/internal/pad/shell"
)

// Bridge push events (contracts §4).
const (
	// HomeEvent: a player pressed Home (bit 10 or system "home"). The
	// overlay comes forward. Data is Home.
	HomeEvent = "pad.home"
	// KeyboardEvent: whether any phone currently shows a keyboard. The
	// overlay hides its on-screen keyboard while true. Data is Keyboard.
	KeyboardEvent = "pad.keyboard"
	// AppInputEvent: outputs routed to the apps adapter, for ctx.controllers.
	// Data is AppInput.
	AppInputEvent = "pad.app.input"
)

// Home is the pad.home payload. Via is "button" or "system".
type Home struct {
	PlayerID string `json:"player_id"`
	Via      string `json:"via"`
}

// Keyboard is the pad.keyboard payload.
type Keyboard struct {
	Active bool `json:"active"`
}

// AppInput is the pad.app.input payload.
type AppInput struct {
	PlayerID string `json:"player_id"`
	Target   string `json:"target"`
	Kind     string `json:"kind"`
	Value    any    `json:"value,omitempty"`
}

// Session is the part of session.Manager the router uses.
type Session interface {
	SetControlHandler(fn func(playerID string, msg any))
	SetInputHandler(fn func(playerID string, state protocol.InputState))
	SetPlayerHandler(fn func(ev session.PlayerEvent))
	SendControl(playerID string, msg any) error
}

// Pointer is the CDP adapter as the router sees it.
type Pointer interface {
	hub.Adapter
	Hide()
	Navigate(url string)
}

// Pads is the virtual pad adapter as the router sees it.
type Pads interface {
	hub.Adapter
	Release(playerID string)
}

// Config builds a Router. Every field is optional.
type Config struct {
	Emit    func(event string, data any)
	Pointer Pointer
	Pads    Pads
	// StartURL is the page the remote's Home button loads.
	StartURL string
}

// Router owns the focus stack and the session binding.
type Router struct {
	cfg   Config
	stack *hub.Stack
	shell *shell.Provider

	mu        sync.Mutex
	sess      Session
	players   map[string]protocol.Player // ready players
	sensitive bool

	// pushMu orders layout sends so versions reach each phone increasing.
	pushMu   sync.Mutex
	version  int
	sent     map[string][]byte
	keyboard bool
}

// New builds the stack with the shell provider at the bottom and registers
// the adapters.
func New(cfg Config) (*Router, error) {
	r := &Router{cfg: cfg, players: map[string]protocol.Player{}, sent: map[string][]byte{}}
	deps := shell.Deps{
		IsHost:   r.IsHost,
		HostName: r.hostName,
		Emit:     r.emit,
		Refresh:  func() { r.Refresh(false) },
		Haptic: func(id string, pattern []int) {
			_ = r.Haptic(id, pattern)
		},
		NavigateHome: func() {
			if cfg.Pointer != nil && cfg.StartURL != "" {
				cfg.Pointer.Navigate(cfg.StartURL)
			}
		},
	}
	if cfg.Pointer != nil {
		deps.Pointer = cfg.Pointer
	}
	r.shell = shell.New(deps)
	stack, err := hub.New(r.shell, r.onHome)
	if err != nil {
		return nil, err
	}
	stack.SetFocusFunc(func(hub.Layer, hub.Provider) { r.Refresh(true) })
	if cfg.Pointer != nil {
		if err := stack.RegisterAdapter(cfg.Pointer); err != nil {
			return nil, err
		}
	}
	if cfg.Pads != nil {
		if err := stack.RegisterAdapter(cfg.Pads); err != nil {
			return nil, err
		}
	}
	if err := stack.RegisterAdapter(appsAdapter{emit: r.emit}); err != nil {
		return nil, err
	}
	r.stack = stack
	return r, nil
}

// Stack is the focus stack, for pushing app and native providers.
func (r *Router) Stack() *hub.Stack { return r.stack }

// Attach binds a running session. A previously attached session is detached.
func (r *Router) Attach(s Session) {
	r.mu.Lock()
	prev := r.sess
	r.mu.Unlock()
	if prev != nil && prev != s {
		r.Detach(prev)
	}
	r.mu.Lock()
	r.sess = s
	r.mu.Unlock()
	s.SetPlayerHandler(r.onPlayer)
	s.SetControlHandler(r.onControl)
	s.SetInputHandler(r.onInput)
}

// Detach unbinds s (if it is attached) and forgets all of its players.
func (r *Router) Detach(s Session) {
	r.mu.Lock()
	if r.sess != s {
		r.mu.Unlock()
		return
	}
	r.sess = nil
	ids := make([]string, 0, len(r.players))
	for id := range r.players {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	s.SetPlayerHandler(nil)
	s.SetControlHandler(nil)
	s.SetInputHandler(nil)
	for _, id := range ids {
		r.forget(id)
	}
	if r.cfg.Pointer != nil {
		r.cfg.Pointer.Hide()
	}
	r.Refresh(false)
}

// SetSensitive stores D9 and tells every phone with a system "sensitive"
// message so it shows "Use the screen for this". The CDP adapter reads the
// flag through Sensitive and drops pointer and text input while it is set.
func (r *Router) SetSensitive(active bool) {
	r.mu.Lock()
	changed := r.sensitive != active
	r.sensitive = active
	ids := r.readyIDs()
	r.mu.Unlock()
	if !changed {
		return
	}
	if active && r.cfg.Pointer != nil {
		r.cfg.Pointer.Hide()
	}
	for _, id := range ids {
		r.sendSensitive(id, active)
	}
}

// Sensitive reports D9.
func (r *Router) Sensitive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sensitive
}

// Haptic vibrates one phone. pattern is in ms, like navigator.vibrate.
func (r *Router) Haptic(playerID string, pattern []int) error {
	if len(pattern) == 0 {
		return errors.New("pad: empty haptic pattern")
	}
	return r.send(playerID, protocol.Haptic{T: "haptic", Pattern: pattern})
}

// IsHost reports whether a ready player is the host.
func (r *Router) IsHost(playerID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.players[playerID]
	return ok && p.Host
}

// SlotOf returns a player's pad index (slot 1–4 → 0–3).
func (r *Router) SlotOf(playerID string) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.players[playerID]
	if !ok || p.Slot == nil || *p.Slot < 1 || *p.Slot > 4 {
		return 0, false
	}
	return *p.Slot - 1, true
}

// Color returns a ready player's colour name.
func (r *Router) Color(playerID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.players[playerID].Color
}

func (r *Router) hostName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.players {
		if p.Host {
			return p.Name
		}
	}
	return ""
}

// readyIDs lists ready players. r.mu held.
func (r *Router) readyIDs() []string {
	ids := make([]string, 0, len(r.players))
	for id := range r.players {
		ids = append(ids, id)
	}
	return ids
}

func (r *Router) onPlayer(ev session.PlayerEvent) {
	id := ev.Player.ID
	if id == "" {
		return
	}
	switch ev.Kind {
	case session.PlayerReady:
		r.mu.Lock()
		r.players[id] = ev.Player
		sensitive := r.sensitive
		r.mu.Unlock()
		// The phone may be new or reconnecting: always send it a layout.
		r.pushMu.Lock()
		delete(r.sent, id)
		r.pushMu.Unlock()
		if sensitive {
			r.sendSensitive(id, true)
		}
		r.Refresh(false)
	case session.PlayerUpdated:
		r.mu.Lock()
		_, ready := r.players[id]
		if ready {
			r.players[id] = ev.Player
		}
		r.mu.Unlock()
		if ready {
			// A host change swaps the remote and the waiting layout.
			r.Refresh(false)
		}
	case session.PlayerLeft:
		r.forget(id)
		r.Refresh(false)
	}
}

func (r *Router) forget(id string) {
	r.mu.Lock()
	delete(r.players, id)
	r.mu.Unlock()
	r.pushMu.Lock()
	delete(r.sent, id)
	r.pushMu.Unlock()
	r.shell.Forget(id)
	if err := r.stack.Forget(id); err != nil {
		log.Printf("pad hub: forget %s: %v", id, err)
	}
	if r.cfg.Pads != nil {
		r.cfg.Pads.Release(id)
	}
}

func (r *Router) onControl(playerID string, msg any) {
	var err error
	switch m := msg.(type) {
	case protocol.Event:
		err = r.stack.Event(playerID, m)
	case protocol.Text:
		err = r.stack.Text(playerID, m)
	case protocol.System:
		if m.Action == "sensitive" {
			return // kiosk → pad only
		}
		err = r.stack.System(playerID, m)
	default:
		return
	}
	if err != nil {
		log.Printf("pad hub: %s: %v", playerID, err)
	}
}

func (r *Router) onInput(playerID string, state protocol.InputState) {
	if err := r.stack.Input(playerID, state); err != nil {
		log.Printf("pad hub: input %s: %v", playerID, err)
	}
}

func (r *Router) onHome(playerID string, via hub.HomeVia) {
	r.emit(HomeEvent, Home{PlayerID: playerID, Via: string(via)})
}

// Refresh sends every ready player the top provider's layout. With force,
// every player gets it; otherwise only players whose tree changed. Each
// send carries a new, higher version.
func (r *Router) Refresh(force bool) {
	r.mu.Lock()
	sess := r.sess
	players := make([]protocol.Player, 0, len(r.players))
	for _, p := range r.players {
		players = append(players, p)
	}
	r.mu.Unlock()

	r.pushMu.Lock()
	defer r.pushMu.Unlock()
	keyboard := false
	for _, p := range players {
		tree := r.stack.Layout(p)
		if tree.Type == "" {
			tree = protocol.Node{Type: "column"}
		}
		if hub.ContainsType(tree, "keyboard") {
			keyboard = true
		}
		raw, err := json.Marshal(tree)
		if err != nil {
			log.Printf("pad hub: layout for %s: %v", p.ID, err)
			continue
		}
		if !force && bytes.Equal(r.sent[p.ID], raw) {
			continue
		}
		if sess == nil {
			continue
		}
		r.version++
		if err := sess.SendControl(p.ID, protocol.Layout{T: "layout", Version: r.version, Tree: tree}); err != nil {
			log.Printf("pad hub: layout to %s: %v", p.ID, err)
			delete(r.sent, p.ID)
			continue
		}
		r.sent[p.ID] = raw
	}
	if keyboard != r.keyboard {
		r.keyboard = keyboard
		r.emit(KeyboardEvent, Keyboard{Active: keyboard})
	}
}

func (r *Router) sendSensitive(playerID string, active bool) {
	if err := r.send(playerID, protocol.System{T: "system", Action: "sensitive", Value: active}); err != nil {
		log.Printf("pad hub: sensitive to %s: %v", playerID, err)
	}
}

func (r *Router) send(playerID string, msg any) error {
	r.mu.Lock()
	sess := r.sess
	r.mu.Unlock()
	if sess == nil {
		return errors.New("pad: no play session")
	}
	return sess.SendControl(playerID, msg)
}

func (r *Router) emit(event string, data any) {
	if r.cfg.Emit != nil {
		r.cfg.Emit(event, data)
	}
}

// appsAdapter forwards routed outputs to the overlay, where ctx.controllers
// (plan 07) hands them to the active app.
type appsAdapter struct {
	emit func(event string, data any)
}

// AppsAdapterName is the apps adapter's route name.
const AppsAdapterName = "apps"

func (appsAdapter) Name() string { return AppsAdapterName }

func (a appsAdapter) Deliver(playerID string, outputs []hub.Output) error {
	for _, out := range outputs {
		a.emit(AppInputEvent, AppInput{PlayerID: playerID, Target: out.Target, Kind: out.Kind, Value: out.Value})
	}
	return nil
}
