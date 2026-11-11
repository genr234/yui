package router

import (
	"log"
	"sort"
	"sync"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

// Bridge push events for player reactions and pokes (contracts §4).
const (
	// ReactionEvent: a player sent an emote. Data is Reaction.
	ReactionEvent = "pad.reaction"
	// PokeEvent: a player poked another. Data is PokeNote.
	PokeEvent = "pad.poke"
	// InputEvent: the buttons of players whose buttons changed, at most about
	// 10 Hz. Data is InputNote.
	InputEvent = "pad.input"
)

// Limits. Anything over them is dropped silently.
const (
	reactEvery = 700 * time.Millisecond
	pokeEvery  = 3 * time.Second
	// inputEvery is the shortest gap between two pad.input emits.
	inputEvery = 100 * time.Millisecond
)

// Reaction is the pad.reaction payload.
type Reaction struct {
	PlayerID string `json:"player_id"`
	Emote    string `json:"emote"`
}

// PokeNote is the pad.poke payload.
type PokeNote struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PlayerButtons is one entry of pad.input.
type PlayerButtons struct {
	PlayerID string `json:"player_id"`
	Buttons  uint32 `json:"buttons"`
}

// InputNote is the pad.input payload.
type InputNote struct {
	Players []PlayerButtons `json:"players"`
}

// social holds the reaction, poke and button-tap state. Its own mutex keeps
// the input path off the router's lock for anything but the button compare.
type social struct {
	mu        sync.Mutex
	now       func() time.Time
	lastReact map[string]time.Time // per sender
	lastPoker map[string]time.Time // per sender
	lastPoked map[string]time.Time // per target
	pokesOff  map[string]bool

	buttons   map[string]uint32 // latest per player
	emitted   map[string]uint32 // last emitted per player
	dirty     bool
	lastEmit  time.Time
	flushTime *time.Timer
}

func newSocial() *social {
	return &social{
		now:       time.Now,
		lastReact: map[string]time.Time{},
		lastPoker: map[string]time.Time{},
		lastPoked: map[string]time.Time{},
		pokesOff:  map[string]bool{},
		buttons:   map[string]uint32{},
		emitted:   map[string]uint32{},
	}
}

// reset forgets everything (the session ended).
func (s *social) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReact = map[string]time.Time{}
	s.lastPoker = map[string]time.Time{}
	s.lastPoked = map[string]time.Time{}
	s.pokesOff = map[string]bool{}
	s.buttons = map[string]uint32{}
	s.emitted = map[string]uint32{}
	s.dirty = false
	if s.flushTime != nil {
		s.flushTime.Stop()
		s.flushTime = nil
	}
}

// forget drops one player's button state (the opt-out stays for the session).
func (s *social) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buttons, id)
	delete(s.emitted, id)
	delete(s.lastReact, id)
}

func (r *Router) onReact(playerID string, m protocol.React) {
	r.mu.Lock()
	_, ready := r.players[playerID]
	sensitive := r.sensitive
	r.mu.Unlock()
	if !ready || sensitive || !protocol.IsEmote(m.Emote) {
		return
	}
	s := r.soc
	s.mu.Lock()
	now := s.now()
	if last, ok := s.lastReact[playerID]; ok && now.Sub(last) < reactEvery {
		s.mu.Unlock()
		return
	}
	s.lastReact[playerID] = now
	s.mu.Unlock()
	r.emit(ReactionEvent, Reaction{PlayerID: playerID, Emote: m.Emote})
}

func (r *Router) onPoke(from string, m protocol.Poke) {
	if m.To == "" || m.To == from {
		return
	}
	r.mu.Lock()
	_, okFrom := r.players[from]
	_, okTo := r.players[m.To]
	r.mu.Unlock()
	if !okFrom || !okTo {
		return
	}
	s := r.soc
	s.mu.Lock()
	if s.pokesOff[m.To] {
		s.mu.Unlock()
		return
	}
	now := s.now()
	if last, ok := s.lastPoker[from]; ok && now.Sub(last) < pokeEvery {
		s.mu.Unlock()
		return
	}
	if last, ok := s.lastPoked[m.To]; ok && now.Sub(last) < pokeEvery {
		s.mu.Unlock()
		return
	}
	s.lastPoker[from] = now
	s.lastPoked[m.To] = now
	s.mu.Unlock()
	if err := r.send(m.To, protocol.Poked{T: "poked", From: from}); err != nil {
		log.Printf("pad hub: poked to %s: %v", m.To, err)
	}
	r.emit(PokeEvent, PokeNote{From: from, To: m.To})
}

// setPokes records a player's opt-in or opt-out and refreshes the rosters,
// which leave opted-out players out.
func (r *Router) setPokes(playerID string, value any) {
	on, ok := value.(bool)
	if !ok {
		return
	}
	s := r.soc
	s.mu.Lock()
	was := s.pokesOff[playerID]
	if on {
		delete(s.pokesOff, playerID)
	} else {
		s.pokesOff[playerID] = true
	}
	changed := was != !on
	s.mu.Unlock()
	if changed {
		r.sendRosters()
	}
}

// sendRosters sends every ready player the others, opted-out ones flagged.
func (r *Router) sendRosters() {
	r.mu.Lock()
	sess := r.sess
	all := make([]protocol.Player, 0, len(r.players))
	for _, p := range r.players {
		all = append(all, p)
	}
	r.mu.Unlock()
	if sess == nil {
		return
	}
	s := r.soc
	s.mu.Lock()
	off := make(map[string]bool, len(s.pokesOff))
	for id := range s.pokesOff {
		off[id] = true
	}
	s.mu.Unlock()
	sort.Slice(all, func(i, j int) bool {
		si, sj := 99, 99
		if all[i].Slot != nil {
			si = *all[i].Slot
		}
		if all[j].Slot != nil {
			sj = *all[j].Slot
		}
		if si != sj {
			return si < sj
		}
		return all[i].ID < all[j].ID
	})
	for _, me := range all {
		peers := make([]protocol.Peer, 0, len(all))
		for _, p := range all {
			if p.ID == me.ID {
				continue
			}
			peers = append(peers, protocol.Peer{ID: p.ID, Name: p.Name, Color: p.Color, Slot: p.Slot, Avatar: p.Avatar, Pokes: !off[p.ID]})
		}
		if err := sess.SendControl(me.ID, protocol.Players{T: "players", Players: peers}); err != nil {
			log.Printf("pad hub: players to %s: %v", me.ID, err)
		}
	}
}

// noteButtons records a player's buttons for the throttled pad.input emit.
// It does no I/O on the input path beyond a map write and, at most, a timer.
func (r *Router) noteButtons(playerID string, buttons uint32) {
	s := r.soc
	s.mu.Lock()
	if cur, seen := s.buttons[playerID]; seen && cur == buttons {
		s.mu.Unlock()
		return
	}
	s.buttons[playerID] = buttons
	if s.emitted[playerID] == buttons {
		s.mu.Unlock()
		return
	}
	s.dirty = true
	if s.flushTime != nil {
		s.mu.Unlock()
		return
	}
	wait := inputEvery - s.now().Sub(s.lastEmit)
	if wait < 0 {
		wait = 0
	}
	// Always flush off the input goroutine.
	s.flushTime = time.AfterFunc(wait, r.flushButtons)
	s.mu.Unlock()
}

func (r *Router) flushButtons() {
	s := r.soc
	s.mu.Lock()
	s.flushTime = nil
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	s.dirty = false
	var out []PlayerButtons
	for id, b := range s.buttons {
		if s.emitted[id] != b {
			s.emitted[id] = b
			out = append(out, PlayerButtons{PlayerID: id, Buttons: b})
		}
	}
	s.lastEmit = s.now()
	s.mu.Unlock()
	if len(out) == 0 {
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PlayerID < out[j].PlayerID })
	r.emit(InputEvent, InputNote{Players: out})
}
