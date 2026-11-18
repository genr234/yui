package video

import (
	"encoding/binary"

	"github.com/pion/rtp"
)

// H.264 NAL unit types used by the keyframe gate.
const (
	nalIDR   = 5
	nalSPS   = 7
	nalSTAPA = 24
	nalFUA   = 28
)

func isKeyNAL(t byte) bool { return t == nalIDR || t == nalSPS }

// isKeyframeStart reports whether an RTP payload starts a NAL unit that is an
// SPS or an IDR slice: a single NAL, any aggregated unit inside a STAP-A, or
// the start fragment of a FU-A.
func isKeyframeStart(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	switch t := payload[0] & 0x1f; {
	case t == nalSTAPA:
		for p := payload[1:]; len(p) >= 3; {
			size := int(binary.BigEndian.Uint16(p))
			if isKeyNAL(p[2] & 0x1f) {
				return true
			}
			if size+2 > len(p) {
				return false
			}
			p = p[2+size:]
		}
		return false
	case t == nalFUA:
		return len(payload) >= 2 && payload[1]&0x80 != 0 && isKeyNAL(payload[1]&0x1f)
	default:
		return isKeyNAL(t)
	}
}

// renumber gives one target a contiguous sequence series.
type renumber struct{ n uint16 }

func (r *renumber) next() uint16 {
	v := r.n
	r.n++
	return v
}

// target is one player's track as the forwarder sees it. Only the forwarding
// goroutine touches open and rn after the target is published.
type target struct {
	player string
	w      RTPWriter
	open   bool
	rn     renumber
}

// forward writes p to the target once a keyframe has been seen, renumbering it.
// It reports whether the packet was written.
func (t *target) forward(p *rtp.Packet) bool {
	if !t.open {
		if !isKeyframeStart(p.Payload) {
			return false
		}
		t.open = true
	}
	p.SequenceNumber = t.rn.next()
	return t.w.WriteRTP(p) == nil
}
