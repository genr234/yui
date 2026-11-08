// Package protocol mirrors the Yui Pad wire protocol (see protocol/ in the repo
// root and docs/plans/phone-controller-contracts.md section 3).
package protocol

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	InputSize      = 48
	InputVersion   = 1
	InputTypeState = 1
)

// Button bits.
const (
	ButtonA        uint32 = 1 << 0
	ButtonB        uint32 = 1 << 1
	ButtonOne      uint32 = 1 << 2
	ButtonTwo      uint32 = 1 << 3
	ButtonMinus    uint32 = 1 << 4
	ButtonPlus     uint32 = 1 << 5
	ButtonHome     uint32 = 1 << 6
	ButtonUp       uint32 = 1 << 7
	ButtonDown     uint32 = 1 << 8
	ButtonLeft     uint32 = 1 << 9
	ButtonRight    uint32 = 1 << 10
	ButtonZ        uint32 = 1 << 11
	ButtonC        uint32 = 1 << 12
	ButtonTrigger  uint32 = 1 << 13
	ButtonRecenter uint32 = 1 << 14
)

// InputState is one decoded 48-byte input packet.
type InputState struct {
	Seq     uint16     `json:"seq"`
	TMs     uint32     `json:"tMs"`
	Buttons uint32     `json:"buttons"`
	LX      int16      `json:"lx"`
	LY      int16      `json:"ly"`
	RX      int16      `json:"rx"`
	RY      int16      `json:"ry"`
	Accel   [3]float32 `json:"accel"`
	Gyro    [3]float32 `json:"gyro"`
	PX      int16      `json:"px"`
	PY      int16      `json:"py"`
}

// EncodeInput serialises s into the 48-byte little-endian layout.
func EncodeInput(s InputState) []byte {
	b := make([]byte, InputSize)
	le := binary.LittleEndian
	b[0] = InputVersion
	b[1] = InputTypeState
	le.PutUint16(b[2:], s.Seq)
	le.PutUint32(b[4:], s.TMs)
	le.PutUint32(b[8:], s.Buttons)
	le.PutUint16(b[12:], uint16(s.LX))
	le.PutUint16(b[14:], uint16(s.LY))
	le.PutUint16(b[16:], uint16(s.RX))
	le.PutUint16(b[18:], uint16(s.RY))
	for i := 0; i < 3; i++ {
		le.PutUint32(b[20+i*4:], math.Float32bits(s.Accel[i]))
		le.PutUint32(b[32+i*4:], math.Float32bits(s.Gyro[i]))
	}
	le.PutUint16(b[44:], uint16(s.PX))
	le.PutUint16(b[46:], uint16(s.PY))
	return b
}

// DecodeInput parses a packet; it errors on wrong length, version or type.
func DecodeInput(b []byte) (InputState, error) {
	var s InputState
	if len(b) != InputSize {
		return s, fmt.Errorf("input: expected %d bytes, got %d", InputSize, len(b))
	}
	if b[0] != InputVersion {
		return s, fmt.Errorf("input: unsupported version %d", b[0])
	}
	if b[1] != InputTypeState {
		return s, fmt.Errorf("input: unsupported type %d", b[1])
	}
	le := binary.LittleEndian
	s.Seq = le.Uint16(b[2:])
	s.TMs = le.Uint32(b[4:])
	s.Buttons = le.Uint32(b[8:])
	s.LX = int16(le.Uint16(b[12:]))
	s.LY = int16(le.Uint16(b[14:]))
	s.RX = int16(le.Uint16(b[16:]))
	s.RY = int16(le.Uint16(b[18:]))
	for i := 0; i < 3; i++ {
		s.Accel[i] = math.Float32frombits(le.Uint32(b[20+i*4:]))
		s.Gyro[i] = math.Float32frombits(le.Uint32(b[32+i*4:]))
	}
	s.PX = int16(le.Uint16(b[44:]))
	s.PY = int16(le.Uint16(b[46:]))
	return s, nil
}

// SeqNewer reports whether a is newer than b using 16-bit serial-number arithmetic.
func SeqNewer(a, b uint16) bool {
	d := a - b
	return d != 0 && d < 0x8000
}
