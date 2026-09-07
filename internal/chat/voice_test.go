package chat_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iaaaanb/notdiscord/internal/protocol"
)

// join conecta un cliente y le registra un nick.
func join(t *testing.T, url, nick string) *websocket.Conn {
	t.Helper()
	conn := dial(t, url)
	write(t, conn, "set_nick", protocol.SetNick{Nick: nick})
	expect(t, conn, "nick_ok", nil)
	return conn
}

// waitVoice lee hasta que el canal quede exactamente con want.
//
// No sirve mirar solo el primer voice_state que llega: cada entrada a
// voz difunde uno a todo el mundo, así que un cliente ve la secuencia
// completa de estados intermedios. Lo que importa es el estado final.
func waitVoice(t *testing.T, conn *websocket.Conn, channel string, want []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var last []string
	for i := 0; i < 60; i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("esperando voz %v: %v", want, err)
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil || env.Type != "voice_state" {
			continue
		}
		var vs protocol.VoiceState
		if err := json.Unmarshal(env.Data, &vs); err != nil || vs.Channel != channel {
			continue
		}
		last = vs.Members
		if slices.Equal(vs.Members, want) {
			return
		}
	}
	t.Fatalf("#%s quedó en %v, quería %v", channel, last, want)
}

// TestVoiceState sigue el ciclo completo: entrar, verse mutuamente, y
// desaparecer de la lista al cortar la conexión.
func TestVoiceState(t *testing.T) {
	url := newServer(t)
	ana := join(t, url, "ana")
	beto := join(t, url, "beto")

	write(t, ana, "voice_join", nil)
	waitVoice(t, ana, "general", []string{"ana"})

	write(t, beto, "voice_join", nil)
	waitVoice(t, beto, "general", []string{"ana", "beto"})
	// ana también se entera, que es lo que dispara la negociación.
	waitVoice(t, ana, "general", []string{"ana", "beto"})

	// Se cae ana: beto debe quedar solo sin hacer nada.
	ana.CloseNow()
	waitVoice(t, beto, "general", []string{"beto"})
}

// TestSignalRouting verifica que el servidor reenvía la negociación tal
// cual, poniendo quién la mandó.
func TestSignalRouting(t *testing.T) {
	url := newServer(t)
	ana := join(t, url, "ana")
	beto := join(t, url, "beto")

	write(t, ana, "voice_join", nil)
	write(t, beto, "voice_join", nil)
	waitVoice(t, ana, "general", []string{"ana", "beto"})
	waitVoice(t, beto, "general", []string{"ana", "beto"})

	oferta := json.RawMessage(`{"desc":{"type":"offer","sdp":"v=0..."}}`)
	write(t, ana, "signal", protocol.Signal{To: "beto", Payload: oferta})

	var got protocol.Signal
	expect(t, beto, "signal", &got)
	if got.From != "ana" {
		t.Errorf("from = %q, quería ana", got.From)
	}
	if string(got.Payload) != string(oferta) {
		t.Errorf("el payload llegó alterado: %s", got.Payload)
	}
}

// TestSignalFueraDeVoz: el servidor no debe prestarse para mandarle
// paquetes a alguien que no está en la conversación.
func TestSignalFueraDeVoz(t *testing.T) {
	url := newServer(t)
	ana := join(t, url, "ana")
	join(t, url, "beto")

	write(t, ana, "voice_join", nil)
	waitVoice(t, ana, "general", []string{"ana"})

	write(t, ana, "signal", protocol.Signal{To: "beto", Payload: json.RawMessage(`{}`)})

	var e protocol.ErrorMsg
	expect(t, ana, "error", &e)
	if e.Code != "no_peer" {
		t.Errorf("código = %q, quería no_peer", e.Code)
	}
}

// TestVoiceIndependienteDelTexto: cambiar de canal de texto no te saca
// de la voz, igual que en discord.
func TestVoiceIndependienteDelTexto(t *testing.T) {
	url := newServer(t)
	ana := join(t, url, "ana")
	beto := join(t, url, "beto")

	write(t, ana, "create_channel", protocol.CreateChannel{Name: "tareas"})
	expect(t, ana, "channel_joined", nil)

	write(t, beto, "voice_join", nil) // beto queda en la voz de general
	waitVoice(t, beto, "general", []string{"beto"})

	// ana se mueve a #tareas y de vuelta; beto sigue en voz.
	write(t, ana, "join_channel", protocol.JoinChannel{Name: "general"})
	expect(t, ana, "channel_joined", nil)

	write(t, beto, "join_channel", protocol.JoinChannel{Name: "tareas"})
	expect(t, beto, "channel_joined", nil)

	// beto sigue en la voz de #general aunque ahora está leyendo #tareas.
	write(t, ana, "voice_join", nil)
	waitVoice(t, ana, "general", []string{"ana", "beto"})
}

// TestVoiceLleno protege el límite de la malla.
func TestVoiceLleno(t *testing.T) {
	url := newServer(t)

	nicks := []string{"c1", "c2", "c3", "c4", "c5", "c6"}
	for i, n := range nicks {
		conn := join(t, url, n)
		write(t, conn, "voice_join", nil)
		waitVoice(t, conn, "general", nicks[:i+1])
	}

	tarde := join(t, url, "tarde")
	write(t, tarde, "voice_join", nil)

	var e protocol.ErrorMsg
	expect(t, tarde, "error", &e)
	if e.Code != "voice_full" {
		t.Errorf("código = %q, quería voice_full", e.Code)
	}
}
