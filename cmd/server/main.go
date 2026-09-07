// Command server arranca el notdiscord.
//
// M4: chat con canales, persistencia en SQLite, keepalive por ping y
// reconexión automática del cliente. Cierra ordenado con Ctrl-C.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
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

	// Ctrl-C: dejamos de aceptar conexiones y le damos unos segundos a
	// las que quedan para cerrarse antes de soltar la base.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("apagando…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	// El flag viene como ":8080" o como "127.0.0.1:8080"; en el primer
	// caso le agregamos el host para que el link quede clickeable.
	shown := *addr
	if strings.HasPrefix(shown, ":") {
		shown = "localhost" + shown
	}
	log.Printf("escuchando en http://%s", shown)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
