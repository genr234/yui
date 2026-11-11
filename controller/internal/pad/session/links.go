package session

import (
	"net"
	"strconv"

	"github.com/pion/webrtc/v4"
)

// Link is how one phone currently reaches the kiosk, for diagnostics.
// Path is "lan" (host candidates on both sides, straight over the local
// network), "stun" (through NAT, still direct), "turn" (through a relay) or
// "server" (ICE failed; control rides the ActionCable signal channel).
type Link struct {
	ICEState   string  `json:"ice_state"`
	Path       string  `json:"path"`
	Fallback   bool    `json:"fallback"`
	Protocol   string  `json:"protocol,omitempty"`
	Local      string  `json:"local,omitempty"`
	LocalType  string  `json:"local_type,omitempty"`
	Remote     string  `json:"remote,omitempty"`
	RemoteType string  `json:"remote_type,omitempty"`
	RTTMs      float64 `json:"rtt_ms,omitempty"`
	BytesIn    uint64  `json:"bytes_in,omitempty"`
	BytesOut   uint64  `json:"bytes_out,omitempty"`
}

// Links reports every connected phone's path and round-trip time.
func (m *Manager) Links() map[string]Link {
	m.mu.Lock()
	peers := make(map[string]*peer, len(m.peers))
	for id, p := range m.peers {
		peers[id] = p
	}
	m.mu.Unlock()

	out := make(map[string]Link, len(peers))
	for id, p := range peers {
		out[id] = p.link()
	}
	return out
}

func (p *peer) link() Link {
	l := Link{ICEState: p.pc.ICEConnectionState().String(), Fallback: p.fallback.Load()}
	if l.Fallback {
		l.Path = "server"
	}
	stats := p.pc.GetStats()
	for _, s := range stats {
		pair, ok := s.(webrtc.ICECandidatePairStats)
		if !ok || !pair.Nominated || pair.State != webrtc.StatsICECandidatePairStateSucceeded {
			continue
		}
		local, _ := stats[pair.LocalCandidateID].(webrtc.ICECandidateStats)
		remote, _ := stats[pair.RemoteCandidateID].(webrtc.ICECandidateStats)
		l.Protocol = local.Protocol
		l.Local = addr(local)
		l.LocalType = local.CandidateType.String()
		l.Remote = addr(remote)
		l.RemoteType = remote.CandidateType.String()
		l.RTTMs = pair.CurrentRoundTripTime * 1000
		l.BytesIn, l.BytesOut = pair.BytesReceived, pair.BytesSent
		if !l.Fallback {
			l.Path = pathOf(local.CandidateType, remote.CandidateType, remote.IP)
		}
		break
	}
	if l.Path == "" {
		l.Path = "none"
	}
	return l
}

func addr(c webrtc.ICECandidateStats) string {
	if c.IP == "" {
		return ""
	}
	return c.IP + ":" + strconv.Itoa(int(c.Port))
}

func pathOf(local, remote webrtc.ICECandidateType, remoteIP string) string {
	switch {
	case local == webrtc.ICECandidateTypeRelay || remote == webrtc.ICECandidateTypeRelay:
		return "turn"
	// Phone browsers hide their host address behind an mDNS name, so the
	// kiosk learns it as peer-reflexive when the phone's check arrives.
	// A private address on both ends is still the local network.
	case local == webrtc.ICECandidateTypeHost && (remote == webrtc.ICECandidateTypeHost || privateIP(remoteIP)):
		return "lan"
	default:
		return "stun"
	}
}

func privateIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback())
}
