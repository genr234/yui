package bridge

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"kiosk/controller/handlers"
	"kiosk/controller/internal/config"
)

type Runtime struct {
	cfg      config.Config
	commands *handlers.Registry
}

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

func Start(ctx context.Context, cfg config.Config) {
	if !cfg.PlatformEnabled {
		return
	}

	r := New(cfg)
	r.commands.StartPlugins(ctx)
	go r.serve(ctx)
}

func New(cfg config.Config) *Runtime {
	r := &Runtime{
		cfg:      cfg,
		commands: handlers.NewRegistry(cfg),
	}
	return r
}

func (r *Runtime) serve(ctx context.Context) {
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{CheckOrigin: func(req *http.Request) bool { return true }}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			log.Printf("bridge upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		r.handle(conn)
	})

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

func (r *Runtime) handle(conn *websocket.Conn) {
	for {
		var req rpcRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}

		result, err := r.dispatch(req.Method, req.Params)
		resp := rpcResponse{ID: req.ID, Result: result}
		if err != nil {
			resp.Error = err.Error()
			resp.Result = nil
		}
		if err := conn.WriteJSON(resp); err != nil {
			return
		}
	}
}

func (r *Runtime) dispatch(method string, params json.RawMessage) (any, error) {
	return r.commands.DispatchAuthenticated(method, params)
}
