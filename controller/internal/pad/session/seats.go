package session

import (
	"context"
	"errors"
	"log"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

// Seat passing (contracts §3 "Seat passing"): a seated player offers their
// slot to a connected spectator. The kiosk keeps the pending offer and its
// timer; Rails makes the swap in one transaction when the spectator accepts.

const (
	defaultSeatOfferTTL = 20 * time.Second
	seatPassBudget      = 10 * time.Second
)

// seatOffer is one pending offer. It is keyed by the giver, who holds the
// seat, so a seat has at most one.
type seatOffer struct {
	giver, to string
	slot      int
	timer     *time.Timer
}

func seatMate(p protocol.Player) protocol.SeatMate {
	return protocol.SeatMate{ID: p.ID, Name: p.Name, Color: p.Color, Avatar: p.Avatar}
}

func (m *Manager) seatTTL() time.Duration {
	if m.cfg.SeatOfferTTL > 0 {
		return m.cfg.SeatOfferTTL
	}
	return defaultSeatOfferTTL
}

func ceilSeconds(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// offerSeat starts an offer from giverID to the spectator toID.
func (m *Manager) offerSeat(giverID, toID string) {
	m.mu.Lock()
	giver, to := m.players[giverID], m.players[toID]
	reason := ""
	switch {
	case giver == nil || giver.Slot == nil:
		reason = "not_seated"
	case to == nil || giverID == toID || to.Slot != nil || !to.Connected:
		reason = "recipient_unavailable"
	case m.offers[giverID] != nil:
		reason = "busy"
	default:
		for _, o := range m.offers {
			if o.to == toID {
				reason = "busy"
			}
		}
	}
	if reason != "" {
		slot := 0
		var other *protocol.SeatMate
		if giver != nil && giver.Slot != nil {
			slot = *giver.Slot
		}
		if to != nil {
			mate := seatMate(to.Player)
			other = &mate
		}
		m.mu.Unlock()
		m.sendSeatResult(giverID, protocol.SeatResult{Result: "failed", Role: "giver", Slot: slot, Other: other, Reason: reason})
		return
	}
	ttl := m.seatTTL()
	o := &seatOffer{giver: giverID, to: toID, slot: *giver.Slot}
	o.timer = time.AfterFunc(ttl, func() { m.endOffer(o, "expired", "") })
	if m.offers == nil {
		m.offers = map[string]*seatOffer{}
	}
	m.offers[giverID] = o
	giverMate, toMate := seatMate(giver.Player), seatMate(to.Player)
	m.mu.Unlock()

	secs := ceilSeconds(ttl)
	m.sendSeatResult(giverID, protocol.SeatResult{Result: "pending", Role: "giver", Slot: o.slot, Other: &toMate, ExpiresIn: secs})
	if err := m.sendControl(toID, protocol.SeatOffered{T: "seat_offered", From: giverMate, Slot: o.slot, ExpiresIn: secs}); err != nil {
		log.Printf("pad session: seat_offered %s: %v", toID, err)
		m.endOffer(o, "failed", "recipient_unavailable")
	}
}

// takeOffer removes the offer if it is still the pending one (so a timer, an
// answer, a cancel and a roster change settle it exactly once).
func (m *Manager) takeOffer(o *seatOffer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.offers[o.giver] != o {
		return false
	}
	delete(m.offers, o.giver)
	o.timer.Stop()
	return true
}

// endOffer settles the offer with a final result for both phones.
func (m *Manager) endOffer(o *seatOffer, result, reason string) {
	if !m.takeOffer(o) {
		return
	}
	m.announceOffer(o, result, reason)
}

func (m *Manager) announceOffer(o *seatOffer, result, reason string) {
	m.mu.Lock()
	var giverMate, toMate *protocol.SeatMate
	if st := m.players[o.giver]; st != nil {
		mate := seatMate(st.Player)
		giverMate = &mate
	}
	if st := m.players[o.to]; st != nil {
		mate := seatMate(st.Player)
		toMate = &mate
	}
	m.mu.Unlock()
	m.sendSeatResult(o.giver, protocol.SeatResult{Result: result, Role: "giver", Slot: o.slot, Other: toMate, Reason: reason})
	m.sendSeatResult(o.to, protocol.SeatResult{Result: result, Role: "recipient", Slot: o.slot, Other: giverMate, Reason: reason})
}

func (m *Manager) sendSeatResult(playerID string, res protocol.SeatResult) {
	res.T = "seat_result"
	if err := m.sendControl(playerID, res); err != nil {
		log.Printf("pad session: seat_result %s: %v", playerID, err)
	}
}

func (m *Manager) offerFor(giverID string) *seatOffer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offers[giverID]
}

// cancelSeat is the giver withdrawing their offer.
func (m *Manager) cancelSeat(giverID string) {
	if o := m.offerFor(giverID); o != nil {
		m.endOffer(o, "cancelled", "")
	}
}

// answerSeat is the recipient's reply to the offer made by fromID. An answer
// to an offer that already lapsed gets "expired" so the card closes.
func (m *Manager) answerSeat(recipientID, fromID string, accept bool) {
	o := m.offerFor(fromID)
	if o == nil || o.to != recipientID {
		m.sendSeatResult(recipientID, protocol.SeatResult{Result: "expired", Role: "recipient", Slot: 0})
		return
	}
	if !accept {
		m.endOffer(o, "declined", "")
		return
	}
	if !m.takeOffer(o) {
		return
	}
	// Rails can take a moment and this runs on the data-channel goroutine.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), seatPassBudget)
		defer cancel()
		if err := m.PassSeat(ctx, o.giver, o.to, o.slot); err != nil {
			reason := "unavailable"
			var seatErr *SeatError
			if errors.As(err, &seatErr) {
				reason = seatErr.Code
			} else {
				log.Printf("pad session: pass seat %s -> %s: %v", o.giver, o.to, err)
			}
			m.announceOffer(o, "failed", reason)
			return
		}
		m.announceOffer(o, "accepted", "")
	}()
}

// PassSeat asks Rails to hand the giver's slot to the spectator. The roster
// updates when player_updated arrives for both.
func (m *Manager) PassSeat(ctx context.Context, giverID, toID string, slot int) error {
	id, err := m.sessionID()
	if err != nil {
		return err
	}
	return m.api.PassSeat(ctx, id, giverID, toID, slot)
}

// reviewOffers cancels offers a roster change made pointless: either party
// left or went offline, the giver lost the seat, or the recipient got one.
// left is true for a removed player.
func (m *Manager) reviewOffers(p protocol.Player, left bool) {
	m.mu.Lock()
	var stale []*seatOffer
	for _, o := range m.offers {
		switch p.ID {
		case o.giver:
			if left || !p.Connected || p.Slot == nil || *p.Slot != o.slot {
				stale = append(stale, o)
			}
		case o.to:
			if left || !p.Connected || p.Slot != nil {
				stale = append(stale, o)
			}
		}
	}
	m.mu.Unlock()
	for _, o := range stale {
		m.endOffer(o, "cancelled", "left_or_changed")
	}
}

// clearOffers stops every timer when the session goes away.
func (m *Manager) clearOffers() {
	m.mu.Lock()
	for id, o := range m.offers {
		o.timer.Stop()
		delete(m.offers, id)
	}
	m.mu.Unlock()
}
