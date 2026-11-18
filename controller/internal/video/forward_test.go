package video

import (
	"testing"

	"github.com/pion/rtp"
)

func TestIsKeyframeStart(t *testing.T) {
	cases := []struct {
		name string
		p    []byte
		want bool
	}{
		{"empty", nil, false},
		{"single SPS", []byte{0x67, 1, 2}, true},
		{"single IDR", []byte{0x65, 1, 2}, true},
		{"single PPS", []byte{0x68, 1}, false},
		{"single non-IDR slice", []byte{0x41, 1, 2}, false},
		{"STAP-A with SPS first", []byte{0x78, 0, 2, 0x67, 9, 0, 1, 0x68}, true},
		{"STAP-A with IDR second", []byte{0x78, 0, 2, 0x68, 9, 0, 2, 0x65, 9}, true},
		{"STAP-A without key NAL", []byte{0x78, 0, 2, 0x68, 9, 0, 2, 0x41, 9}, false},
		{"STAP-A truncated", []byte{0x78, 0, 9, 0x68}, false},
		{"FU-A start of IDR", []byte{0x7c, 0x85, 1}, true},
		{"FU-A middle of IDR", []byte{0x7c, 0x05, 1}, false},
		{"FU-A end of IDR", []byte{0x7c, 0x45, 1}, false},
		{"FU-A start of non-IDR", []byte{0x7c, 0x81, 1}, false},
		{"FU-A short", []byte{0x7c}, false},
	}
	for _, c := range cases {
		if got := isKeyframeStart(c.p); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

type sink struct{ got []rtp.Packet }

func (s *sink) WriteRTP(p *rtp.Packet) error {
	s.got = append(s.got, *p)
	return nil
}

func pkt(seq uint16, payload ...byte) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{SequenceNumber: seq, Timestamp: 90}, Payload: payload}
}

func TestTargetGateAndRenumber(t *testing.T) {
	w := &sink{}
	tg := &target{player: "p", w: w}
	for i, p := range []*rtp.Packet{pkt(100, 0x41, 1), pkt(101, 0x7c, 0x05, 1)} {
		if tg.forward(p) {
			t.Fatalf("packet %d forwarded before a keyframe", i)
		}
	}
	steps := []*rtp.Packet{pkt(102, 0x67, 1), pkt(103, 0x68, 1), pkt(104, 0x41, 2)}
	for _, p := range steps {
		if !tg.forward(p) {
			t.Fatal("packet after keyframe not forwarded")
		}
	}
	if len(w.got) != 3 {
		t.Fatalf("written %d", len(w.got))
	}
	for i, p := range w.got {
		if p.SequenceNumber != uint16(i) {
			t.Errorf("packet %d seq %d", i, p.SequenceNumber)
		}
	}
}

func TestRenumberWraps(t *testing.T) {
	r := renumber{n: 65535}
	if a, b := r.next(), r.next(); a != 65535 || b != 0 {
		t.Fatalf("%d %d", a, b)
	}
}

func TestBackoff(t *testing.T) {
	var b backoff
	want := []int{250, 500, 1000, 2000, 4000, 5000, 5000}
	for i, w := range want {
		if got := b.next().Milliseconds(); int(got) != w {
			t.Errorf("step %d: %d want %d", i, got, w)
		}
	}
	b.reset()
	if b.next() != backoffMin {
		t.Error("reset")
	}
}
