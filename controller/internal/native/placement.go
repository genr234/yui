package native

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Window placement: a program can ask for its main window to be borderless at
// a fixed aspect ratio, centred on the primary screen, so the overlay's bars
// stay visible around it. Nothing here knows what the program is.

const (
	minAspect = 0.2
	maxAspect = 5.0
	// AlignCenter is the only alignment: the full-width rect, vertically centred.
	AlignCenter = "center"
)

// Rect is a window rectangle in physical screen pixels.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Screen is the primary monitor's size in physical pixels.
type Screen struct {
	W int `json:"w"`
	H int `json:"h"`
}

// Aspect is width / height. In JSON it is a number or a "W:H" string.
type Aspect float64

func (a *Aspect) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		v, err := ParseAspect(s)
		*a = Aspect(v)
		return err
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("window aspect must be a number or \"W:H\"")
	}
	*a = Aspect(f)
	return nil
}

// ParseAspect reads "16:9" (or "1.5") into width / height.
func ParseAspect(s string) (float64, error) {
	bad := fmt.Errorf("window aspect must be a number or \"W:H\"")
	w, h, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		f, err := strconv.ParseFloat(w, 64)
		if err != nil {
			return 0, bad
		}
		return f, nil
	}
	wf, err1 := strconv.ParseFloat(w, 64)
	hf, err2 := strconv.ParseFloat(h, 64)
	if err1 != nil || err2 != nil || hf <= 0 {
		return 0, bad
	}
	return wf / hf, nil
}

// WindowSpec is the placement a program asked for.
type WindowSpec struct {
	Aspect Aspect `json:"aspect"`
	Align  string `json:"align,omitempty"`
}

func normalizeWindow(w *WindowSpec) error {
	if w == nil {
		return nil
	}
	f := float64(w.Aspect)
	if f != f || f < minAspect || f > maxAspect {
		return userErrorf("window aspect must be between %g and %g", minAspect, maxAspect)
	}
	switch w.Align {
	case "":
		w.Align = AlignCenter
	case AlignCenter:
	default:
		return userErrorf("window align must be %q", AlignCenter)
	}
	return nil
}

// PlaceRect is the largest rect of the given aspect that fits the screen's
// width (and height), centred. A 1080x1920 screen at 16:9 gives
// {0, 656, 1080, 608}. It returns the zero Rect for unusable input.
func PlaceRect(screen Screen, aspect float64) Rect {
	if screen.W <= 0 || screen.H <= 0 || !(aspect > 0) {
		return Rect{}
	}
	w := screen.W
	h := int(float64(w)/aspect + 0.5)
	if h > screen.H {
		h = screen.H
		w = int(float64(h)*aspect + 0.5)
		if w > screen.W {
			w = screen.W
		}
	}
	return Rect{X: (screen.W - w) / 2, Y: (screen.H - h) / 2, W: w, H: h}
}

// WindowEvent is the `native.window` bridge event. Rect is nil when no
// placement applies (session ended, or the program asked for none).
type WindowEvent struct {
	AppID  string `json:"app_id"`
	Rect   *Rect  `json:"rect"`
	Screen Screen `json:"screen"`
}

// Placer keeps a program's window placed while the session lives.
type Placer interface {
	// Place blocks until stop is closed. It calls report whenever it applies
	// or re-applies a placement that changed the window's rect.
	Place(pid int, spec WindowSpec, stop <-chan struct{}, report func(Rect, Screen))
}

type noPlacer struct{}

func (noPlacer) Place(int, WindowSpec, <-chan struct{}, func(Rect, Screen)) {}

// SystemPlacer is the platform Placer: real on Windows, a no-op elsewhere.
type SystemPlacer struct{}
