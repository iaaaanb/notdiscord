// Package chat implementa el hub central y las conexiones de clientes.
//
// El diseño sigue el patrón clásico de chat en Go: el hub corre en una
// sola goroutine y es el ÚNICO que toca el estado (clientes, nicks,
// canales). Todo le llega por channels, así que no hay mutexes ni
// data races.
package chat

import (
	"log"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/iaaaanb/notdiscord/internal/protocol"
	"github.com/iaaaanb/notdiscord/internal/store"
)

const (
	maxNickLen     = 32
	maxContentLen  = 2000
	defaultChannel = "general"
	historyLimit   = 50
)

// Los nombres de canal se normalizan a minúsculas con guiones,
// estilo discord: "Mi Canal" → "mi-canal".
var channelNameRe = regexp.MustCompile(`^[a-z0-9\p{L}][a-z0-9\p{L}_-]{0,31}$`)

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
	clients  map[*Client]bool
	nicks    map[string]*Client
	channels map[string]bool
	store    *store.Store
}

// NewHub crea el hub con los canales cargados desde la base.
func NewHub(st *store.Store, channelNames []string) *Hub {
	channels := map[string]bool{defaultChannel: true}
	for _, n := range channelNames {
		channels[n] = true
	}
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		inbound:    make(chan inbound),
		clients:    make(map[*Client]bool),
		nicks:      make(map[string]*Client),
		channels:   channels,
		store:      st,
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
		}), "")
	}
}

func (h *Hub) handle(c *Client, env protocol.Envelope) {
	switch env.Type {
	case "set_nick":
		var req protocol.SetNick
		if decode(c, env, &req) {
			h.setNick(c, req.Nick)
		}

	case "send_message":
		var req protocol.SendMessage
		if decode(c, env, &req) {
			h.message(c, req.Content)
		}

	case "join_channel":
		var req protocol.JoinChannel
		if decode(c, env, &req) {
			h.joinChannel(c, req.Name)
		}

	case "create_channel":
		var req protocol.CreateChannel
		if decode(c, env, &req) {
			h.createChannel(c, req.Name)
		}

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
	c.channel = defaultChannel
	h.nicks[nick] = c
	log.Printf("chat: %q entró (%d online)", nick, len(h.nicks))

	c.trySend(protocol.Marshal("nick_ok", protocol.NickOK{
		Nick:     nick,
		Online:   h.online(),
		Channels: h.channelNames(),
		Channel:  c.channel,
	}))
	h.sendHistory(c)
	h.broadcast(protocol.Marshal("user_joined", protocol.Presence{
		Nick:   nick,
		Online: h.online(),
	}), "")
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
	msg := protocol.NewMessage(c.channel, c.nick, content)
	if err := h.store.SaveMessage(msg); err != nil {
		// El chat sigue funcionando aunque falle el disco; solo se
		// pierde ese mensaje del historial.
		log.Printf("store: guardando mensaje: %v", err)
	}
	h.broadcast(protocol.Marshal("message", msg), c.channel)
}

func (h *Hub) joinChannel(c *Client, name string) {
	if c.nick == "" {
		c.sendError("no_nick", "elige un nick antes de cambiar de canal")
		return
	}
	name = NormalizeChannel(name)
	if !h.channels[name] {
		c.sendError("no_channel", "el canal #"+name+" no existe")
		return
	}
	c.channel = name
	c.trySend(protocol.Marshal("channel_joined", protocol.ChannelJoined{Name: name}))
	h.sendHistory(c)
}

func (h *Hub) createChannel(c *Client, name string) {
	if c.nick == "" {
		c.sendError("no_nick", "elige un nick antes de crear canales")
		return
	}
	name = NormalizeChannel(name)
	if !channelNameRe.MatchString(name) {
		c.sendError("bad_channel", "nombre inválido: usa letras, números, - o _ (máx. 32)")
		return
	}
	if h.channels[name] {
		c.sendError("channel_exists", "el canal #"+name+" ya existe")
		return
	}

	if err := h.store.CreateChannel(name); err != nil {
		log.Printf("store: creando canal: %v", err)
		c.sendError("store_error", "no pude guardar el canal, intenta de nuevo")
		return
	}
	h.channels[name] = true
	log.Printf("chat: %q creó el canal #%s", c.nick, name)

	// Todos ven el canal nuevo; el creador además se cambia a él.
	h.broadcast(protocol.Marshal("channel_list", protocol.ChannelList{
		Channels: h.channelNames(),
	}), "")
	c.channel = name
	c.trySend(protocol.Marshal("channel_joined", protocol.ChannelJoined{Name: name}))
	h.sendHistory(c)
}

// sendHistory manda al cliente los últimos mensajes de su canal actual.
func (h *Hub) sendHistory(c *Client) {
	msgs, err := h.store.History(c.channel, historyLimit)
	if err != nil {
		log.Printf("store: leyendo historial de #%s: %v", c.channel, err)
		return
	}
	if msgs == nil {
		msgs = []protocol.Message{} // JSON: [] en vez de null
	}
	c.trySend(protocol.Marshal("history", protocol.History{
		Channel:  c.channel,
		Messages: msgs,
	}))
}

// broadcast envía data a los clientes con nick. Si channel != "", solo a
// quienes están mirando ese canal. Si el buffer de un cliente está lleno
// (consumidor lento), se le desconecta en vez de bloquear al hub entero.
func (h *Hub) broadcast(data []byte, channel string) {
	for c := range h.clients {
		if c.nick == "" {
			continue // aún no entra al chat
		}
		if channel != "" && c.channel != channel {
			continue
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

// channelNames devuelve los canales ordenados, con "general" primero.
func (h *Hub) channelNames() []string {
	out := make([]string, 0, len(h.channels))
	for n := range h.channels {
		if n != defaultChannel {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return append([]string{defaultChannel}, out...)
}

// NormalizeChannel convierte un nombre libre al formato de canal:
// minúsculas y espacios como guiones. "Mi Canal" → "mi-canal".
func NormalizeChannel(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "#")
	return strings.ReplaceAll(name, " ", "-")
}
