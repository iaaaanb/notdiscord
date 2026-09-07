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
	sendBuffer   = 64               // mensajes en cola por cliente
	writeTimeout = 5 * time.Second  // máximo para escribir un mensaje
	pingInterval = 30 * time.Second // cada cuánto sondeamos al cliente
	pongTimeout  = 10 * time.Second // cuánto esperamos el pong
)

// Client representa una conexión WebSocket registrada en el hub.
//
// Concurrencia: readPump y writePump corren en goroutines separadas.
// Los campos nick, channel, session y closeCode/closeReason solo los
// escribe el hub (goroutine única); writePump los lee recién después de
// que el hub cierra c.send, y ese cierre establece el happens-before.
type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	nick    string // "" hasta que el hub acepte un set_nick
	channel string // canal que el cliente está mirando; lo asigna el hub
	session string // token para reclamar el nick al reconectar

	closeCode   websocket.StatusCode
	closeReason string
}

// ServeWS actualiza la petición HTTP a WebSocket y ata el cliente al hub.
// Bloquea hasta que la conexión termina (es el handler HTTP).
func ServeWS(h *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}

	c := &Client{
		hub:         h,
		conn:        conn,
		send:        make(chan []byte, sendBuffer),
		closeCode:   websocket.StatusNormalClosure,
		closeReason: "adiós",
	}
	h.register <- c

	go c.writePump()
	c.readPump(r.Context())
}

// kick deja anotado con qué código cerrar. El cierre real lo dispara el
// hub al cerrar c.send; el cliente web usa el código para decidir si
// tiene sentido reintentar la conexión.
func (c *Client) kick(code websocket.StatusCode, reason string) {
	c.closeCode, c.closeReason = code, reason
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

// writePump drena el channel send hacia el socket y, entremedio, manda
// pings periódicos.
//
// El ping es lo que hace posible la reconexión: si al usuario se le cae
// el wifi o cierra el notebook, el TCP puede quedar "abierto" para
// siempre y su nick quedaría ocupado por un fantasma. Con el ping
// detectamos la conexión muerta en ~40s y la soltamos.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case data, ok := <-c.send:
			if !ok {
				// El hub cerró el canal: despedida ordenada.
				c.conn.Close(c.closeCode, c.closeReason)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			err := c.conn.Write(ctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.conn.CloseNow()
				return // readPump notará el cierre y desregistrará
			}

		case <-ticker.C:
			// Ping espera el pong; readPump lo procesa por debajo.
			ctx, cancel := context.WithTimeout(context.Background(), pongTimeout)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				c.conn.CloseNow()
				return
			}
		}
	}
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
