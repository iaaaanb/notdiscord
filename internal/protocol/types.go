// Package protocol define los mensajes JSON que viajan por el WebSocket.
//
// Todo mensaje es un Envelope: {"type": "...", "data": {...}}.
//
// Cliente → servidor: set_nick, send_message
// Servidor → cliente: nick_ok, message, user_joined, user_left, error
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
type SetNick struct {
	Nick string `json:"nick"`
}

// SendMessage envía un mensaje al canal (global en M1).
type SendMessage struct {
	Content string `json:"content"`
}

// --- servidor → cliente ---

// NickOK confirma el registro del nick e incluye quiénes están online.
type NickOK struct {
	Nick   string   `json:"nick"`
	Online []string `json:"online"`
}

// Message es un mensaje de chat difundido a todos.
type Message struct {
	Author  string    `json:"author"`
	Content string    `json:"content"`
	SentAt  time.Time `json:"sent_at"`
}

// NewMessage crea un Message con la hora actual del servidor.
func NewMessage(author, content string) Message {
	return Message{Author: author, Content: content, SentAt: time.Now().UTC()}
}

// Presence anuncia que alguien entró o salió (user_joined / user_left)
// e incluye la lista online actualizada para que el cliente la reemplace.
type Presence struct {
	Nick   string   `json:"nick"`
	Online []string `json:"online"`
}

// ErrorMsg informa un error al cliente sin cortar la conexión.
type ErrorMsg struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
