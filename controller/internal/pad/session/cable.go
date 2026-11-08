package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	cableChannel     = "PlaySessionChannel"
	cableReadIdle    = 45 * time.Second
	cableWriteBudget = 10 * time.Second
	subscribeBudget  = 10 * time.Second

	// Signal types the Rails channel relays today.
	signalOffer  = "offer"
	signalAnswer = "answer"
	signalICE    = "ice"
	signalBye    = "bye"
	signalHello  = "hello"

	// signalControl is the ICE-failure fallback: control messages relayed
	// over the cable when a player's data channel cannot connect. Rails
	// forwards it (PlaySessionChannel allows the type). See the package doc.
	signalControl = "control"

	maxSignalBytes = 64 * 1024
)

var errSubscriptionRejected = errors.New("pad session: signaling subscription rejected")

type cableIdent struct {
	Channel   string `json:"channel"`
	SessionID string `json:"session_id"`
}

type cableCommand struct {
	Command    string `json:"command"`
	Identifier string `json:"identifier"`
	Data       string `json:"data,omitempty"`
}

type cableEnvelope struct {
	Type       string          `json:"type"`
	Identifier string          `json:"identifier"`
	Message    json.RawMessage `json:"message"`
	Reason     string          `json:"reason"`
}

// cableMessage is a PlaySessionChannel payload delivered to the kiosk.
type cableMessage struct {
	From   string          `json:"from"`
	Type   string          `json:"type"`
	Player *Player         `json:"player"`
	Data   json.RawMessage `json:"data"`
}

type cable struct {
	conn    *websocket.Conn
	ident   string
	writeMu sync.Mutex
	confirm chan error
}

func dialCable(ctx context.Context, serverURL, token, sessionID string, dialer *websocket.Dialer) (*cable, error) {
	wsURL, err := cableURL(serverURL)
	if err != nil {
		return nil, err
	}
	identRaw, err := json.Marshal(cableIdent{Channel: cableChannel, SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	if dialer == nil {
		copied := *websocket.DefaultDialer
		copied.HandshakeTimeout = subscribeBudget
		dialer = &copied
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	if origin := cableOrigin(wsURL); origin != "" {
		header.Set("Origin", origin)
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("pad session: cable: %w", err)
	}
	return &cable{
		conn:    conn,
		ident:   string(identRaw),
		confirm: make(chan error, 1),
	}, nil
}

// cableOrigin is the server's own origin. Action Cable accepts that, and
// rejects a foreign Origin even when the bearer token is valid.
func cableOrigin(wsURL string) string {
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return ""
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	default:
		return ""
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func cableURL(serverURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(serverURL, "/") + "/cable")
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("pad session: unsupported server URL scheme %q", parsed.Scheme)
	}
	return parsed.String(), nil
}

func (c *cable) Close() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.Close()
}

func (c *cable) subscribe(ctx context.Context) error {
	if err := c.write(cableCommand{Command: "subscribe", Identifier: c.ident}); err != nil {
		return err
	}
	timer := time.NewTimer(subscribeBudget)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.confirm:
		return err
	case <-timer.C:
		return errors.New("pad session: signaling subscription timed out")
	}
}

// signal performs PlaySessionChannel#signal. data must encode as a JSON object
// so Rails accepts it (a non-hash is dropped).
func (c *cable) signal(to, typ string, data any) error {
	if data == nil {
		data = map[string]any{}
	}
	payload := map[string]any{
		"action": "signal",
		"type":   typ,
		"data":   data,
	}
	if to != "" {
		payload["to"] = to
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) > maxSignalBytes {
		return fmt.Errorf("pad session: signal %s is %d bytes", typ, len(raw))
	}
	return c.write(cableCommand{Command: "message", Identifier: c.ident, Data: string(raw)})
}

func (c *cable) write(cmd cableCommand) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(cableWriteBudget))
	return c.conn.WriteJSON(cmd)
}

func (c *cable) readLoop(ctx context.Context, handle func(cableMessage)) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(cableReadIdle))
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("pad session: cable: %w", err)
		}
		var env cableEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case "ping", "welcome":
			continue
		case "confirm_subscription":
			if env.Identifier == c.ident {
				c.notify(nil)
			}
			continue
		case "reject_subscription":
			if env.Identifier == c.ident {
				c.notify(errSubscriptionRejected)
				return errSubscriptionRejected
			}
			continue
		case "disconnect":
			if env.Reason == "" {
				env.Reason = "disconnect"
			}
			return fmt.Errorf("pad session: cable %s", env.Reason)
		}
		if env.Identifier != c.ident || len(env.Message) == 0 || string(env.Message) == "null" {
			continue
		}
		var msg cableMessage
		if err := json.Unmarshal(env.Message, &msg); err != nil || msg.Type == "" {
			continue
		}
		handle(msg)
	}
}

func (c *cable) notify(err error) {
	select {
	case c.confirm <- err:
	default:
	}
}
