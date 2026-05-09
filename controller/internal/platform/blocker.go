package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"kiosk/controller/internal/blocker"
)

type cdpRequestPausedParams struct {
	RequestID    string `json:"requestId"`
	FrameID      string `json:"frameId"`
	ResourceType string `json:"resourceType"`
	Request      struct {
		URL string `json:"url"`
	} `json:"request"`
}

type cdpFrameNavigatedParams struct {
	Frame cdpFrame `json:"frame"`
}

type cdpFrameDetachedParams struct {
	FrameID string `json:"frameId"`
}

type cdpFrame struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	URL      string `json:"url"`
}

func (r *Runtime) blockerLoop(ctx context.Context) {
	if r.cfg.PlatformDebugPort <= 0 {
		return
	}

	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := r.blockerOnce(ctx); err != nil {
				log.Printf("embed blocker retry later: %v", err)
			}
		}
	}
}

func (r *Runtime) blockerOnce(ctx context.Context) error {
	target, err := r.activePageTarget(ctx)
	if err != nil {
		return err
	}
	if target.WebSocketDebuggerURL == "" {
		return fmt.Errorf("active page has no debugger websocket")
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("connect cdp blocker target: %w", err)
	}
	defer conn.Close()

	session := newBlockerSession(conn)
	if err := session.send("Page.enable", nil); err != nil {
		return fmt.Errorf("enable page domain: %w", err)
	}
	if err := session.send("Fetch.enable", map[string]any{
		"patterns": []map[string]any{
			{"urlPattern": "http://*/*", "requestStage": "Request"},
			{"urlPattern": "https://*/*", "requestStage": "Request"},
		},
	}); err != nil {
		return fmt.Errorf("enable fetch domain: %w", err)
	}

	log.Printf("embed blocker attached to target %s", target.ID)
	return session.readLoop(ctx)
}

type blockerSession struct {
	conn         *websocket.Conn
	nextID       int
	frameOrigins map[string]string
}

func newBlockerSession(conn *websocket.Conn) *blockerSession {
	return &blockerSession{
		conn:         conn,
		nextID:       1000,
		frameOrigins: make(map[string]string),
	}
}

func (s *blockerSession) send(method string, params map[string]any) error {
	s.nextID++
	return s.conn.WriteJSON(cdpMessage{ID: s.nextID, Method: method, Params: params})
}

func (s *blockerSession) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var msg cdpMessage
		if err := s.conn.ReadJSON(&msg); err != nil {
			return err
		}

		switch msg.Method {
		case "Page.frameNavigated":
			s.handleFrameNavigated(msg.Params)
		case "Page.frameDetached":
			s.handleFrameDetached(msg.Params)
		case "Fetch.requestPaused":
			s.handleRequestPaused(msg.Params)
		}
	}
}

func (s *blockerSession) handleFrameNavigated(params map[string]any) {
	data, _ := json.Marshal(params)
	var event cdpFrameNavigatedParams
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}
	origin := originForURL(event.Frame.URL)
	if origin == "" {
		delete(s.frameOrigins, event.Frame.ID)
		return
	}
	s.frameOrigins[event.Frame.ID] = origin
}

func (s *blockerSession) handleFrameDetached(params map[string]any) {
	data, _ := json.Marshal(params)
	var event cdpFrameDetachedParams
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}
	delete(s.frameOrigins, event.FrameID)
}

func (s *blockerSession) handleRequestPaused(params map[string]any) {
	data, _ := json.Marshal(params)
	var event cdpRequestPausedParams
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}

	frameOrigin := s.frameOrigins[event.FrameID]
	blocked := blocker.DefaultManager.ShouldBlock(frameOrigin, event.Request.URL, event.ResourceType)
	if blocked {
		_ = s.send("Fetch.failRequest", map[string]any{
			"requestId":   event.RequestID,
			"errorReason": "BlockedByClient",
		})
		log.Printf("embed blocker blocked %s for %s", event.Request.URL, frameOrigin)
		return
	}

	_ = s.send("Fetch.continueRequest", map[string]any{
		"requestId": event.RequestID,
	})
}

func originForURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}
