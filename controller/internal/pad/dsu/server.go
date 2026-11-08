package dsu

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"
)

type subscription struct {
	addr    *net.UDPAddr
	expires time.Time
	all     bool
	slots   [numSlots]bool
	macs    [][6]byte
}

func (c *subscription) wants(slot int) bool {
	if c.all || c.slots[slot] {
		return true
	}
	m := slotMAC(slot)
	for _, x := range c.macs {
		if x == m {
			return true
		}
	}
	return false
}

type slotState struct {
	connected bool
	state     PadState
	counter   uint32
}

// Server is a DSU protocol server.
type Server struct {
	conn     *net.UDPConn
	serverID uint32
	start    time.Time

	subTimeout     time.Duration // subscription lifetime (default 5 s)
	resendInterval time.Duration // idle resend period (default 100 ms)

	mu    sync.Mutex
	slots [numSlots]slotState
	subs  map[string]*subscription
}

// NewServer binds a UDP socket on addr (e.g. "127.0.0.1:26760").
func NewServer(addr string) (*Server, error) {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		return nil, err
	}
	var id [4]byte
	_, _ = rand.Read(id[:])
	return &Server{
		conn:           conn,
		serverID:       binary.LittleEndian.Uint32(id[:]),
		start:          time.Now(),
		subTimeout:     5 * time.Second,
		resendInterval: 100 * time.Millisecond,
		subs:           map[string]*subscription{},
	}, nil
}

// Addr returns the local UDP address.
func (s *Server) Addr() net.Addr { return s.conn.LocalAddr() }

// Run serves requests until ctx is cancelled, then closes the socket.
func (s *Server) Run(ctx context.Context) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			s.conn.Close()
		case <-done:
		}
	}()
	go s.resendLoop(ctx)

	buf := make([]byte, 2048)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.handle(buf[:n], addr)
	}
}

func (s *Server) resendLoop(ctx context.Context) {
	t := time.NewTicker(s.resendInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			for i := range s.slots {
				if s.slots[i].connected {
					s.pushLocked(i)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Server) handle(b []byte, addr *net.UDPAddr) {
	req, ok := parseRequest(b)
	if !ok {
		return
	}
	switch req.msgType {
	case msgVersion:
		s.send(encodeVersion(s.serverID), addr)
	case msgInfo:
		if len(req.payload) < 4 {
			return
		}
		cnt := int(int32(binary.LittleEndian.Uint32(req.payload)))
		if cnt < 0 || cnt > numSlots {
			return
		}
		for i := 0; i < cnt && 4+i < len(req.payload); i++ {
			slot := int(req.payload[4+i])
			if slot >= numSlots {
				continue
			}
			s.mu.Lock()
			conn := s.slots[slot].connected
			s.mu.Unlock()
			s.send(encodeInfo(s.serverID, slot, conn), addr)
		}
	case msgData:
		if len(req.payload) < 8 {
			return
		}
		flags := req.payload[0]
		s.mu.Lock()
		key := addr.String()
		sub := s.subs[key]
		if sub == nil {
			sub = &subscription{addr: addr}
			s.subs[key] = sub
		}
		sub.expires = time.Now().Add(s.subTimeout)
		if flags == 0 {
			sub.all = true
		}
		if flags&1 != 0 && int(req.payload[1]) < numSlots {
			sub.slots[req.payload[1]] = true
		}
		if flags&2 != 0 {
			var m [6]byte
			copy(m[:], req.payload[2:8])
			dup := false
			for _, x := range sub.macs {
				dup = dup || x == m
			}
			if !dup {
				sub.macs = append(sub.macs, m)
			}
		}
		s.mu.Unlock()
	}
}

func (s *Server) send(b []byte, addr *net.UDPAddr) {
	_, _ = s.conn.WriteToUDP(b, addr)
}

// pushLocked sends slot's current state to all live subscribers. s.mu held.
func (s *Server) pushLocked(slot int) {
	now := time.Now()
	st := &s.slots[slot]
	st.counter++
	ps := st.state
	if ps.MotionTimestamp == 0 {
		ps.MotionTimestamp = uint64(now.Sub(s.start) / time.Microsecond)
	}
	var pkt []byte
	for k, sub := range s.subs {
		if now.After(sub.expires) {
			delete(s.subs, k)
			continue
		}
		if !sub.wants(slot) {
			continue
		}
		if pkt == nil {
			pkt = encodeControllerData(s.serverID, slot, st.counter, ps)
		}
		s.send(pkt, sub.addr)
	}
}

// SetSlot marks slot connected, stores state and pushes it to subscribers.
func (s *Server) SetSlot(slot int, state PadState) {
	if slot < 0 || slot >= numSlots {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slots[slot].connected = true
	s.slots[slot].state = state
	s.pushLocked(slot)
}

// DisconnectSlot marks slot as not connected; no more data is sent for it.
func (s *Server) DisconnectSlot(slot int) {
	if slot < 0 || slot >= numSlots {
		return
	}
	s.mu.Lock()
	s.slots[slot].connected = false
	s.slots[slot].state = PadState{}
	s.mu.Unlock()
}
