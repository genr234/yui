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
	InputTypeTouch = 2
	TouchSize      = 20
)

// Button bits: a generic gamepad named by position (see protocol/src/index.ts).
// Device-shaped layouts map their own labels onto these; nothing here is
// specific to one console.
const (
	ButtonSouth    uint32 = 1 << 0
	ButtonEast     uint32 = 1 << 1
	ButtonWest     uint32 = 1 << 2
	ButtonNorth    uint32 = 1 << 3
	ButtonL1       uint32 = 1 << 4
	ButtonR1       uint32 = 1 << 5
	ButtonL2       uint32 = 1 << 6
	ButtonR2       uint32 = 1 << 7
	ButtonSelect   uint32 = 1 << 8
	ButtonStart    uint32 = 1 << 9
	ButtonHome     uint32 = 1 << 10 // reserved: opens the phone's system sheet
	ButtonL3       uint32 = 1 << 11
	ButtonR3       uint32 = 1 << 12
	ButtonUp       uint32 = 1 << 13
	ButtonDown     uint32 = 1 << 14
	ButtonLeft     uint32 = 1 << 15
	ButtonRight    uint32 = 1 << 16
	ButtonRecenter uint32 = 1 << 17 // system: re-zero motion / pointer
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

// Acted reports whether next differs from prev in something a person does
// on purpose: buttons, sticks or the pointer. Phones stream motion data and
// packet counters even when lying on a table, so those are ignored.
func Acted(prev, next InputState) bool {
	return prev.Buttons != next.Buttons ||
		prev.LX != next.LX || prev.LY != next.LY ||
		prev.RX != next.RX || prev.RY != next.RY ||
		prev.PX != next.PX || prev.PY != next.PY
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

// TouchPoint is one finger on a surface; X and Y span the video content
// rectangle, 0..65535, and are 0 when the point is not down.
type TouchPoint struct {
	Down bool   `json:"down"`
	X    uint16 `json:"x"`
	Y    uint16 `json:"y"`
}

// TouchState is one decoded 20-byte touch frame.
type TouchState struct {
	Seq    uint16        `json:"seq"`
	TMs    uint32        `json:"tMs"`
	Points [2]TouchPoint `json:"points"`
}

// EncodeTouch serialises s into the 20-byte little-endian layout.
func EncodeTouch(s TouchState) []byte {
	b := make([]byte, TouchSize)
	le := binary.LittleEndian
	b[0] = InputVersion
	b[1] = InputTypeTouch
	le.PutUint16(b[2:], s.Seq)
	le.PutUint32(b[4:], s.TMs)
	for i, p := range s.Points {
		if p.Down {
			b[8] |= 1 << i
		}
		le.PutUint16(b[10+i*4:], p.X)
		le.PutUint16(b[12+i*4:], p.Y)
	}
	return b
}

// DecodeTouch parses a touch frame; it errors on wrong length, version or type.
func DecodeTouch(b []byte) (TouchState, error) {
	var s TouchState
	if len(b) != TouchSize {
		return s, fmt.Errorf("touch: expected %d bytes, got %d", TouchSize, len(b))
	}
	if b[0] != InputVersion {
		return s, fmt.Errorf("touch: unsupported version %d", b[0])
	}
	if b[1] != InputTypeTouch {
		return s, fmt.Errorf("touch: unsupported type %d", b[1])
	}
	le := binary.LittleEndian
	s.Seq = le.Uint16(b[2:])
	s.TMs = le.Uint32(b[4:])
	for i := range s.Points {
		s.Points[i] = TouchPoint{
			Down: b[8]&(1<<i) != 0,
			X:    le.Uint16(b[10+i*4:]),
			Y:    le.Uint16(b[12+i*4:]),
		}
	}
	return s, nil
}

// FrameType returns b[1] after checking len(b) >= 2 and b[0] == InputVersion.
func FrameType(b []byte) (byte, error) {
	if len(b) < 2 {
		return 0, fmt.Errorf("input: frame too short (%d bytes)", len(b))
	}
	if b[0] != InputVersion {
		return 0, fmt.Errorf("input: unsupported version %d", b[0])
	}
	return b[1], nil
}
