package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// Name is the adapter name used in routes.
const Name = "cdp"

// CursorEvent is the bridge push event the overlay draws the cursor from.
const CursorEvent = "pad.cursor"

// Cursor is the pad.cursor event payload. X and Y are CSS pixels in the
// page viewport.
type Cursor struct {
	PlayerID string  `json:"player_id,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Visible  bool    `json:"visible"`
	Color    string  `json:"color,omitempty"`
	Click    bool    `json:"click,omitempty"`
}

// Options configures the adapter.
type Options struct {
	Caller Caller
	// Sensitive reports D9: while true, pointer, click, key and text output
	// is dropped entirely.
	Sensitive func() bool
	// Emit pushes bridge events (pad.cursor).
	Emit func(event string, data any)
	// Color returns the player's colour name for the cursor.
	Color func(playerID string) string
	// Gain scales trackpad pixels to page pixels (default 1.4); fast swipes
	// accelerate up to three times that.
	Gain float64
	// ScrollGain scales two-finger pixels to wheel delta (default 2.5).
	ScrollGain float64
	// CallTimeout bounds one CDP command (default 2 s).
	CallTimeout time.Duration
}

// Adapter implements hub.Adapter for the Chrome page.
//
// Targets (hub.Route.Target):
//
//	pointer      trackpad events: change {dx, dy, fingers} moves the cursor
//	             (1 finger) or scrolls (2 fingers); tap {fingers:1} clicks
//	text         text ops (KindText): insert → Input.insertText,
//	             backspace / enter → key events
//	key:<Key>    a key held by a button level or press/release/tap events
//	             (Enter, Backspace, Tab, Escape, Space, ArrowUp, ArrowDown,
//	             ArrowLeft, ArrowRight, Home, End, PageUp, PageDown, Delete,
//	             or one letter or digit)
//	click        the left mouse button at the cursor, by level or events
//
// Commands run in order on one worker (Run); Deliver never blocks on Chrome.
type Adapter struct {
	opts  Options
	queue chan op

	mu        sync.Mutex
	x, y      float64
	placed    bool
	vw, vh    float64
	metricsAt time.Time
	held      map[string]bool
	lastErr   time.Time
}

type opKind int

const (
	opMove opKind = iota
	opScroll
	opClick
	opMouse
	opInsert
	opKey
	opNavigate
)

type op struct {
	kind   opKind
	player string
	dx, dy float64
	text   string
	key    keyDef
	down   bool
	tap    bool
}

// New builds an adapter. Call Run to start executing commands.
func New(opts Options) *Adapter {
	if opts.Gain <= 0 {
		opts.Gain = 1.4
	}
	if opts.ScrollGain <= 0 {
		opts.ScrollGain = 2.5
	}
	if opts.CallTimeout <= 0 {
		opts.CallTimeout = 2 * time.Second
	}
	return &Adapter{opts: opts, queue: make(chan op, 256), held: map[string]bool{}}
}

// Name implements hub.Adapter.
func (a *Adapter) Name() string { return Name }

func (a *Adapter) sensitive() bool {
	return a.opts.Sensitive != nil && a.opts.Sensitive()
}

// Deliver implements hub.Adapter. Output is dropped while sensitive (D9).
func (a *Adapter) Deliver(playerID string, outputs []hub.Output) error {
	if a.sensitive() {
		return nil
	}
	for _, out := range outputs {
		switch {
		case out.Target == "pointer":
			a.pointer(playerID, out)
		case out.Target == "text":
			a.text(playerID, out)
		case strings.HasPrefix(out.Target, "key:"):
			key, ok := lookupKey(strings.TrimPrefix(out.Target, "key:"))
			if !ok {
				return fmt.Errorf("cdp: unknown key %q", out.Target)
			}
			a.level(playerID, out, op{kind: opKey, player: playerID, key: key})
		case out.Target == "click":
			a.level(playerID, out, op{kind: opMouse, player: playerID})
		default:
			return fmt.Errorf("cdp: unknown target %q", out.Target)
		}
	}
	return nil
}

func (a *Adapter) pointer(playerID string, out hub.Output) {
	value, _ := out.Value.(map[string]any)
	fingers := int(number(value, "fingers"))
	if fingers <= 0 {
		fingers = 1
	}
	switch out.Kind {
	case "change":
		dx, dy := number(value, "dx"), number(value, "dy")
		if dx == 0 && dy == 0 {
			return
		}
		kind := opMove
		if fingers >= 2 {
			kind = opScroll
		}
		a.enqueue(op{kind: kind, player: playerID, dx: dx, dy: dy})
	case "tap":
		if fingers == 1 {
			a.enqueue(op{kind: opClick, player: playerID})
		}
	}
}

func (a *Adapter) text(playerID string, out hub.Output) {
	msg, ok := out.Value.(protocol.Text)
	if !ok {
		return
	}
	switch msg.Op {
	case "insert":
		if msg.Text != "" {
			a.enqueue(op{kind: opInsert, player: playerID, text: msg.Text})
		}
	case "backspace":
		a.enqueue(op{kind: opKey, player: playerID, key: keys["Backspace"], tap: true})
	case "enter":
		a.enqueue(op{kind: opKey, player: playerID, key: keys["Enter"], tap: true})
	}
}

// level turns a button level or press/release/tap event into down/up ops,
// sending only changes so a 60 Hz frame does not repeat a held key.
func (a *Adapter) level(playerID string, out hub.Output, base op) {
	var down bool
	switch out.Kind {
	case hub.KindButton:
		v, ok := out.Value.(bool)
		if !ok {
			return
		}
		down = v
	case "press":
		down = true
	case "release":
		down = false
	case "tap":
		base.tap = true
		a.enqueue(base)
		return
	default:
		return
	}
	id := playerID + "\x00" + out.Target
	a.mu.Lock()
	changed := a.held[id] != down
	if down {
		a.held[id] = true
	} else {
		delete(a.held, id)
	}
	a.mu.Unlock()
	if changed {
		base.down = down
		a.enqueue(base)
	}
}

func (a *Adapter) enqueue(o op) {
	select {
	case a.queue <- o:
	default: // input is lossy; a stuck Chrome must not stall the pads
	}
}

// Navigate loads url in the page (the remote's Home button). Dropped while
// sensitive.
func (a *Adapter) Navigate(url string) {
	if url == "" || a.sensitive() {
		return
	}
	a.enqueue(op{kind: opNavigate, text: url})
}

// Hide tells the overlay to hide the cursor (session end, sensitive surface).
func (a *Adapter) Hide() {
	a.mu.Lock()
	x, y := a.x, a.y
	a.mu.Unlock()
	a.emit(Cursor{X: x, Y: y, Visible: false})
}

// Run executes queued commands until ctx ends.
func (a *Adapter) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-a.queue:
			if err := a.exec(ctx, o); err != nil {
				a.logErr(err)
			}
		}
	}
}

func (a *Adapter) logErr(err error) {
	a.mu.Lock()
	quiet := time.Since(a.lastErr) < 5*time.Second
	if !quiet {
		a.lastErr = time.Now()
	}
	a.mu.Unlock()
	if !quiet {
		log.Printf("pad cdp: %v", err)
	}
}

func (a *Adapter) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if a.opts.Caller == nil {
		return nil, fmt.Errorf("no CDP connection")
	}
	callCtx, cancel := context.WithTimeout(ctx, a.opts.CallTimeout)
	defer cancel()
	return a.opts.Caller.Call(callCtx, method, params)
}

func (a *Adapter) exec(ctx context.Context, o op) error {
	// D9 is checked again here: the surface may have turned sensitive
	// after the command was queued.
	if a.sensitive() {
		return nil
	}
	switch o.kind {
	case opMove:
		a.viewport(ctx)
		x, y := a.move(o.dx, o.dy)
		a.emit(Cursor{PlayerID: o.player, X: x, Y: y, Visible: true, Color: a.color(o.player)})
		_, err := a.call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x, "y": y})
		return err
	case opScroll:
		a.viewport(ctx)
		x, y := a.position()
		_, err := a.call(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": "mouseWheel", "x": x, "y": y,
			// Natural scrolling: fingers moving up scroll the page down.
			"deltaX": -o.dx * a.opts.ScrollGain,
			"deltaY": -o.dy * a.opts.ScrollGain,
		})
		return err
	case opClick:
		a.viewport(ctx)
		x, y := a.position()
		a.emit(Cursor{PlayerID: o.player, X: x, Y: y, Visible: true, Color: a.color(o.player), Click: true})
		for _, typ := range []string{"mousePressed", "mouseReleased"} {
			buttons := 1
			if typ == "mouseReleased" {
				buttons = 0
			}
			if _, err := a.call(ctx, "Input.dispatchMouseEvent", map[string]any{
				"type": typ, "x": x, "y": y, "button": "left", "buttons": buttons, "clickCount": 1,
			}); err != nil {
				return err
			}
		}
		return nil
	case opMouse:
		a.viewport(ctx)
		x, y := a.position()
		types := []string{"mouseReleased"}
		if o.tap {
			types = []string{"mousePressed", "mouseReleased"}
		} else if o.down {
			types = []string{"mousePressed"}
		}
		for _, typ := range types {
			buttons := 0
			if typ == "mousePressed" {
				buttons = 1
			}
			if _, err := a.call(ctx, "Input.dispatchMouseEvent", map[string]any{
				"type": typ, "x": x, "y": y, "button": "left", "buttons": buttons, "clickCount": 1,
			}); err != nil {
				return err
			}
		}
		return nil
	case opInsert:
		_, err := a.call(ctx, "Input.insertText", map[string]any{"text": o.text})
		return err
	case opKey:
		if o.tap || o.down {
			if _, err := a.call(ctx, "Input.dispatchKeyEvent", o.key.params(true)); err != nil {
				return err
			}
		}
		if o.tap || !o.down {
			_, err := a.call(ctx, "Input.dispatchKeyEvent", o.key.params(false))
			return err
		}
		return nil
	case opNavigate:
		_, err := a.call(ctx, "Page.navigate", map[string]any{"url": o.text})
		return err
	}
	return nil
}

// viewport refreshes the page size every few seconds.
func (a *Adapter) viewport(ctx context.Context) {
	a.mu.Lock()
	fresh := a.vw > 0 && time.Since(a.metricsAt) < 5*time.Second
	a.mu.Unlock()
	if fresh {
		return
	}
	w, h := 1920.0, 1080.0
	if raw, err := a.call(ctx, "Page.getLayoutMetrics", nil); err == nil {
		var m struct {
			CSS    viewportSize `json:"cssLayoutViewport"`
			Legacy viewportSize `json:"layoutViewport"`
		}
		if json.Unmarshal(raw, &m) == nil {
			switch {
			case m.CSS.ClientWidth > 0 && m.CSS.ClientHeight > 0:
				w, h = m.CSS.ClientWidth, m.CSS.ClientHeight
			case m.Legacy.ClientWidth > 0 && m.Legacy.ClientHeight > 0:
				w, h = m.Legacy.ClientWidth, m.Legacy.ClientHeight
			}
		}
	}
	a.mu.Lock()
	a.vw, a.vh = w, h
	a.metricsAt = time.Now()
	if !a.placed {
		a.x, a.y = w/2, h/2
		a.placed = true
	}
	a.x, a.y = clamp(a.x, 0, w-1), clamp(a.y, 0, h-1)
	a.mu.Unlock()
}

type viewportSize struct {
	ClientWidth  float64 `json:"clientWidth"`
	ClientHeight float64 `json:"clientHeight"`
}

func (a *Adapter) move(dx, dy float64) (float64, float64) {
	speed := math.Hypot(dx, dy)
	f := a.opts.Gain * (1 + 0.04*math.Min(speed, 50))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.x = clamp(a.x+dx*f, 0, a.vw-1)
	a.y = clamp(a.y+dy*f, 0, a.vh-1)
	return a.x, a.y
}

func (a *Adapter) position() (float64, float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.x, a.y
}

func (a *Adapter) color(playerID string) string {
	if a.opts.Color == nil {
		return ""
	}
	return a.opts.Color(playerID)
}

func (a *Adapter) emit(c Cursor) {
	if a.opts.Emit != nil {
		a.opts.Emit(CursorEvent, c)
	}
}

func clamp(v, lo, hi float64) float64 {
	if hi < lo {
		hi = lo
	}
	return math.Max(lo, math.Min(hi, v))
}

func number(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

type keyDef struct {
	key  string
	code string
	vk   int
	text string
}

func (k keyDef) params(down bool) map[string]any {
	p := map[string]any{
		"key":                   k.key,
		"code":                  k.code,
		"windowsVirtualKeyCode": k.vk,
		"nativeVirtualKeyCode":  k.vk,
	}
	switch {
	case !down:
		p["type"] = "keyUp"
	case k.text != "":
		// keyDown with text also produces keypress/input, so Enter submits.
		p["type"] = "keyDown"
		p["text"] = k.text
		p["unmodifiedText"] = k.text
	default:
		p["type"] = "rawKeyDown"
	}
	return p
}

var keys = map[string]keyDef{
	"Enter":      {"Enter", "Enter", 13, "\r"},
	"Backspace":  {"Backspace", "Backspace", 8, ""},
	"Tab":        {"Tab", "Tab", 9, ""},
	"Escape":     {"Escape", "Escape", 27, ""},
	"Space":      {" ", "Space", 32, " "},
	"PageUp":     {"PageUp", "PageUp", 33, ""},
	"PageDown":   {"PageDown", "PageDown", 34, ""},
	"End":        {"End", "End", 35, ""},
	"Home":       {"Home", "Home", 36, ""},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"ArrowUp":    {"ArrowUp", "ArrowUp", 38, ""},
	"ArrowRight": {"ArrowRight", "ArrowRight", 39, ""},
	"ArrowDown":  {"ArrowDown", "ArrowDown", 40, ""},
	"Delete":     {"Delete", "Delete", 46, ""},
}

func lookupKey(name string) (keyDef, bool) {
	if k, ok := keys[name]; ok {
		return k, true
	}
	if len(name) == 1 {
		c := name[0]
		switch {
		case c >= 'a' && c <= 'z':
			return keyDef{name, "Key" + strings.ToUpper(name), int(c - 'a' + 'A'), name}, true
		case c >= 'A' && c <= 'Z':
			return keyDef{strings.ToLower(name), "Key" + name, int(c), strings.ToLower(name)}, true
		case c >= '0' && c <= '9':
			return keyDef{name, "Digit" + name, int(c), name}, true
		}
	}
	return keyDef{}, false
}
