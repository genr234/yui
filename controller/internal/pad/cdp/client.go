// Package cdp drives the kiosk's Chrome page over the DevTools protocol for
// the input hub: pointer, wheel, clicks and text from phones (plan 03).
//
// Input goes to the page target, not to a frame, so Chrome hit-tests the
// coordinates itself and events reach cross-origin iframes too.
package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Caller sends one CDP command and waits for its result.
type Caller interface {
	Call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error)
}

type target struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type message struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params map[string]any  `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type reply struct {
	result json.RawMessage
	err    error
}

// PageClient keeps one DevTools websocket to the first page target (the same
// target the overlay is injected into) and redials after errors.
type PageClient struct {
	base   string
	http   *http.Client
	dialer *websocket.Dialer

	mu      sync.Mutex
	conn    *websocket.Conn
	nextID  int
	pending map[int]chan reply
	writeMu sync.Mutex
}

// NewPageClient talks to Chrome's remote debugging endpoint, for example
// "http://127.0.0.1:9222".
func NewPageClient(base string) *PageClient {
	return &PageClient{
		base:    strings.TrimRight(base, "/"),
		http:    &http.Client{Timeout: 2 * time.Second},
		dialer:  &websocket.Dialer{HandshakeTimeout: 2 * time.Second},
		pending: map[int]chan reply{},
	}
}

// Call implements Caller.
func (c *PageClient) Call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	err = conn.WriteJSON(message{ID: id, Method: method, Params: params})
	_ = conn.SetWriteDeadline(time.Time{})
	c.writeMu.Unlock()
	if err != nil {
		c.drop(conn, err)
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Close drops the connection; the next Call redials.
func (c *PageClient) Close() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		c.drop(conn, errors.New("cdp: closed"))
	}
}

func (c *PageClient) connect(ctx context.Context) (*websocket.Conn, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		return conn, nil
	}
	wsURL, err := c.pageURL(ctx)
	if err != nil {
		return nil, err
	}
	conn, _, err = c.dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp: dial page: %w", err)
	}
	c.mu.Lock()
	if c.conn != nil {
		existing := c.conn
		c.mu.Unlock()
		_ = conn.Close()
		return existing, nil
	}
	c.conn = conn
	c.mu.Unlock()
	go c.readLoop(conn)
	return conn, nil
}

func (c *PageClient) pageURL(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/json", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("cdp: list targets: %w", err)
	}
	defer resp.Body.Close()
	var targets []target
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return "", fmt.Errorf("cdp: decode targets: %w", err)
	}
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL, nil
		}
	}
	return "", errors.New("cdp: no page target")
}

func (c *PageClient) readLoop(conn *websocket.Conn) {
	for {
		var msg message
		if err := conn.ReadJSON(&msg); err != nil {
			c.drop(conn, err)
			return
		}
		if msg.ID == 0 {
			continue // an event
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			ch <- reply{err: fmt.Errorf("cdp: %s", msg.Error)}
			continue
		}
		ch <- reply{result: msg.Result}
	}
}

// drop closes conn if it is current and fails its pending calls.
func (c *PageClient) drop(conn *websocket.Conn, err error) {
	c.mu.Lock()
	if c.conn != conn {
		c.mu.Unlock()
		return
	}
	c.conn = nil
	pending := c.pending
	c.pending = map[int]chan reply{}
	c.mu.Unlock()
	_ = conn.Close()
	for _, ch := range pending {
		ch <- reply{err: fmt.Errorf("cdp: connection lost: %w", err)}
	}
}
