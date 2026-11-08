package dsu

import (
	"bytes"
	"encoding/binary"
	"testing"

	"kiosk/controller/internal/pad/protocol"
)

// vectorFrame is the pad input frame that converts to controllerDataVector,
// except for what a phone cannot send: PS (Home is reserved for the
// platform) and the analog left trigger (L2 is digital on the pad).
func vectorFrame() protocol.InputState {
	return protocol.InputState{
		Seq: 7, TMs: 1234,
		Buttons: protocol.ButtonUp | protocol.ButtonLeft | protocol.ButtonSouth | protocol.ButtonNorth |
			protocol.ButtonL1 | protocol.ButtonR2 | protocol.ButtonStart | protocol.ButtonSelect |
			protocol.ButtonRecenter | protocol.ButtonHome,
		LX: 32767, LY: 32767, RX: -32767, RY: 0,
		// Remote frame (x right, y toward the screen, z up) → DSU (x, z, -y).
		Accel: [3]float32{0.5, -2, -1},
		Gyro:  [3]float32{10, 20, 30},
	}
}

func TestFromInputMatchesVector(t *testing.T) {
	want := controllerDataVector()
	want.PS = false
	want.LT = 0

	got := FromInput(vectorFrame(), want.MotionTimestamp, InputOptions{})
	if got != want {
		t.Fatalf("FromInput:\n got %+v\nwant %+v", got, want)
	}
	gotBytes := encodeControllerData(9, 1, 0xA1B2C3D4, got)
	wantBytes := encodeControllerData(9, 1, 0xA1B2C3D4, want)
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("packet differs:\n got % x\nwant % x", gotBytes, wantBytes)
	}

	// The same byte checks as TestEncodeControllerData, minus PS and LT.
	p := gotBytes[20:]
	if p[16] != 128|16|8|1 {
		t.Fatalf("mask A %08b", p[16])
	}
	if p[17] != 64|16|4|2 {
		t.Fatalf("mask B %08b", p[17])
	}
	if p[18] != 0 || p[19] != 255 {
		t.Fatalf("home must not reach PS, recenter is touch: ps %d touch %d", p[18], p[19])
	}
	if p[20] != 255 || p[21] != 255 || p[22] != 1 || p[23] != 128 {
		t.Fatalf("sticks % x", p[20:24])
	}
	if p[35] != 0 {
		t.Fatalf("L2 analog %d", p[35])
	}
	if binary.LittleEndian.Uint64(p[48:]) != 0x0102030405060708 {
		t.Fatal("timestamp")
	}
	if f32(p[56:]) != 0.5 || f32(p[60:]) != -1 || f32(p[64:]) != 2 {
		t.Fatalf("accel %v %v %v", f32(p[56:]), f32(p[60:]), f32(p[64:]))
	}
	if f32(p[68:]) != 10 || f32(p[72:]) != 20 || f32(p[76:]) != 30 {
		t.Fatal("gyro")
	}
}

func TestFromInputGravityAtRest(t *testing.T) {
	// Phone flat, face up: gravity reads +1 g on the remote frame's z.
	ps := FromInput(protocol.InputState{Accel: [3]float32{0, 0, 1}}, 0, InputOptions{})
	if ps.Accel != [3]float32{0, 1, 0} {
		t.Fatalf("flat remote should read +1 g on DSU y (up): %v", ps.Accel)
	}
	// Pointing straight up at the ceiling: gravity along -y of the remote
	// frame, which is +z (toward the player) in DSU.
	ps = FromInput(protocol.InputState{Accel: [3]float32{0, -1, 0}}, 0, InputOptions{})
	if ps.Accel != [3]float32{0, 0, 1} {
		t.Fatalf("upright remote: %v", ps.Accel)
	}
}

func TestFromInputPointerStick(t *testing.T) {
	s := protocol.InputState{RX: 100, RY: 100, PX: 32767, PY: -32767}
	ps := FromInput(s, 0, InputOptions{PointerStick: true})
	if ps.RX != 1 || ps.RY != 1 {
		t.Fatalf("pointer stick %v %v (pointer up must be stick up)", ps.RX, ps.RY)
	}
	ps = FromInput(protocol.InputState{LX: -32768}, 0, InputOptions{})
	if ps.LX != -1 {
		t.Fatalf("min stick %v", ps.LX)
	}
}
