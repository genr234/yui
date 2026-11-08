package session

import (
	"log"
	"strconv"

	"kiosk/controller/internal/pad/protocol"
)

func (m *Manager) handleControl(playerID string, raw []byte, gen int, fromCable bool) {
	if !fromCable && !m.peerCurrent(playerID, gen) {
		return
	}
	msg, err := protocol.ParseControl(raw)
	if err != nil {
		log.Printf("pad session: control %s: %v", playerID, err)
		return
	}
	switch typed := msg.(type) {
	case protocol.Hello:
		if typed.V != 1 {
			log.Printf("pad session: player %s hello version %d", playerID, typed.V)
			return
		}
		m.mu.Lock()
		if st := m.players[playerID]; st != nil {
			hello := typed
			st.hello = &hello
		}
		m.mu.Unlock()
		m.sendWelcome(playerID)
	case protocol.Ping:
		if typed.T == "pong" {
			m.notePong(playerID, typed)
			return
		}
		if err := m.sendControl(playerID, protocol.Ping{T: "pong", ID: typed.ID, TS: typed.TS}); err != nil {
			log.Printf("pad session: pong %s: %v", playerID, err)
		}
	default:
		m.mu.Lock()
		fn := m.onControl
		m.mu.Unlock()
		if fn != nil {
			fn(playerID, msg)
		}
	}
}

func (m *Manager) sendWelcome(playerID string) {
	m.mu.Lock()
	st := m.players[playerID]
	if st == nil {
		m.mu.Unlock()
		return
	}
	player := st.Player
	name := m.cfg.KioskName
	m.mu.Unlock()
	welcome := protocol.Welcome{T: "welcome", V: 1, Player: player, KioskName: name}
	if err := m.sendControl(playerID, welcome); err != nil {
		log.Printf("pad session: welcome %s: %v", playerID, err)
		return
	}
	m.notifyPlayer(PlayerReady, player)
}

// PlayerEventKind says what happened to a player.
type PlayerEventKind string

const (
	// PlayerReady: the player said hello and was sent welcome, so control
	// messages (layout, haptic, system) reach it. Repeats on every hello.
	PlayerReady PlayerEventKind = "ready"
	// PlayerUpdated: roster fields changed (slot, host, name, colour).
	PlayerUpdated PlayerEventKind = "updated"
	// PlayerLeft: the player disconnected, was kicked or removed, or the
	// session ended. Player may carry only the id.
	PlayerLeft PlayerEventKind = "left"
)

// PlayerEvent is one player lifecycle change for SetPlayerHandler.
type PlayerEvent struct {
	Kind   PlayerEventKind
	Player protocol.Player
}

func (m *Manager) notePong(playerID string, pong protocol.Ping) {
	m.mu.Lock()
	pending, ok := m.pings[playerID]
	if !ok || !pingIDEqual(pending.id, pong.ID) {
		m.mu.Unlock()
		return
	}
	rtt := m.now().Sub(pending.sent)
	if rtt < 0 {
		rtt = 0
	}
	delete(m.pings, playerID)
	slot := "spectator"
	if st := m.players[playerID]; st != nil {
		st.rtt = rtt
		st.hasRTT = true
		slot = slotLabel(st.Player)
	}
	m.mu.Unlock()
	log.Printf("pad session: player %s slot %s control rtt %s", playerID, slot, rtt)
}

func pingIDEqual(want int64, got any) bool {
	switch value := got.(type) {
	case float64:
		return int64(value) == want
	case int:
		return int64(value) == want
	case int64:
		return value == want
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return err == nil && parsed == want
	default:
		return false
	}
}

func slotLabel(player protocol.Player) string {
	if player.Slot == nil {
		return "spectator"
	}
	return strconv.Itoa(*player.Slot)
}
