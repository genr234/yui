package cdp

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// Gesture targets, used by manifest keymaps (plan 07) so embedded web games
// work from a phone with no app code:
//
//	swipe:up=K,down=K,left=K,right=K
//	             relative trackpad `change` events are summed per gesture;
//	             once the finger has travelled SwipeThreshold px mostly along
//	             one axis, key K for that direction is tapped (one tap per
//	             gesture; a pause of SwipeGap re-arms). `swipe` events whose
//	             value is a direction ("up", or {"direction": "up"}) tap too.
//	mouse        an absolute trackpad drives the left mouse button: `press`
//	mouse:X,Y,W,H arms it, the next input frame moves to the touch point and
//	             presses, later frames drag, `release` lets go. The pointer
//	             axes (−32767…32767) map onto the rectangle X,Y,W,H in CSS
//	             pixels (an embedded game's frame), or the whole viewport.
//	             The hub's neutral frame (all zero) releases the button.

// SwipeThreshold is how far (trackpad px) a gesture travels before it counts.
const SwipeThreshold = 36.0

// SwipeGap is the pause between `change` events that ends a gesture.
const SwipeGap = 150 * time.Millisecond

// Directions a swipe target maps.
var swipeDirections = []string{"up", "down", "left", "right"}

// Rect is a region of the page viewport in CSS pixels.
type Rect struct {
	X, Y, W, H float64
}

// ValidKey reports whether name is a key the key: and swipe: targets accept.
func ValidKey(name string) bool {
	_, ok := lookupKey(name)
	return ok
}

// KeyNames lists the named keys (letters and digits are also accepted).
func KeyNames() []string {
	out := make([]string, 0, len(keys))
	for name := range keys {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// SwipeTarget builds a swipe: target. keys maps directions (up, down, left,
// right) to key names; missing directions do nothing.
func SwipeTarget(keys map[string]string) (string, error) {
	var parts []string
	for _, dir := range swipeDirections {
		key, ok := keys[dir]
		if !ok || key == "" {
			continue
		}
		if !ValidKey(key) {
			return "", fmt.Errorf("cdp: unknown key %q", key)
		}
		parts = append(parts, dir+"="+key)
	}
	for dir := range keys {
		if !validDirection(dir) {
			return "", fmt.Errorf("cdp: unknown swipe direction %q", dir)
		}
	}
	if len(parts) == 0 {
		return "", errors.New("cdp: a swipe needs at least one key")
	}
	return "swipe:" + strings.Join(parts, ","), nil
}

// MouseTarget builds a mouse target, limited to region when it is set.
func MouseTarget(region *Rect) string {
	if region == nil || region.W <= 0 || region.H <= 0 {
		return "mouse"
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return "mouse:" + f(region.X) + "," + f(region.Y) + "," + f(region.W) + "," + f(region.H)
}

func validDirection(dir string) bool {
	for _, d := range swipeDirections {
		if d == dir {
			return true
		}
	}
	return false
}

func parseSwipeTarget(target string) (map[string]keyDef, error) {
	spec := strings.TrimPrefix(target, "swipe:")
	out := map[string]keyDef{}
	for _, part := range strings.Split(spec, ",") {
		dir, name, ok := strings.Cut(part, "=")
		if !ok || !validDirection(dir) {
			return nil, fmt.Errorf("cdp: bad swipe target %q", target)
		}
		key, ok := lookupKey(name)
		if !ok {
			return nil, fmt.Errorf("cdp: unknown key %q", name)
		}
		out[dir] = key
	}
	return out, nil
}

func parseMouseTarget(target string) (*Rect, error) {
	if target == "mouse" {
		return nil, nil
	}
	spec := strings.TrimPrefix(target, "mouse:")
	parts := strings.Split(spec, ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("cdp: bad mouse target %q", target)
	}
	var v [4]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(p, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("cdp: bad mouse target %q", target)
		}
		v[i] = f
	}
	if v[2] <= 0 || v[3] <= 0 {
		return nil, fmt.Errorf("cdp: bad mouse target %q", target)
	}
	return &Rect{X: v[0], Y: v[1], W: v[2], H: v[3]}, nil
}

type swipeState struct {
	ax, ay float64
	last   time.Time
	armed  bool
}

// swipe handles a swipe: output.
func (a *Adapter) swipe(playerID string, out hub.Output) error {
	keysByDir, err := parseSwipeTarget(out.Target)
	if err != nil {
		return err
	}
	var dir string
	switch out.Kind {
	case "swipe":
		dir = swipeDirection(out.Value)
	case "change":
		value, _ := out.Value.(map[string]any)
		dir = a.trackSwipe(playerID+"\x00"+out.Target, number(value, "dx"), number(value, "dy"))
	default:
		return nil
	}
	if key, ok := keysByDir[dir]; ok {
		a.enqueue(op{kind: opKey, player: playerID, key: key, tap: true})
	}
	return nil
}

// trackSwipe adds one trackpad delta to the player's gesture and returns
// the direction when the gesture first crosses the threshold.
func (a *Adapter) trackSwipe(id string, dx, dy float64) string {
	now := a.clock()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.swipes[id]
	if s == nil || now.Sub(s.last) > SwipeGap {
		s = &swipeState{armed: true}
		a.swipes[id] = s
	}
	s.last = now
	if !s.armed {
		return ""
	}
	s.ax += dx
	s.ay += dy
	ax, ay := math.Abs(s.ax), math.Abs(s.ay)
	if math.Max(ax, ay) < SwipeThreshold {
		return ""
	}
	s.armed = false
	// Trackpad deltas are screen-like: +dy is down.
	if ax > ay {
		if s.ax > 0 {
			return "right"
		}
		return "left"
	}
	if s.ay > 0 {
		return "down"
	}
	return "up"
}

func swipeDirection(v any) string {
	switch t := v.(type) {
	case string:
		if validDirection(t) {
			return t
		}
	case map[string]any:
		if d, ok := t["direction"].(string); ok && validDirection(d) {
			return d
		}
	}
	return ""
}

// mouse handles a mouse / mouse:X,Y,W,H output.
func (a *Adapter) mouse(playerID string, out hub.Output) error {
	region, err := parseMouseTarget(out.Target)
	if err != nil {
		return err
	}
	switch out.Kind {
	case hub.KindState:
		st, ok := out.Value.(protocol.InputState)
		if !ok {
			return nil
		}
		a.mu.Lock()
		if st == (protocol.InputState{}) {
			// The hub's neutral frame: focus moved or the player left.
			down := a.mouseDown
			a.mouseDown, a.mousePending = false, false
			a.mu.Unlock()
			if down {
				a.enqueue(op{kind: opAbsRelease, player: playerID})
			}
			return nil
		}
		if !a.mouseDown && !a.mousePending {
			// Frames from other controls (a held d-pad) do not move it.
			a.mu.Unlock()
			return nil
		}
		press := a.mousePending
		a.mousePending, a.mouseDown = false, true
		a.mu.Unlock()
		a.enqueue(op{
			kind:   opAbsMove,
			player: playerID,
			nx:     (float64(st.PX) + 32767) / 65534,
			ny:     (float64(st.PY) + 32767) / 65534,
			region: region,
			down:   press,
		})
	case "press":
		a.mu.Lock()
		if !a.mouseDown {
			a.mousePending = true
		}
		a.mu.Unlock()
	case "release":
		a.mu.Lock()
		down, pending := a.mouseDown, a.mousePending
		a.mouseDown, a.mousePending = false, false
		a.mu.Unlock()
		switch {
		case down:
			a.enqueue(op{kind: opAbsRelease, player: playerID})
		case pending:
			// Released before any frame arrived: a tap where the cursor is.
			a.enqueue(op{kind: opMouse, player: playerID, tap: true})
		}
	}
	return nil
}

func (a *Adapter) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}
