// Command server arranca el notdiscord.
//
// M0: sirve el cliente web (embebido en el binario) y expone /ws,
// que por ahora solo hace echo de lo que recibe. El hub llega en M1.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/iaaaanb/notdiscord/web"
)

func main() {
	addr := flag.String("addr", ":8080", "dirección de escucha, ej. :8080")
	flag.Parse()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(web.FS))
	mux.HandleFunc("/ws", handleWS)

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

// handleWS acepta la conexión y hace echo de cada mensaje de texto.
// En M1 esto se reemplaza por el registro del cliente en el hub.
func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	defer conn.CloseNow()

	log.Printf("conexión ws desde %s", r.RemoteAddr)
	ctx := r.Context()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			// Cierre normal o error de red: terminamos la conexión.
			log.Printf("ws read (%s): %v", r.RemoteAddr, err)
			return
		}
		if err := conn.Write(ctx, typ, data); err != nil {
			log.Printf("ws write (%s): %v", r.RemoteAddr, err)
			return
		}
	}
}
