// CDP = Chrome DevTools Protocol

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type cdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type cdpMessage struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params map[string]any  `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  any             `json:"error,omitempty"`
}

func (r *Runtime) injectLoop(ctx context.Context) {
	if r.cfg.PlatformDevServer == "" && !staticHasPlatformBundle(r.staticFS) {
		log.Printf("platform bundle missing; run platform build before controller build")
	}

	// inject every 2 seconds until it works
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := r.injectOnce(ctx); err != nil {
				log.Printf("platform injection retry later: %v", err)
			}
		}
	}
}

func (r *Runtime) injectOnce(ctx context.Context) error {
	target, err := r.activePageTarget(ctx)
	if err != nil {
		return err
	}
	if target.WebSocketDebuggerURL == "" {
		return fmt.Errorf("active page has no debugger websocket")
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("connect cdp target: %w", err)
	}
	defer conn.Close()

	source, err := r.injectionSource()
	if err != nil {
		return err
	}

	if err := cdpCall(conn, 1, "Runtime.evaluate", map[string]any{
		"expression":                  source,
		"awaitPromise":                false,
		"returnByValue":               false,
		"allowUnsafeEvalBlockedByCSP": true,
	}); err != nil {
		return fmt.Errorf("evaluate overlay: %w", err)
	}

	return nil
}

func (r *Runtime) injectionSource() (string, error) {
	// YUI_BRIDGE_URL = websocket URL for the bridge
	bridgeURL := "ws://" + localhost(r.cfg.PlatformBridgeAddr) + "/ws"
	if r.cfg.PlatformBridgeToken != "" {
		bridgeURL += "?token=" + r.cfg.PlatformBridgeToken
	}
	// YUI_PLATFORM_HTTP = HTTP URL for the server
	httpURL := "http://" + localhost(r.cfg.PlatformHTTPAddr)
	httpToken := r.cfg.PlatformBridgeToken

	if r.cfg.PlatformDevServer != "" {
		devServer := strings.TrimRight(r.cfg.PlatformDevServer, "/")
		return fmt.Sprintf(
			"window.__YUI_BRIDGE_URL=%q;window.__YUI_PLATFORM_HTTP=%q;window.__YUI_PLATFORM_HTTP_TOKEN=%q;window.__YUI_PLATFORM_DEV_SERVER=%q;import(%q);",
			bridgeURL,
			httpURL,
			httpToken,
			devServer,
			devServer+"/src/main.ts",
		), nil
	}

	bundle, err := r.staticFS.ReadFile("static/platform.js")
	if err != nil {
		return "", fmt.Errorf("read platform bundle: %w", err)
	}

	return fmt.Sprintf(
		"window.__YUI_BRIDGE_URL=%q;window.__YUI_PLATFORM_HTTP=%q;window.__YUI_PLATFORM_HTTP_TOKEN=%q;window.__YUI_PLATFORM_DEV_SERVER=%q;\n%s",
		bridgeURL,
		httpURL,
		httpToken,
		"",
		string(bundle),
	), nil
}

func (r *Runtime) activePageTarget(ctx context.Context) (cdpTarget, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json", r.cfg.PlatformDebugPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return cdpTarget{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cdpTarget{}, fmt.Errorf("query cdp targets: %w", err)
	}
	defer resp.Body.Close()

	var targets []cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return cdpTarget{}, fmt.Errorf("decode cdp targets: %w", err)
	}

	for _, target := range targets {
		if target.Type == "page" && target.WebSocketDebuggerURL != "" {
			return target, nil
		}
	}

	return cdpTarget{}, fmt.Errorf("no page target available")
}

func cdpCall(conn *websocket.Conn, id int, method string, params map[string]any) error {
	if err := conn.WriteJSON(cdpMessage{ID: id, Method: method, Params: params}); err != nil {
		return err
	}

	for {
		var msg cdpMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		if msg.ID != id {
			continue
		}
		if msg.Error != nil {
			data, _ := json.Marshal(msg.Error)
			return fmt.Errorf("%s", string(data))
		}
		return nil
	}
}
