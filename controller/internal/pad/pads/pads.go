// Package pads is the virtual-pad adapter of the input hub: players' input
// frames become DSU pads that native programs read (D5).
//
// Two DSU servers: the player server (slots 0–3 = players 1–4, UDP 26760 by
// default) and the system pad server (slot 0, UDP 26761), whose buttons the
// active app binds to its runtime's hotkeys.
//
// Route targets (hub.Route.Target) this adapter accepts:
//
//	player               whole frame (KindState) → the pad of the player's slot
//	player/pointer-stick same, with the pointer axes as the right stick
//	slot:N               whole frame → pad N (0–3), whatever the player's slot
//	slot:N/pointer-stick same, with the pointer axes as the right stick
//	slot:N               a touch frame (KindTouch) → pad N's touch points
//	slot:N/touch-pointer a state frame whose pointer axes also drive touch
//	                     point 0, always active (a pointing device on a pad
//	                     whose program reads touch)
//
// Suffixes combine ("slot:1/pointer-stick/touch-pointer"). A state frame
// without /touch-pointer keeps the touch points that touch frames set.
//
//	system               whole frame → the system pad
//	system:<Button>      one button (KindButton bool, or press/release/tap
//	                     events) → that button on the system pad
package pads

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/pad/dsu"
	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// Name is the adapter name used in routes.
const Name = "pads"

// Default listen addresses.
const (
	DefaultPlayersAddr = "127.0.0.1:26760"
	DefaultSystemAddr  = "127.0.0.1:26761"
)

const numSlots = 4

// Sink is a DSU server (or a test double).
type Sink interface {
	SetSlot(slot int, state dsu.PadState)
	DisconnectSlot(slot int)
}

// SlotFunc returns a player's pad index (0–3) or false for spectators.
type SlotFunc func(playerID string) (int, bool)

// Adapter implements hub.Adapter.
type Adapter struct {
	players Sink
	system  Sink
	slotOf  SlotFunc
	now     func() time.Time
	start   time.Time

	mu      sync.Mutex
	clocks  map[string]*motionClock
	origin  map[string][2]int16
	buttons map[string]uint32
	owned   map[int]string // slot → player that last wrote it
	sysBits uint32
	slots   [numSlots]slotTouch

	delivered map[string]deliveryStats // debugging: frames per player
}

// slotTouch is what the adapter remembers about one player pad so touch and
// state frames, which arrive separately, can be merged into one DSU packet.
type slotTouch struct {
	last    dsu.PadState      // last state written, without touch
	surface [2]dsu.TouchPoint // from touch frames
	pointer dsu.TouchPoint    // from the pointer axes (/touch-pointer)
	nextID  uint8
}

type deliveryStats struct {
	Frames uint64    `json:"frames"`
	Errors uint64    `json:"errors"`
	Last   time.Time `json:"last"`
	Error  string    `json:"error,omitempty"`
}

// New builds an adapter over two sinks. Either sink may be nil.
func New(players, system Sink, slotOf SlotFunc) *Adapter {
	return &Adapter{
		players: players,
		system:  system,
		slotOf:  slotOf,
		now:     time.Now,
		start:   time.Now(),
		clocks:  map[string]*motionClock{},
		origin:  map[string][2]int16{},
		buttons: map[string]uint32{},
		owned:   map[int]string{},

		delivered: map[string]deliveryStats{},
	}
}

// Listen starts both DSU servers and returns an adapter writing to them.
// They stop when ctx ends. If one address cannot be bound the other still
// runs and the error says which failed.
func Listen(ctx context.Context, playersAddr, systemAddr string, slotOf SlotFunc) (*Adapter, error) {
	var errs []error
	var players, system Sink
	if srv, err := dsu.NewServer(playersAddr); err != nil {
		errs = append(errs, fmt.Errorf("player pads on %s: %w", playersAddr, err))
	} else {
		players = srv
		go run(ctx, srv, "player pads")
		log.Printf("pad: DSU player pads on %s", srv.Addr())
	}
	if srv, err := dsu.NewServer(systemAddr); err != nil {
		errs = append(errs, fmt.Errorf("system pad on %s: %w", systemAddr, err))
	} else {
		system = srv
		go run(ctx, srv, "system pad")
		log.Printf("pad: DSU system pad on %s", srv.Addr())
	}
	return New(players, system, slotOf), errors.Join(errs...)
}

func run(ctx context.Context, srv *dsu.Server, what string) {
	if err := srv.Run(ctx); err != nil {
		log.Printf("pad: DSU %s stopped: %v", what, err)
	}
}

// Name implements hub.Adapter.
func (a *Adapter) Name() string { return Name }

// Deliver implements hub.Adapter.
func (a *Adapter) Deliver(playerID string, outputs []hub.Output) error {
	var errs []error
	for _, out := range outputs {
		if err := a.deliverOne(playerID, out); err != nil {
			errs = append(errs, err)
		}
	}
	err := errors.Join(errs...)
	a.mu.Lock()
	d := a.delivered[playerID]
	d.Frames++
	d.Last = a.now()
	if err != nil {
		d.Errors++
		d.Error = err.Error()
	}
	a.delivered[playerID] = d
	a.mu.Unlock()
	return err
}

// Stats is a debugging snapshot of both DSU servers and what the hub sent.
func (a *Adapter) Stats() map[string]any {
	type statser interface{ Stats() dsu.Stats }
	out := map[string]any{}
	if s, ok := a.players.(statser); ok {
		out["player_pads"] = s.Stats()
	} else {
		out["player_pads"] = "not listening"
	}
	if s, ok := a.system.(statser); ok {
		out["system_pad"] = s.Stats()
	} else {
		out["system_pad"] = "not listening"
	}
	a.mu.Lock()
	delivered := map[string]deliveryStats{}
	for id, d := range a.delivered {
		delivered[id] = d
	}
	owned := map[string]string{}
	for slot, id := range a.owned {
		owned[strconv.Itoa(slot)] = id
	}
	a.mu.Unlock()
	out["delivered"] = delivered
	out["slot_owners"] = owned
	return out
}

func (a *Adapter) deliverOne(playerID string, out hub.Output) error {
	target := out.Target
	var pointerStick, touchPointer bool
	for {
		if t, ok := strings.CutSuffix(target, "/pointer-stick"); ok {
			target, pointerStick = t, true
		} else if t, ok := strings.CutSuffix(target, "/touch-pointer"); ok {
			target, touchPointer = t, true
		} else {
			break
		}
	}
	switch {
	case out.Kind == hub.KindTouch && (target == "player" || strings.HasPrefix(target, "slot:")):
		t, ok := out.Value.(protocol.TouchState)
		if !ok {
			return fmt.Errorf("pads: %s needs a touch frame", out.Target)
		}
		slot, ok := a.resolveSlot(playerID, target)
		if !ok {
			return nil
		}
		a.setTouch(playerID, slot, t)
		return nil
	case target == "player" || strings.HasPrefix(target, "slot:"):
		state, ok := out.Value.(protocol.InputState)
		if out.Kind != hub.KindState || !ok {
			return fmt.Errorf("pads: %s needs a state route", out.Target)
		}
		slot, ok := a.resolveSlot(playerID, target)
		if !ok {
			return nil // spectators have no pad
		}
		ps := a.convert(playerID, state, pointerStick)
		a.mu.Lock()
		a.owned[slot] = playerID
		st := &a.slots[slot]
		st.last = ps
		if touchPointer {
			ps.Touches = [2]dsu.TouchPoint{a.pointerTouch(playerID, st, state)}
		} else {
			st.pointer = dsu.TouchPoint{}
			ps.Touches = st.surface
		}
		if a.players != nil {
			a.players.SetSlot(slot, ps)
		}
		a.mu.Unlock()
		return nil
	case target == "system":
		state, ok := out.Value.(protocol.InputState)
		if out.Kind != hub.KindState || !ok {
			return errors.New("pads: system needs a state route")
		}
		if a.system != nil {
			a.system.SetSlot(0, a.convert(playerID, state, pointerStick))
		}
		return nil
	case strings.HasPrefix(target, "system:"):
		bit, ok := hub.ButtonBit(strings.TrimPrefix(target, "system:"))
		if !ok {
			return fmt.Errorf("pads: unknown system button %q", target)
		}
		down, ok := level(out)
		if !ok {
			return nil
		}
		a.SetSystemButton(bit, down)
		if out.Kind == "tap" {
			a.SetSystemButton(bit, false)
		}
		return nil
	}
	return fmt.Errorf("pads: unknown target %q", out.Target)
}

// DSU's touch area, in the units a client reads x and y in.
const (
	touchW = 1919
	touchH = 941
)

// newContact gives a point that just went down a fresh id. a.mu held.
func (st *slotTouch) newContact() uint8 {
	st.nextID++
	if st.nextID == 0 {
		st.nextID = 1
	}
	return st.nextID
}

// pointerTouch maps the frame's pointer axes onto touch point 0, always
// active. a.mu held.
func (a *Adapter) pointerTouch(playerID string, st *slotTouch, state protocol.InputState) dsu.TouchPoint {
	if !st.pointer.Active {
		st.pointer = dsu.TouchPoint{Active: true, ID: st.newContact()}
	}
	// Same Recenter origin as /pointer-stick, so one recenter press
	// re-zeroes both.
	o := a.origin[playerID]
	st.pointer.X = scaleAxis(clampAxis(int32(state.PX)-int32(o[0])), touchW)
	st.pointer.Y = scaleAxis(clampAxis(int32(state.PY)-int32(o[1])), touchH)
	return st.pointer
}

// scaleAxis maps -32767..32767 onto 0..max.
func scaleAxis(v int16, max int) uint16 {
	n := (int(v) + 32767) * max / 65534
	if n < 0 {
		n = 0
	}
	if n > max {
		n = max
	}
	return uint16(n)
}

// setTouch applies a touch frame to a slot and re-sends the slot's pad.
func (a *Adapter) setTouch(playerID string, slot int, t protocol.TouchState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := &a.slots[slot]
	for i, p := range t.Points {
		cur := &st.surface[i]
		if !p.Down {
			*cur = dsu.TouchPoint{ID: cur.ID}
			continue
		}
		if !cur.Active {
			cur.ID = st.newContact()
		}
		cur.Active = true
		cur.X = uint16(uint32(p.X) * touchW / 65535)
		cur.Y = uint16(uint32(p.Y) * touchH / 65535)
	}
	a.owned[slot] = playerID
	ps := st.last
	ps.Touches = st.surface
	if a.players != nil {
		a.players.SetSlot(slot, ps)
	}
}

func level(out hub.Output) (bool, bool) {
	switch out.Kind {
	case hub.KindButton:
		v, ok := out.Value.(bool)
		return v, ok
	case "press", "tap":
		return true, true
	case "release":
		return false, true
	}
	return false, false
}

// SetSystemButton holds or releases one button on the system pad. Apps use it
// for runtime hotkeys (pause, save state, connect a controller).
func (a *Adapter) SetSystemButton(bit uint32, down bool) {
	a.mu.Lock()
	if down {
		a.sysBits |= bit
	} else {
		a.sysBits &^= bit
	}
	bits := a.sysBits
	a.mu.Unlock()
	if a.system != nil {
		a.system.SetSlot(0, dsu.FromInput(protocol.InputState{Buttons: bits}, 0, dsu.InputOptions{}))
	}
}

func (a *Adapter) resolveSlot(playerID, target string) (int, bool) {
	if target == "player" {
		if a.slotOf == nil {
			return 0, false
		}
		slot, ok := a.slotOf(playerID)
		return slot, ok && slot >= 0 && slot < numSlots
	}
	slot, err := strconv.Atoi(strings.TrimPrefix(target, "slot:"))
	return slot, err == nil && slot >= 0 && slot < numSlots
}

// convert stamps the frame, applies the Recenter pointer origin, and maps it.
func (a *Adapter) convert(playerID string, state protocol.InputState, pointerStick bool) dsu.PadState {
	now := a.now()
	a.mu.Lock()
	clock := a.clocks[playerID]
	if clock == nil {
		clock = &motionClock{}
		a.clocks[playerID] = clock
	}
	ts := clock.stamp(uint64(now.Sub(a.start)/time.Microsecond), state.TMs)
	prev := a.buttons[playerID]
	a.buttons[playerID] = state.Buttons
	if state.Buttons&protocol.ButtonRecenter != 0 && prev&protocol.ButtonRecenter == 0 {
		a.origin[playerID] = [2]int16{state.PX, state.PY}
	}
	o := a.origin[playerID]
	a.mu.Unlock()
	state.PX = clampAxis(int32(state.PX) - int32(o[0]))
	state.PY = clampAxis(int32(state.PY) - int32(o[1]))
	return dsu.FromInput(state, ts, dsu.InputOptions{PointerStick: pointerStick})
}

func clampAxis(v int32) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32767 {
		return -32767
	}
	return int16(v)
}

// Release disconnects every pad a player last wrote and forgets their
// motion clock and pointer origin. Call it when the player leaves.
func (a *Adapter) Release(playerID string) {
	a.mu.Lock()
	delete(a.clocks, playerID)
	delete(a.origin, playerID)
	delete(a.buttons, playerID)
	var slots []int
	for slot, owner := range a.owned {
		if owner == playerID {
			slots = append(slots, slot)
			delete(a.owned, slot)
		}
	}
	for _, slot := range slots {
		a.slots[slot] = slotTouch{}
	}
	a.mu.Unlock()
	if a.players == nil {
		return
	}
	for _, slot := range slots {
		a.players.DisconnectSlot(slot)
	}
}

// motionClock turns the pad's millisecond clock into DSU microsecond
// timestamps on the kiosk's clock. Deltas between frames come from the pad
// (so network jitter does not distort gyro integration); the base follows
// the kiosk clock, and the stamp re-anchors when the pad clock jumps (reload,
// wrap) or drifts more than maxDrift from the kiosk. Stamps always increase.
type motionClock struct {
	have    bool
	lastPad uint32
	lastUS  uint64
}

const (
	maxPadStep = 1000 // ms; a larger jump re-anchors
	maxDrift   = 500_000
)

func (c *motionClock) stamp(kioskUS uint64, padMs uint32) uint64 {
	if kioskUS == 0 {
		kioskUS = 1
	}
	if !c.have {
		c.have = true
		c.lastPad = padMs
		c.lastUS = kioskUS
		return c.lastUS
	}
	delta := padMs - c.lastPad // wraps with the u32 clock
	c.lastPad = padMs
	var ts uint64
	if int32(delta) < 0 || delta > maxPadStep {
		ts = kioskUS
	} else {
		ts = c.lastUS + uint64(delta)*1000
		if ts > kioskUS+maxDrift || ts+maxDrift < kioskUS {
			ts = kioskUS
		}
	}
	if ts <= c.lastUS {
		ts = c.lastUS + 1
	}
	c.lastUS = ts
	return ts
}
