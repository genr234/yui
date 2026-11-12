package dsu

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"math"
	"net"
	"testing"
	"time"
)

func checkHeader(t *testing.T, b []byte, id uint32, msgType uint32, total int) {
	t.Helper()
	if len(b) != total {
		t.Fatalf("len = %d, want %d", len(b), total)
	}
	if string(b[0:4]) != "DSUS" {
		t.Fatalf("magic %q", b[0:4])
	}
	if v := binary.LittleEndian.Uint16(b[4:]); v != 1001 {
		t.Fatalf("version %d", v)
	}
	if n := binary.LittleEndian.Uint16(b[6:]); int(n) != total-16 {
		t.Fatalf("length field %d, want %d", n, total-16)
	}
	if got := binary.LittleEndian.Uint32(b[12:]); got != id {
		t.Fatalf("server id %x", got)
	}
	if got := binary.LittleEndian.Uint32(b[16:]); got != msgType {
		t.Fatalf("type %x", got)
	}
	crc := binary.LittleEndian.Uint32(b[8:])
	tmp := append([]byte(nil), b...)
	copy(tmp[8:12], []byte{0, 0, 0, 0})
	if crc32.ChecksumIEEE(tmp) != crc {
		t.Fatalf("bad CRC")
	}
}

func TestEncodeVersion(t *testing.T) {
	b := encodeVersion(0xAABBCCDD)
	checkHeader(t, b, 0xAABBCCDD, 0x100000, 22)
	if !bytes.Equal(b[20:], []byte{0xE9, 0x03}) {
		t.Fatalf("payload % x", b[20:])
	}
	if !bytes.Equal(b[12:16], []byte{0xDD, 0xCC, 0xBB, 0xAA}) {
		t.Fatalf("id bytes % x", b[12:16])
	}
}

func TestEncodeInfo(t *testing.T) {
	b := encodeInfo(7, 2, true)
	checkHeader(t, b, 7, 0x100001, 32)
	want := []byte{2, 2, 2, 2, 0x02, 'Y', 'U', 'I', 0, 3, 5, 0}
	if !bytes.Equal(b[20:], want) {
		t.Fatalf("got % x want % x", b[20:], want)
	}
	b = encodeInfo(7, 1, false)
	checkHeader(t, b, 7, 0x100001, 32)
	if !bytes.Equal(b[20:], []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("disconnected: % x", b[20:])
	}
}

func f32(b []byte) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b)) }

// controllerDataVector is the reference pad state that the byte checks in
// TestEncodeControllerData are written against.
func controllerDataVector() PadState {
	return PadState{
		Up: true, Left: true, Cross: true, Triangle: true, L1: true, R2: true,
		Options: true, Share: true, PS: true, Touch: true,
		LX: 1, LY: 1, RX: -1, RY: 0,
		LT: 0.5, RT: 0,
		Accel: [3]float32{0.5, -1, 2}, Gyro: [3]float32{10, 20, 30},
		MotionTimestamp: 0x0102030405060708,
	}
}

func TestEncodeControllerData(t *testing.T) {
	s := controllerDataVector()
	b := encodeControllerData(9, 1, 0xA1B2C3D4, s)
	checkHeader(t, b, 9, 0x100002, 100)
	p := b[20:]
	if !bytes.Equal(p[:11], []byte{1, 2, 2, 2, 0x02, 'Y', 'U', 'I', 0, 2, 5}) {
		t.Fatalf("slot header % x", p[:11])
	}
	if p[11] != 1 {
		t.Fatal("connected flag")
	}
	if binary.LittleEndian.Uint32(p[12:]) != 0xA1B2C3D4 {
		t.Fatal("packet number")
	}
	// A: Left(128)+Up(16)+Options(8)+Share(1)
	if p[16] != 128|16|8|1 {
		t.Fatalf("mask A %08b", p[16])
	}
	// B: Cross(64)+Triangle(16)+L1(4)+R2(2)
	if p[17] != 64|16|4|2 {
		t.Fatalf("mask B %08b", p[17])
	}
	if p[18] != 255 || p[19] != 255 {
		t.Fatal("ps/touch")
	}
	if p[20] != 255 || p[21] != 255 || p[22] != 1 || p[23] != 128 {
		t.Fatalf("sticks % x", p[20:24])
	}
	if p[24] != 255 || p[25] != 0 || p[26] != 0 || p[27] != 255 {
		t.Fatalf("analog dpad % x", p[24:28])
	}
	if p[28] != 0 || p[29] != 255 || p[30] != 0 || p[31] != 255 {
		t.Fatalf("analog face % x", p[28:32])
	}
	// R1 0, L1 255, R2 255, L2 128 (0.5*255 rounded)
	if p[32] != 0 || p[33] != 255 || p[34] != 255 || p[35] != 128 {
		t.Fatalf("analog shoulders % x", p[32:36])
	}
	if !bytes.Equal(p[36:48], make([]byte, 12)) {
		t.Fatal("touch not zero")
	}
	if binary.LittleEndian.Uint64(p[48:]) != 0x0102030405060708 {
		t.Fatal("timestamp")
	}
	if f32(p[56:]) != 0.5 || f32(p[60:]) != -1 || f32(p[64:]) != 2 {
		t.Fatal("accel")
	}
	if f32(p[68:]) != 10 || f32(p[72:]) != 20 || f32(p[76:]) != 30 {
		t.Fatal("gyro")
	}
}

func TestStickBytes(t *testing.T) {
	cases := map[float32]byte{0: 128, 1: 255, -1: 1, 2: 255, -2: 1}
	for in, want := range cases {
		if got := stickByte(in); got != want {
			t.Errorf("stickByte(%v) = %d, want %d", in, got, want)
		}
	}
}

// ---- client helpers ----

func clientPacket(msgType uint32, payload []byte) []byte {
	b := make([]byte, 20+len(payload))
	copy(b, "DSUC")
	binary.LittleEndian.PutUint16(b[4:], 1001)
	binary.LittleEndian.PutUint16(b[6:], uint16(4+len(payload)))
	binary.LittleEndian.PutUint32(b[12:], 0x1234)
	binary.LittleEndian.PutUint32(b[16:], msgType)
	copy(b[20:], payload)
	binary.LittleEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b))
	return b
}

func startServer(t *testing.T, timeout, resend time.Duration) (*Server, *net.UDPConn) {
	t.Helper()
	s, err := NewServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.subTimeout = timeout
	s.resendInterval = resend
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	c, err := net.DialUDP("udp", nil, s.Addr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return s, c
}

func recv(t *testing.T, c *net.UDPConn, d time.Duration) []byte {
	t.Helper()
	buf := make([]byte, 2048)
	c.SetReadDeadline(time.Now().Add(d))
	n, err := c.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

func TestEndToEnd(t *testing.T) {
	// Long resend interval so only SetSlot pushes occur; short timeout.
	s, c := startServer(t, 300*time.Millisecond, time.Hour)

	c.Write(clientPacket(0x100000, nil))
	b := recv(t, c, time.Second)
	if b == nil {
		t.Fatal("no version reply")
	}
	checkHeader(t, b, s.serverID, 0x100000, 22)

	s.SetSlot(1, PadState{Cross: true})
	c.Write(clientPacket(0x100001, []byte{4, 0, 0, 0, 0, 1, 2, 3}))
	for slot := 0; slot < 4; slot++ {
		b = recv(t, c, time.Second)
		if b == nil {
			t.Fatalf("no info for slot %d", slot)
		}
		checkHeader(t, b, s.serverID, 0x100001, 32)
		if int(b[20]) != slot {
			t.Fatalf("slot %d reported as %d", slot, b[20])
		}
		wantState := byte(0)
		if slot == 1 {
			wantState = 2
		}
		if b[21] != wantState {
			t.Fatalf("slot %d state %d", slot, b[21])
		}
	}

	// No subscription yet: SetSlot must not reach the client.
	s.SetSlot(1, PadState{Cross: true})
	if b := recv(t, c, 100*time.Millisecond); b != nil {
		t.Fatal("unexpected data before subscribing")
	}

	// Subscribe to slot 1 only.
	c.Write(clientPacket(0x100002, []byte{1, 1, 0, 0, 0, 0, 0, 0}))
	time.Sleep(50 * time.Millisecond)
	s.SetSlot(0, PadState{}) // not subscribed
	s.SetSlot(1, PadState{Cross: true})
	b = recv(t, c, time.Second)
	if b == nil {
		t.Fatal("no data push")
	}
	checkHeader(t, b, s.serverID, 0x100002, 100)
	if b[20] != 1 || b[20+17]&64 == 0 { // Cross = bit 6
		t.Fatalf("wrong data: slot %d maskB %08b", b[20], b[37])
	}
	c1 := binary.LittleEndian.Uint32(b[32:])
	s.SetSlot(1, PadState{Cross: true})
	b = recv(t, c, time.Second)
	if b == nil || binary.LittleEndian.Uint32(b[32:]) != c1+1 {
		t.Fatal("packet counter did not increment")
	}
	if binary.LittleEndian.Uint64(b[68:]) == 0 {
		t.Fatal("server did not fill motion timestamp")
	}

	// Expiry.
	time.Sleep(400 * time.Millisecond)
	s.SetSlot(1, PadState{Cross: true})
	if b := recv(t, c, 150*time.Millisecond); b != nil {
		t.Fatal("data after subscription expiry")
	}

	// Renewal by MAC works.
	mac := slotMAC(1)
	c.Write(clientPacket(0x100002, append([]byte{2, 0}, mac[:]...)))
	time.Sleep(50 * time.Millisecond)
	s.SetSlot(1, PadState{})
	if recv(t, c, time.Second) == nil {
		t.Fatal("no data for MAC subscription")
	}

	// Disconnect stops data.
	s.DisconnectSlot(1)
	s.SetSlot(2, PadState{})
	if b := recv(t, c, 100*time.Millisecond); b != nil {
		t.Fatal("data for unsubscribed slot")
	}
}

func TestRejectsBadCRC(t *testing.T) {
	_, c := startServer(t, time.Second, time.Hour)
	p := clientPacket(0x100000, nil)
	p[8] ^= 0xFF
	c.Write(p)
	if recv(t, c, 150*time.Millisecond) != nil {
		t.Fatal("replied to bad CRC")
	}
}

func TestPeriodicResend(t *testing.T) {
	s, c := startServer(t, time.Second, 30*time.Millisecond)
	c.Write(clientPacket(0x100002, make([]byte, 8)))
	time.Sleep(50 * time.Millisecond)
	s.SetSlot(0, PadState{})
	recv(t, c, time.Second)
	if recv(t, c, time.Second) == nil {
		t.Fatal("no periodic resend")
	}
}

func TestEncodeTouch(t *testing.T) {
	s := controllerDataVector()
	s.Touches[0] = TouchPoint{Active: true, ID: 5, X: 0x0123, Y: 0x0345}
	s.Touches[1] = TouchPoint{Active: true, ID: 6, X: 1919, Y: 941}
	p := encodeControllerData(9, 1, 1, s)[20:]
	want := []byte{
		1, 5, 0x23, 0x01, 0x45, 0x03,
		1, 6, 0x7f, 0x07, 0xad, 0x03,
	}
	if !bytes.Equal(p[36:48], want) {
		t.Fatalf("touch bytes % x, want % x", p[36:48], want)
	}
	// An inactive point is all zero even when it carries stale coordinates.
	s.Touches[1] = TouchPoint{ID: 9, X: 100, Y: 100}
	p = encodeControllerData(9, 1, 1, s)[20:]
	if !bytes.Equal(p[42:48], make([]byte, 6)) {
		t.Fatalf("inactive touch % x", p[42:48])
	}
	if p[36] != 1 {
		t.Fatal("first point lost")
	}
}
