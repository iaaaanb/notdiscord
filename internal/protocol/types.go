// Package protocol define los mensajes JSON que viajan por el WebSocket.
//
// Todo mensaje es un Envelope: {"type": "...", "data": {...}}.
//
// Cliente → servidor: set_nick, send_message, join_channel, create_channel
// Servidor → cliente: nick_ok, message, history, user_joined, user_left,
//
//	channel_list, channel_joined, error
package protocol

import (
	"encoding/json"
	"time"
)

// Envelope envuelve cualquier mensaje del protocolo.
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Marshal arma un Envelope listo para enviar por el socket.
// data no debería poder fallar al serializarse (son structs nuestros),
// por eso el error se trata como bug de programación.
func Marshal(typ string, data any) []byte {
	raw, err := json.Marshal(data)
	if err != nil {
		panic("protocol: marshal de " + typ + ": " + err.Error())
	}
	out, err := json.Marshal(Envelope{Type: typ, Data: raw})
	if err != nil {
		panic("protocol: marshal envelope: " + err.Error())
	}
	return out
}

// --- cliente → servidor ---

// SetNick pide registrar un apodo para esta conexión.
//
// M4: Session y Channel son opcionales y sirven para reconectar. Si el
// nick está ocupado pero Session coincide con el de la conexión que lo
// tiene, el servidor asume que es el mismo cliente volviendo tras una
// caída de red y le transfiere el nick (echando a la conexión vieja).
// Channel permite volver al canal donde estabas sin un round-trip extra.
type SetNick struct {
	Nick    string `json:"nick"`
	Session string `json:"session,omitempty"`
	Channel string `json:"channel,omitempty"`
}

// SendMessage envía un mensaje al canal en el que está el cliente.
type SendMessage struct {
	Content string `json:"content"`
}

// JoinChannel pide cambiarse a un canal existente.
type JoinChannel struct {
	Name string `json:"name"`
}

// CreateChannel pide crear un canal nuevo (y te cambia a él).
type CreateChannel struct {
	Name string `json:"name"`
}

// --- servidor → cliente ---

// NickOK confirma el registro del nick. Incluye el estado inicial:
// quiénes están online, qué canales existen y en cuál quedaste.
// Session es el token que el cliente debe guardar para reconectar.
type NickOK struct {
	Nick     string   `json:"nick"`
	Session  string   `json:"session"`
	Online   []string `json:"online"`
	Channels []string `json:"channels"`
	Channel  string   `json:"channel"`
}

// Message es un mensaje de chat difundido al canal correspondiente.
type Message struct {
	Channel string    `json:"channel"`
	Author  string    `json:"author"`
	Content string    `json:"content"`
	SentAt  time.Time `json:"sent_at"`
}

// NewMessage crea un Message con la hora actual del servidor.
func NewMessage(channel, author, content string) Message {
	return Message{Channel: channel, Author: author, Content: content, SentAt: time.Now().UTC()}
}

// Presence anuncia que alguien entró o salió (user_joined / user_left)
// e incluye la lista online actualizada para que el cliente la reemplace.
type Presence struct {
	Nick   string   `json:"nick"`
	Online []string `json:"online"`
}

// ChannelList es la lista completa de canales (se reenvía al crear uno).
type ChannelList struct {
	Channels []string `json:"channels"`
}

// ChannelJoined confirma que el cliente quedó mirando otro canal.
type ChannelJoined struct {
	Name string `json:"name"`
}

// History trae los últimos mensajes de un canal (del más antiguo al
// más nuevo). Se envía justo después de nick_ok y de channel_joined.
type History struct {
	Channel  string    `json:"channel"`
	Messages []Message `json:"messages"`
}

// ErrorMsg informa un error al cliente sin cortar la conexión.
type ErrorMsg struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
