package cdp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"kiosk/controller/internal/pad/hub"
	"kiosk/controller/internal/pad/protocol"
)

// fakeTarget is a DevTools endpoint with one page target. It records every
// command and answers Page.getLayoutMetrics with a 1000×500 viewport.
type fakeTarget struct {
	srv   *httptest.Server
	calls chan message
	dials atomic.Int32
	mu    sync.Mutex
	conns []*websocket.Conn
}

func newFakeTarget(t *testing.T) *fakeTarget {
	t.Helper()
	f := &fakeTarget{calls: make(chan message, 256)}
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		ws := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/devtools/page/1"
		_ = json.NewEncoder(w).Encode([]target{
			{Type: "service_worker", WebSocketDebuggerURL: "ws://ignored"},
			{Type: "page", URL: "https://kiosk.example", WebSocketDebuggerURL: ws},
		})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.dials.Add(1)
		f.mu.Lock()
		f.conns = append(f.conns, conn)
		f.mu.Unlock()
		defer conn.Close()
		for {
			var msg message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Unsolicited events are ignored by the client.
			_ = conn.WriteJSON(map[string]any{"method": "Page.frameNavigated", "params": map[string]any{}})
			result := json.RawMessage(`{}`)
			if msg.Method == "Page.getLayoutMetrics" {
				result = json.RawMessage(`{"cssLayoutViewport":{"clientWidth":1000,"clientHeight":500}}`)
			}
			if err := conn.WriteJSON(message{ID: msg.ID, Result: result}); err != nil {
				return
			}
			if msg.Method != "Page.getLayoutMetrics" {
				f.calls <- msg
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		f.mu.Lock()
		for _, c := range f.conns {
			_ = c.Close()
		}
		f.mu.Unlock()
		f.srv.Close()
	})
	return f
}

func (f *fakeTarget) next(t *testing.T) message {
	t.Helper()
	select {
	case m := <-f.calls:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("no CDP command")
		return message{}
	}
}

func (f *fakeTarget) none(t *testing.T) {
	t.Helper()
	select {
	case m := <-f.calls:
		t.Fatalf("unexpected CDP command %s %v", m.Method, m.Params)
	case <-time.After(150 * time.Millisecond):
	}
}

func (f *fakeTarget) dropConnections() {
	f.mu.Lock()
	for _, c := range f.conns {
		_ = c.Close()
	}
	f.conns = nil
	f.mu.Unlock()
}

type rig struct {
	target    *fakeTarget
	adapter   *Adapter
	sensitive atomic.Bool
	mu        sync.Mutex
	cursors   []Cursor
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{target: newFakeTarget(t)}
	client := NewPageClient(r.target.srv.URL)
	t.Cleanup(client.Close)
	r.adapter = New(Options{
		Caller:    client,
		Sensitive: r.sensitive.Load,
		Emit: func(event string, data any) {
			if event != CursorEvent {
				t.Errorf("event %s", event)
				return
			}
			r.mu.Lock()
			r.cursors = append(r.cursors, data.(Cursor))
			r.mu.Unlock()
		},
		Color: func(string) string { return "teal" },
		Gain:  1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go r.adapter.Run(ctx)
	return r
}

func (r *rig) lastCursor() Cursor {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cursors) == 0 {
		return Cursor{}
	}
	return r.cursors[len(r.cursors)-1]
}

func pointer(kind string, value map[string]any) hub.Output {
	return hub.Output{Adapter: Name, Target: "pointer", Kind: kind, Value: value}
}

func num(m map[string]any, k string) float64 { return number(m, k) }

func TestPointerMoveClickAndScroll(t *testing.T) {
	r := newRig(t)
	// JSON numbers from the phone arrive as float64.
	if err := r.adapter.Deliver("p1", []hub.Output{pointer("change", map[string]any{"dx": 10.0, "dy": -5.0, "fingers": 1.0})}); err != nil {
		t.Fatal(err)
	}
	m := r.target.next(t)
	if m.Method != "Input.dispatchMouseEvent" || m.Params["type"] != "mouseMoved" {
		t.Fatalf("move %s %v", m.Method, m.Params)
	}
	// Starts at the viewport centre (500, 250); gain 1 with speed boost.
	x, y := num(m.Params, "x"), num(m.Params, "y")
	if x <= 510 || y >= 245 || x > 520 {
		t.Fatalf("moved to %v,%v", x, y)
	}
	c := r.lastCursor()
	if !c.Visible || c.X != x || c.Y != y || c.Color != "teal" || c.PlayerID != "p1" {
		t.Fatalf("cursor %+v", c)
	}

	// The cursor stays inside the viewport.
	_ = r.adapter.Deliver("p1", []hub.Output{pointer("change", map[string]any{"dx": 99999.0, "dy": 99999.0})})
	m = r.target.next(t)
	if num(m.Params, "x") != 999 || num(m.Params, "y") != 499 {
		t.Fatalf("clamp %v", m.Params)
	}

	_ = r.adapter.Deliver("p1", []hub.Output{pointer("tap", map[string]any{"fingers": 1.0})})
	press, release := r.target.next(t), r.target.next(t)
	if press.Params["type"] != "mousePressed" || release.Params["type"] != "mouseReleased" ||
		press.Params["button"] != "left" || num(press.Params, "x") != 999 || num(press.Params, "clickCount") != 1 {
		t.Fatalf("click %v / %v", press.Params, release.Params)
	}
	if !r.lastCursor().Click {
		t.Fatal("click ripple not emitted")
	}

	// Two fingers scroll; fingers moving up scroll the page down.
	_ = r.adapter.Deliver("p1", []hub.Output{pointer("change", map[string]any{"dx": 0.0, "dy": -10.0, "fingers": 2.0})})
	m = r.target.next(t)
	if m.Params["type"] != "mouseWheel" || num(m.Params, "deltaY") != 25 || num(m.Params, "x") != 999 {
		t.Fatalf("wheel %v", m.Params)
	}

	// A two-finger tap is not a click.
	_ = r.adapter.Deliver("p1", []hub.Output{pointer("tap", map[string]any{"fingers": 2.0})})
	r.target.none(t)
}

func TestTextAndKeys(t *testing.T) {
	r := newRig(t)
	text := func(op, s string) hub.Output {
		return hub.Output{Adapter: Name, Target: "text", Kind: hub.KindText, Value: protocol.Text{T: "text", Op: op, Text: s}}
	}
	_ = r.adapter.Deliver("p1", []hub.Output{text("insert", "héllo"), text("backspace", ""), text("enter", "")})
	m := r.target.next(t)
	if m.Method != "Input.insertText" || m.Params["text"] != "héllo" {
		t.Fatalf("insert %s %v", m.Method, m.Params)
	}
	down, up := r.target.next(t), r.target.next(t)
	if down.Params["type"] != "rawKeyDown" || down.Params["key"] != "Backspace" || num(down.Params, "windowsVirtualKeyCode") != 8 || up.Params["type"] != "keyUp" {
		t.Fatalf("backspace %v %v", down.Params, up.Params)
	}
	down, up = r.target.next(t), r.target.next(t)
	if down.Params["type"] != "keyDown" || down.Params["key"] != "Enter" || down.Params["text"] != "\r" || up.Params["type"] != "keyUp" {
		t.Fatalf("enter %v %v", down.Params, up.Params)
	}

	// A button level held over several frames presses once.
	key := func(v bool) hub.Output {
		return hub.Output{Adapter: Name, Target: "key:ArrowUp", Kind: hub.KindButton, Value: v}
	}
	_ = r.adapter.Deliver("p1", []hub.Output{key(true)})
	_ = r.adapter.Deliver("p1", []hub.Output{key(true)})
	_ = r.adapter.Deliver("p1", []hub.Output{key(false)})
	if m := r.target.next(t); m.Params["type"] != "rawKeyDown" || m.Params["key"] != "ArrowUp" {
		t.Fatalf("arrow down %v", m.Params)
	}
	if m := r.target.next(t); m.Params["type"] != "keyUp" {
		t.Fatalf("arrow up %v", m.Params)
	}
	r.target.none(t)

	_ = r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: "key:a", Kind: "tap"}})
	if m := r.target.next(t); m.Params["code"] != "KeyA" || m.Params["text"] != "a" || num(m.Params, "windowsVirtualKeyCode") != 65 {
		t.Fatalf("letter %v", m.Params)
	}
	r.target.next(t)

	if err := r.adapter.Deliver("p1", []hub.Output{{Adapter: Name, Target: "key:F13", Kind: "tap"}}); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestSensitiveDropsEverything(t *testing.T) {
	r := newRig(t)
	r.sensitive.Store(true)
	_ = r.adapter.Deliver("p1", []hub.Output{
		pointer("change", map[string]any{"dx": 4.0, "dy": 4.0}),
		pointer("tap", map[string]any{"fingers": 1.0}),
		{Adapter: Name, Target: "text", Kind: hub.KindText, Value: protocol.Text{T: "text", Op: "insert", Text: "1234"}},
		{Adapter: Name, Target: "key:Enter", Kind: "tap"},
	})
	r.adapter.Navigate("https://kiosk.example")
	r.target.none(t)
	if c := r.lastCursor(); c.Visible {
		t.Fatalf("cursor shown while sensitive: %+v", c)
	}

	// Queued before the flag flips, executed after: still dropped.
	r.sensitive.Store(false)
	r.adapter.enqueue(op{kind: opInsert, text: "x"})
	r.target.next(t)
}

func TestClientRedialsAfterDrop(t *testing.T) {
	r := newRig(t)
	r.adapter.Navigate("https://kiosk.example/home")
	if m := r.target.next(t); m.Method != "Page.navigate" || m.Params["url"] != "https://kiosk.example/home" {
		t.Fatalf("navigate %v", m)
	}
	r.target.dropConnections()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.adapter.enqueue(op{kind: opInsert, text: "again"})
		select {
		case m := <-r.target.calls:
			if m.Params["text"] == "again" {
				if r.target.dials.Load() < 2 {
					t.Fatal("did not redial")
				}
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("no command after the connection dropped")
}
