package bridge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"kiosk/controller/handlers"
	"kiosk/controller/internal/config"
)

type Runtime struct {
	cfg      config.Config
	commands *handlers.Registry

	connMu sync.Mutex
	conns  map[*bridgeConn]struct{}
}

// bridgeConn is one connected overlay socket. writeMu serialises every write
// to the socket (RPC responses and broadcast events share it).
type bridgeConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (c *bridgeConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

type eventFrame struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// broadcastWriteTimeout bounds how long one slow socket can hold up a broadcast.
const broadcastWriteTimeout = 5 * time.Second

type rpcRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     string `json:"id"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Start runs the bridge until ctx is cancelled. The returned func blocks
// after cancellation until a running play session has told Rails it ended,
// for at most padStopBudget.
func Start(ctx context.Context, cfg config.Config) (waitStopped func()) {
	if !cfg.PlatformEnabled {
		return func() {}
	}

	r := New(cfg)
	r.commands.StartPlugins(ctx)
	r.commands.StartAccountSync(ctx)
	r.commands.BindPadLifecycle(ctx)
	go r.serve(ctx)
	return func() { r.commands.WaitPadStopped(padStopBudget) }
}

// padStopBudget covers the session's goodbye signals and its DELETE.
const padStopBudget = 6 * time.Second

func New(cfg config.Config) *Runtime {
	r := &Runtime{
		cfg:      cfg,
		commands: handlers.NewRegistry(cfg),
		conns:    make(map[*bridgeConn]struct{}),
	}
	r.commands.SetEmitter(r.Broadcast)
	return r
}

// Broadcast sends an event frame {"event": name, "data": data} to every
// connected bridge socket. A socket that fails to accept the write is closed;
// its read loop then removes it.
func (r *Runtime) Broadcast(event string, data any) {
	r.connMu.Lock()
	conns := make([]*bridgeConn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.connMu.Unlock()

	frame := eventFrame{Event: event, Data: data}
	for _, c := range conns {
		c.writeMu.Lock()
		_ = c.conn.SetWriteDeadline(time.Now().Add(broadcastWriteTimeout))
		err := c.conn.WriteJSON(frame)
		_ = c.conn.SetWriteDeadline(time.Time{})
		c.writeMu.Unlock()
		if err != nil {
			log.Printf("bridge broadcast %s failed: %v", event, err)
			_ = c.conn.Close()
		}
	}
}

func (r *Runtime) addConn(c *bridgeConn) {
	r.connMu.Lock()
	r.conns[c] = struct{}{}
	r.connMu.Unlock()
}

func (r *Runtime) removeConn(c *bridgeConn) {
	r.connMu.Lock()
	delete(r.conns, c)
	r.connMu.Unlock()
}

func (r *Runtime) connCount() int {
	r.connMu.Lock()
	defer r.connMu.Unlock()
	return len(r.conns)
}

func (r *Runtime) wsHandler() http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: func(req *http.Request) bool { return true }}
	return func(w http.ResponseWriter, req *http.Request) {
		if !r.authorizeUpgrade(req) {
			http.Error(w, "bridge token required", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			log.Printf("bridge upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		r.handle(conn)
	}
}

func (r *Runtime) serve(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", r.wsHandler())

	server := &http.Server{Addr: r.cfg.PlatformBridgeAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("platform bridge listening on %s", r.cfg.PlatformBridgeAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("platform bridge stopped: %v", err)
	}
	if err := r.commands.Close(); err != nil {
		log.Printf("platform bridge store close failed: %v", err)
	}
}

func (r *Runtime) authorizeUpgrade(req *http.Request) bool {
	if r.cfg.PlatformBridgeToken == "" {
		return true
	}
	token := req.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(r.cfg.PlatformBridgeToken)) == 1 {
		return true
	}
	return r.authorizeDevServerOrigin(req)
}

func (r *Runtime) authorizeDevServerOrigin(req *http.Request) bool {
	if r.cfg.PlatformDevServer == "" {
		return false
	}
	origin := req.Header.Get("Origin")
	if origin == "" {
		return false
	}
	devURL, err := url.Parse(r.cfg.PlatformDevServer)
	if err != nil || devURL.Scheme == "" || devURL.Host == "" {
		return false
	}
	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return originURL.Scheme == devURL.Scheme && originURL.Host == devURL.Host
}

func (r *Runtime) handle(conn *websocket.Conn) {
	c := &bridgeConn{conn: conn}
	r.addConn(c)
	defer r.removeConn(c)

	dispatchSlots := make(chan struct{}, 16)
	for {
		var req rpcRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}

		go func(req rpcRequest) {
			dispatchSlots <- struct{}{}
			defer func() { <-dispatchSlots }()
			result, err := r.dispatch(req.Method, req.Params)
			resp := rpcResponse{ID: req.ID, Result: result}
			if err != nil {
				resp.Error = err.Error()
				resp.Result = nil
			}
			_ = c.writeJSON(resp)
		}(req)
	}
}

func (r *Runtime) dispatch(method string, params json.RawMessage) (any, error) {
	return r.commands.DispatchAuthenticated(method, params)
}
