package dsu

import (
	"fmt"
	"time"
)

type serverStats struct {
	requests    map[uint32]uint64
	badRequests uint64
	lastRequest time.Time
	lastClient  string
}

// Stats is a debugging snapshot: whether a program (the DSU client) is
// talking to this server, and whether input reaches each slot.
type Stats struct {
	Addr          string            `json:"addr"`
	Requests      map[string]uint64 `json:"requests"`
	BadRequests   uint64            `json:"bad_requests"`
	LastRequest   *time.Time        `json:"last_request,omitempty"`
	LastClient    string            `json:"last_client,omitempty"`
	Subscriptions []SubStats        `json:"subscriptions"`
	Slots         []SlotStats       `json:"slots"`
}

type SubStats struct {
	Client  string    `json:"client"`
	All     bool      `json:"all"`
	Slots   []int     `json:"slots,omitempty"`
	MACs    int       `json:"macs,omitempty"`
	Expires time.Time `json:"expires"`
}

type SlotStats struct {
	Slot      int        `json:"slot"`
	Connected bool       `json:"connected"`
	Updates   uint64     `json:"updates"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	Sent      uint64     `json:"sent"`
	Pressed   []string   `json:"pressed,omitempty"`
}

func requestName(t uint32) string {
	switch t {
	case msgVersion:
		return "version"
	case msgInfo:
		return "info"
	case msgData:
		return "data"
	}
	return fmt.Sprintf("0x%x", t)
}

// Stats returns a snapshot for debugging.
func (s *Server) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Stats{Addr: s.conn.LocalAddr().String(), Requests: map[string]uint64{}, BadRequests: s.stats.badRequests, LastClient: s.stats.lastClient}
	for t, n := range s.stats.requests {
		out.Requests[requestName(t)] = n
	}
	if !s.stats.lastRequest.IsZero() {
		at := s.stats.lastRequest
		out.LastRequest = &at
	}
	now := time.Now()
	for _, sub := range s.subs {
		if now.After(sub.expires) {
			continue
		}
		ss := SubStats{Client: sub.addr.String(), All: sub.all, MACs: len(sub.macs), Expires: sub.expires}
		for i, on := range sub.slots {
			if on {
				ss.Slots = append(ss.Slots, i)
			}
		}
		out.Subscriptions = append(out.Subscriptions, ss)
	}
	for i, st := range s.slots {
		sl := SlotStats{Slot: i, Connected: st.connected, Updates: st.updates, Sent: st.sent, Pressed: pressed(st.state)}
		if !st.updatedAt.IsZero() {
			at := st.updatedAt
			sl.UpdatedAt = &at
		}
		out.Slots = append(out.Slots, sl)
	}
	return out
}

func pressed(p PadState) []string {
	var out []string
	add := func(on bool, name string) {
		if on {
			out = append(out, name)
		}
	}
	add(p.Up, "Up")
	add(p.Down, "Down")
	add(p.Left, "Left")
	add(p.Right, "Right")
	add(p.Cross, "Cross")
	add(p.Circle, "Circle")
	add(p.Square, "Square")
	add(p.Triangle, "Triangle")
	add(p.L1, "L1")
	add(p.R1, "R1")
	add(p.L2, "L2")
	add(p.R2, "R2")
	add(p.Share, "Share")
	add(p.Options, "Options")
	add(p.PS, "PS")
	return out
}
