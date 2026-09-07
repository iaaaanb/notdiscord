// Package chat implementa el hub central y las conexiones de clientes.
//
// El diseño sigue el patrón clásico de chat en Go: el hub corre en una
// sola goroutine y es el ÚNICO que toca el estado (clientes, nicks).
// Todo le llega por channels, así que no hay mutexes ni data races.
package chat

import (
	"log"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/iaaaanb/notdiscord/internal/protocol"
)

const (
	maxNickLen    = 32
	maxContentLen = 2000
)

// inbound es un mensaje parseado que un cliente le manda al hub.
type inbound struct {
	client *Client
	env    protocol.Envelope
}

// Hub mantiene el conjunto de clientes conectados y difunde mensajes.
type Hub struct {
	register   chan *Client
	unregister chan *Client
	inbound    chan inbound

	// Estado privado del hub: solo Run() lo toca.
	clients map[*Client]bool
	nicks   map[string]*Client
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		inbound:    make(chan inbound),
		clients:    make(map[*Client]bool),
		nicks:      make(map[string]*Client),
	}
}

// Run procesa eventos para siempre. Debe correr en su propia goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true

		case c := <-h.unregister:
			h.drop(c, true)

		case in := <-h.inbound:
			h.handle(in.client, in.env)
		}
	}
}

// drop saca al cliente del hub y anuncia su salida si tenía nick.
func (h *Hub) drop(c *Client, announce bool) {
	if !h.clients[c] {
		return // ya fue eliminado (p. ej. por ser un cliente lento)
	}
	delete(h.clients, c)
	close(c.send) // writePump termina y cierra la conexión
	if c.nick == "" {
		return
	}
	delete(h.nicks, c.nick)
	log.Printf("chat: %q salió (%d online)", c.nick, len(h.nicks))
	if announce {
		h.broadcast(protocol.Marshal("user_left", protocol.Presence{
			Nick:   c.nick,
			Online: h.online(),
		}))
	}
}

func (h *Hub) handle(c *Client, env protocol.Envelope) {
	switch env.Type {
	case "set_nick":
		var req protocol.SetNick
		if !decode(c, env, &req) {
			return
		}
		h.setNick(c, req.Nick)

	case "send_message":
		var req protocol.SendMessage
		if !decode(c, env, &req) {
			return
		}
		h.message(c, req.Content)

	default:
		c.sendError("unknown_type", "tipo de mensaje desconocido: "+env.Type)
	}
}

func (h *Hub) setNick(c *Client, nick string) {
	nick = strings.TrimSpace(nick)
	switch {
	case c.nick != "":
		c.sendError("already_set", "ya tienes un nick en esta conexión")
		return
	case nick == "" || utf8.RuneCountInString(nick) > maxNickLen:
		c.sendError("bad_nick", "el nick debe tener entre 1 y 32 caracteres")
		return
	case h.nicks[nick] != nil:
		c.sendError("nick_taken", "ese nick ya está en uso")
		return
	}

	c.nick = nick
	h.nicks[nick] = c
	log.Printf("chat: %q entró (%d online)", nick, len(h.nicks))

	c.trySend(protocol.Marshal("nick_ok", protocol.NickOK{
		Nick:   nick,
		Online: h.online(),
	}))
	h.broadcast(protocol.Marshal("user_joined", protocol.Presence{
		Nick:   nick,
		Online: h.online(),
	}))
}

func (h *Hub) message(c *Client, content string) {
	if c.nick == "" {
		c.sendError("no_nick", "elige un nick antes de escribir")
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	if utf8.RuneCountInString(content) > maxContentLen {
		c.sendError("too_long", "mensaje demasiado largo (máx. 2000 caracteres)")
		return
	}
	h.broadcast(protocol.Marshal("message", protocol.NewMessage(c.nick, content)))
}

// broadcast envía data a todos los clientes con nick. Si el buffer de un
// cliente está lleno (consumidor lento), se le desconecta en vez de
// bloquear al hub entero.
func (h *Hub) broadcast(data []byte) {
	for c := range h.clients {
		if c.nick == "" {
			continue // aún no entra al chat
		}
		select {
		case c.send <- data:
		default:
			log.Printf("chat: %q no da abasto, desconectando", c.nick)
			h.drop(c, true)
		}
	}
}

// online devuelve los nicks conectados, ordenados para una UI estable.
func (h *Hub) online() []string {
	out := make([]string, 0, len(h.nicks))
	for n := range h.nicks {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
