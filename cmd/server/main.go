// Command server arranca el notdiscord.
//
// M1: chat global en tiempo real. El hub central mantiene el estado y
// cada conexión WebSocket habla el protocolo JSON de internal/protocol.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/iaaaanb/notdiscord/internal/chat"
	"github.com/iaaaanb/notdiscord/web"
)

func main() {
	addr := flag.String("addr", ":8080", "dirección de escucha, ej. :8080")
	flag.Parse()

	hub := chat.NewHub()
	go hub.Run()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(web.FS))
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		chat.ServeWS(hub, w, r)
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("escuchando en http://localhost%s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
