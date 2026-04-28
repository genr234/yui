package platform

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kiosk/controller/internal/config"
)

type Runtime struct {
	cfg      config.Config
	staticFS embed.FS
}

type rpcRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     string `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func Start(ctx context.Context, cfg config.Config, staticFS embed.FS) {
	if !cfg.PlatformEnabled {
		return
	}

	r := &Runtime{
		cfg:      cfg,
		staticFS: staticFS,
	}

	go r.serveHTTP(ctx)
	go r.injectLoop(ctx)
}

func (r *Runtime) serveHTTP(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("/platform.js", func(w http.ResponseWriter, req *http.Request) {
		data, err := r.staticFS.ReadFile("static/platform.js")
		if err != nil {
			http.Error(w, "platform bundle not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/status.json", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, r.cfg.StatusPath)
	})
	mux.HandleFunc("/diagnostics.txt", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, diagnosticsPath(r.cfg.StatusPath))
	})

	server := &http.Server{Addr: r.cfg.PlatformHTTPAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("platform http listening on %s", r.cfg.PlatformHTTPAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("platform http stopped: %v", err)
	}
}

func readJSONFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func readTextFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{"text": ""}, err
	}
	return map[string]string{"text": string(data)}, nil
}

func diagnosticsPath(statusPath string) string {
	return filepath.Join(filepath.Dir(statusPath), "diagnostics.txt")
}

func staticHasPlatformBundle(staticFS embed.FS) bool {
	_, err := fs.Stat(staticFS, "static/platform.js")
	return err == nil
}

func localhost(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return addr
}
