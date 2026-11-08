package dsu

import "kiosk/controller/internal/pad/protocol"

// InputOptions tunes FromInput.
type InputOptions struct {
	// PointerStick replaces the right stick with the pointer axes (after
	// Recenter offsets are applied by the caller). It is the touch-aim
	// fallback for phones without a gyro: an absolute trackpad drives the
	// pointer, and the runtime reads it as a stick.
	PointerStick bool
}

// FromInput converts one Yui Pad input frame (contracts §3) into a DSU pad
// state. ts is the motion timestamp in microseconds; 0 lets the server fill
// it from its own clock.
//
// Buttons are positional on both sides: South is Cross, East is Circle,
// West is Square, North is Triangle, Select is Share and Start is Options.
// Recenter (bit 17) is the touchpad click, so a runtime can bind its own
// "recenter" hotkey to it. Home (bit 10) is reserved for the platform and
// never reaches a virtual pad, so PS is always released.
//
// Sticks are -32767..32767 with +x right and +y up, as DSU expects.
//
// Motion: the pad sends the "phone held like a remote" frame (+x right,
// +y toward the screen, +z up out of the phone's face) in g and deg/s.
// DSU uses the DualShock 4 frame (+x right, +y up out of the face, +z
// toward the player), so accel becomes (x, z, -y). Gyro is already named
// pitch, yaw, roll and passes through.
func FromInput(s protocol.InputState, ts uint64, opt InputOptions) PadState {
	b := s.Buttons
	on := func(bit uint32) bool { return b&bit != 0 }
	ps := PadState{
		Up:       on(protocol.ButtonUp),
		Down:     on(protocol.ButtonDown),
		Left:     on(protocol.ButtonLeft),
		Right:    on(protocol.ButtonRight),
		Cross:    on(protocol.ButtonSouth),
		Circle:   on(protocol.ButtonEast),
		Square:   on(protocol.ButtonWest),
		Triangle: on(protocol.ButtonNorth),
		L1:       on(protocol.ButtonL1),
		R1:       on(protocol.ButtonR1),
		L2:       on(protocol.ButtonL2),
		R2:       on(protocol.ButtonR2),
		L3:       on(protocol.ButtonL3),
		R3:       on(protocol.ButtonR3),
		Share:    on(protocol.ButtonSelect),
		Options:  on(protocol.ButtonStart),
		Touch:    on(protocol.ButtonRecenter),

		LX: axis(s.LX),
		LY: axis(s.LY),
		RX: axis(s.RX),
		RY: axis(s.RY),

		Accel: [3]float32{s.Accel[0], s.Accel[2], -s.Accel[1]},
		Gyro:  s.Gyro,

		MotionTimestamp: ts,
	}
	if opt.PointerStick {
		// Pointer y grows downward like the screen; stick y grows upward.
		ps.RX = axis(s.PX)
		ps.RY = -axis(s.PY)
	}
	return ps
}

func axis(v int16) float32 {
	f := float32(v) / 32767
	if f < -1 {
		f = -1
	}
	return f
}
