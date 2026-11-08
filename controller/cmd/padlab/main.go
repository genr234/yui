// Command padlab runs a kiosk play session against a local Rails server
// without Chrome or Windows, and prints every bridge event the overlay would
// receive. Use it with the pad simulator at /sim to develop Yui Pad end to end.
//
//	go run ./cmd/padlab -server http://localhost:3000 -token <kiosk device token>
//
// Then open the printed /sim URL in a browser. CDP calls fail (no Chrome) and
// are logged; DSU virtual pads listen on 127.0.0.1:26760/26761 as usual.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"kiosk/controller/handlers"
	"kiosk/controller/internal/config"
)

func main() {
	server := flag.String("server", "http://localhost:3000", "Yui server URL")
	token := flag.String("token", "", "kiosk device token")
	quiet := flag.String("quiet", "pad.cursor", "comma-separated events not to print")
	flag.Parse()
	if *token == "" {
		log.Fatal("padlab: -token is required")
	}

	dir, err := os.MkdirTemp("", "padlab-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cfg := config.Config{
		ConfigPath:      filepath.Join(dir, "controller.json"),
		ConfigDir:       dir,
		StorePath:       filepath.Join(dir, "store.db"),
		ServerURL:       *server,
		ActiveAccountID: "padlab",
		Accounts: []config.AccountConfig{{
			ID: "padlab", Name: "Padlab", KioskID: "padlab", DeviceToken: *token,
		}},
	}
	if err := config.Save(cfg); err != nil {
		log.Fatal(err)
	}
	os.Setenv("YUI_KIOSK_CONFIG", cfg.ConfigPath)

	skip := map[string]bool{}
	for _, name := range strings.Split(*quiet, ",") {
		skip[strings.TrimSpace(name)] = true
	}
	registry := handlers.NewRegistry(cfg)
	registry.SetEmitter(func(event string, data any) {
		if skip[event] {
			return
		}
		body, _ := json.Marshal(data)
		fmt.Printf("event %s %s\n", event, body)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registry.BindPadLifecycle(ctx)

	started, err := registry.Dispatch("pad.session.start", nil)
	if err != nil {
		log.Fatal(err)
	}
	body, _ := json.Marshal(started)
	fmt.Printf("started %s\n", body)
	var snap struct {
		Session *struct {
			Code string `json:"code"`
		} `json:"session"`
	}
	_ = json.Unmarshal(body, &snap)
	// Open the lobby so simulated pads can join by code without the
	// rotating QR token.
	if _, err := registry.Dispatch("pad.lobby", json.RawMessage(`{"open":true}`)); err != nil {
		log.Printf("padlab: open lobby: %v", err)
	}
	if snap.Session != nil {
		fmt.Printf("simulator: %s/sim?code=%s\n", strings.TrimRight(*server, "/"), snap.Session.Code)
	}

	<-ctx.Done()
	registry.WaitPadStopped(6e9)
}
