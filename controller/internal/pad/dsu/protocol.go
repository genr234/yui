// Package dsu implements a DSU (cemuhook) protocol v1001 server so that
// emulators such as Dolphin can read pad and motion data over UDP.
package dsu

import (
	"encoding/binary"
	"hash/crc32"
	"math"
)

const (
	protocolVersion = 1001
	headerLen       = 16
	numSlots        = 4

	msgVersion  = 0x100000
	msgInfo     = 0x100001
	msgData     = 0x100002
	magicServer = "DSUS"
	magicClient = "DSUC"

	// Controller data payload (after the message type) is 80 bytes.
	dataPayloadLen = 80
	dataPacketLen  = headerLen + 4 + dataPayloadLen // 100
	infoPacketLen  = headerLen + 4 + 12             // 32
)

// PadState is the full state of one virtual DS4-style pad.
type PadState struct {
	Up, Down, Left, Right           bool
	Cross, Circle, Square, Triangle bool
	L1, R1, L2, R2, L3, R3          bool
	Share, Options, PS, Touch       bool

	LX, LY, RX, RY float32 // sticks, -1..1; +Y is up
	LT, RT         float32 // analog triggers, 0..1

	Accel [3]float32 // g
	Gyro  [3]float32 // pitch, yaw, roll in deg/s

	MotionTimestamp uint64 // microseconds; 0 = server fills from a monotonic clock
}

// slotMAC returns a stable fake MAC for a slot.
func slotMAC(slot int) [6]byte {
	return [6]byte{0x02, 'Y', 'U', 'I', 0x00, byte(slot + 1)}
}

// newPacket allocates a packet with the header (except CRC) and type filled in.
func newPacket(serverID, msgType uint32, payloadLen int) []byte {
	b := make([]byte, headerLen+4+payloadLen)
	copy(b[0:4], magicServer)
	binary.LittleEndian.PutUint16(b[4:], protocolVersion)
	binary.LittleEndian.PutUint16(b[6:], uint16(4+payloadLen))
	binary.LittleEndian.PutUint32(b[12:], serverID)
	binary.LittleEndian.PutUint32(b[16:], msgType)
	return b
}

// seal computes the CRC32 over the packet with the CRC field zeroed.
func seal(b []byte) []byte {
	binary.LittleEndian.PutUint32(b[8:], 0)
	binary.LittleEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b))
	return b
}

func encodeVersion(serverID uint32) []byte {
	b := newPacket(serverID, msgVersion, 2)
	binary.LittleEndian.PutUint16(b[20:], protocolVersion)
	return seal(b)
}

// putSlotHeader writes the 11-byte shared controller block.
func putSlotHeader(b []byte, slot int, connected bool) {
	b[0] = byte(slot)
	if connected {
		b[1] = 2 // connected
		b[2] = 2 // full gyro
		b[3] = 2 // bluetooth
		mac := slotMAC(slot)
		copy(b[4:10], mac[:])
		b[10] = 0x05 // full battery
	}
}

func encodeInfo(serverID uint32, slot int, connected bool) []byte {
	b := newPacket(serverID, msgInfo, 12)
	putSlotHeader(b[20:], slot, connected)
	// b[31] is the trailing zero byte.
	return seal(b)
}

func stickByte(v float32) byte {
	if v != v { // NaN
		return 128
	}
	if v > 1 {
		v = 1
	} else if v < -1 {
		v = -1
	}
	return byte(math.Round(float64(128 + v*127)))
}

func analogByte(v float32) byte {
	if v != v || v < 0 {
		return 0
	}
	if v > 1 {
		v = 1
	}
	return byte(math.Round(float64(v * 255)))
}

func btn(p bool) byte {
	if p {
		return 255
	}
	return 0
}

func maxByte(a, b byte) byte {
	if a > b {
		return a
	}
	return b
}

func encodeControllerData(serverID uint32, slot int, packetNum uint32, s PadState) []byte {
	b := newPacket(serverID, msgData, dataPayloadLen)
	p := b[20:]
	putSlotHeader(p, slot, true)
	p[11] = 1
	binary.LittleEndian.PutUint32(p[12:], packetNum)

	var a, c byte
	set := func(dst *byte, bit uint, on bool) {
		if on {
			*dst |= 1 << bit
		}
	}
	set(&a, 7, s.Left)
	set(&a, 6, s.Down)
	set(&a, 5, s.Right)
	set(&a, 4, s.Up)
	set(&a, 3, s.Options)
	set(&a, 2, s.R3)
	set(&a, 1, s.L3)
	set(&a, 0, s.Share)
	l2 := s.L2 || s.LT > 0.5
	r2 := s.R2 || s.RT > 0.5
	// cemuhook names these Y/B/A/X by position (Nintendo layout):
	// Y = left = Square, B = bottom = Cross, A = right = Circle,
	// X = top = Triangle. Matches Dolphin's DualShockUDPProto.h.
	set(&c, 7, s.Square)
	set(&c, 6, s.Cross)
	set(&c, 5, s.Circle)
	set(&c, 4, s.Triangle)
	set(&c, 3, s.R1)
	set(&c, 2, s.L1)
	set(&c, 1, r2)
	set(&c, 0, l2)
	p[16], p[17] = a, c
	p[18] = btn(s.PS)
	p[19] = btn(s.Touch)
	p[20] = stickByte(s.LX)
	p[21] = stickByte(s.LY) // 255 = up
	p[22] = stickByte(s.RX)
	p[23] = stickByte(s.RY)
	p[24] = btn(s.Left)
	p[25] = btn(s.Down)
	p[26] = btn(s.Right)
	p[27] = btn(s.Up)
	// Dolphin reads face buttons only from these analog bytes.
	p[28] = btn(s.Square)   // analog Y (left)
	p[29] = btn(s.Cross)    // analog B (bottom)
	p[30] = btn(s.Circle)   // analog A (right)
	p[31] = btn(s.Triangle) // analog X (top)
	p[32] = btn(s.R1)
	p[33] = btn(s.L1)
	p[34] = maxByte(analogByte(s.RT), btn(s.R2))
	p[35] = maxByte(analogByte(s.LT), btn(s.L2))
	// p[36:48]: two touch entries, left inactive (zero).
	binary.LittleEndian.PutUint64(p[48:], s.MotionTimestamp)
	for i := 0; i < 3; i++ {
		binary.LittleEndian.PutUint32(p[56+4*i:], math.Float32bits(s.Accel[i]))
		binary.LittleEndian.PutUint32(p[68+4*i:], math.Float32bits(s.Gyro[i]))
	}
	return seal(b)
}

// request is a parsed client packet.
type request struct {
	msgType uint32
	payload []byte // after the message type
}

// parseRequest validates a client packet (magic, version, length, CRC).
func parseRequest(b []byte) (request, bool) {
	if len(b) < headerLen+4 || string(b[0:4]) != magicClient {
		return request{}, false
	}
	if binary.LittleEndian.Uint16(b[4:]) > protocolVersion {
		return request{}, false
	}
	n := int(binary.LittleEndian.Uint16(b[6:]))
	if n < 4 || headerLen+n > len(b) {
		return request{}, false
	}
	b = b[:headerLen+n]
	crc := binary.LittleEndian.Uint32(b[8:])
	tmp := append([]byte(nil), b...)
	binary.LittleEndian.PutUint32(tmp[8:], 0)
	if crc32.ChecksumIEEE(tmp) != crc {
		return request{}, false
	}
	return request{msgType: binary.LittleEndian.Uint32(b[16:]), payload: b[20:]}, true
}
