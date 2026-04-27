package bridge

import (
	"encoding/json"
	"gorilla/websocket"
	"log"
	"net/http"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Message struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Response struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

func handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	defer conn.Close()

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Printf("read error: %v", err)
			break
		}

		var msg Message
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			log.Printf("unmarshal error: %v", err)
			continue
		}

		log.Printf("received message: %+v", msg)

		resp := Response{
			ID:   msg.ID,
			Type: "response",
			Payload: map[string]any{
				"echo": msg.Payload,
			},
		}

		respBytes, err := json.Marshal(resp)
		if err != nil {
			log.Printf("marshal error: %v", err)
			continue
		}

		if err := conn.WriteMessage(websocket.TextMessage, respBytes); err != nil {
			log.Printf("write error: %v", err)
			break
		}
	}
}

func Start(addr string) error {
	http.HandleFunc("/", handleConnection)
	log.Printf("bridge listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
	return nil
}
