package chat

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/iaaaanb/notdiscord/internal/protocol"
)

const (
	sendBuffer   = 64              // mensajes en cola por cliente
	writeTimeout = 5 * time.Second // máximo para escribir un mensaje
)

// Client representa una conexión WebSocket registrada en el hub.
//
// Concurrencia: readPump y writePump corren en goroutines separadas.
// El campo nick solo lo escribe el hub (goroutine única), y send es el
// puente hub → writePump.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	nick string // "" hasta que el hub acepte un set_nick
}

// ServeWS actualiza la petición HTTP a WebSocket y ata el cliente al hub.
// Bloquea hasta que la conexión termina (es el handler HTTP).
func ServeWS(h *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}

	c := &Client{hub: h, conn: conn, send: make(chan []byte, sendBuffer)}
	h.register <- c

	go c.writePump()
	c.readPump(r.Context())
}

// readPump lee del socket, parsea el envelope y se lo pasa al hub.
// Al salir (cierre o error) desregistra al cliente.
func (c *Client) readPump(ctx context.Context) {
	defer func() {
		c.hub.unregister <- c
		c.conn.CloseNow()
	}()

	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return // cierre normal o error de red
		}
		if typ != websocket.MessageText {
			continue
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			c.sendError("bad_json", "no pude parsear el mensaje")
			continue
		}
		c.hub.inbound <- inbound{client: c, env: env}
	}
}

// writePump drena el channel send hacia el socket. Cuando el hub cierra
// send, despide la conexión con un close frame normal.
func (c *Client) writePump() {
	for data := range c.send {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		err := c.conn.Write(ctx, websocket.MessageText, data)
		cancel()
		if err != nil {
			c.conn.CloseNow()
			return // readPump notará el cierre y desregistrará
		}
	}
	c.conn.Close(websocket.StatusNormalClosure, "adiós")
}

// trySend encola data sin bloquear; si el buffer está lleno se descarta
// (el hub ya maneja la desconexión de clientes lentos en broadcast).
func (c *Client) trySend(data []byte) {
	select {
	case c.send <- data:
	default:
	}
}

// sendError manda un mensaje de error sin cortar la conexión.
func (c *Client) sendError(code, msg string) {
	c.trySend(protocol.Marshal("error", protocol.ErrorMsg{Code: code, Message: msg}))
}

// decode parsea env.Data en dst; si falla, avisa al cliente y retorna false.
func decode(c *Client, env protocol.Envelope, dst any) bool {
	if err := json.Unmarshal(env.Data, dst); err != nil {
		c.sendError("bad_data", "datos inválidos para "+env.Type)
		return false
	}
	return true
}
