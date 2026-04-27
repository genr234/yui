package main

import (
	"log"
	"net/http"
	"os"

	"kiosk/controller/bridge"
	"kiosk/controller/proxy"
)

func main() {
	go serveProxy()
	go serveBridge()
	serveStatic()
}

func serveProxy() {
	log.Println("proxy boilerplate listening on :7070")
	if err := http.ListenAndServe(":7070", proxy.New()); err != nil {
		log.Fatal(err)
	}
}

func serveBridge() {
	log.Println("bridge boilerplate listening on :7071")
	if err := http.ListenAndServe(":7071", bridge.New()); err != nil {
		log.Fatal(err)
	}
}

func serveStatic() {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(staticDir())))

	log.Println("static boilerplate listening on :7072")
	if err := http.ListenAndServe(":7072", mux); err != nil {
		log.Fatal(err)
	}
}

func staticDir() string {
	if dir := os.Getenv("CONTROLLER_STATIC_DIR"); dir != "" {
		return dir
	}
	return "static"
}
