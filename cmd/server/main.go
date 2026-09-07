// Command server arranca el notdiscord.
//
// M3: chat con canales y persistencia en SQLite. Los canales y el
// historial de mensajes sobreviven reinicios del servidor.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/iaaaanb/notdiscord/internal/chat"
	"github.com/iaaaanb/notdiscord/internal/store"
	"github.com/iaaaanb/notdiscord/web"
)

func main() {
	addr := flag.String("addr", ":8080", "dirección de escucha, ej. :8080")
	dbPath := flag.String("db", "notdiscord.db", "ruta del archivo SQLite")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	names, err := st.ChannelNames()
	if err != nil {
		log.Fatalf("store: cargando canales: %v", err)
	}
	log.Printf("store: %s (%d canales)", *dbPath, len(names))

	hub := chat.NewHub(st, names)
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
