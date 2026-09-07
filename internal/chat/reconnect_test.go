package chat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/iaaaanb/notdiscord/internal/chat"
	"github.com/iaaaanb/notdiscord/internal/protocol"
	"github.com/iaaaanb/notdiscord/internal/store"
)

// newServer levanta un hub con una base temporal y devuelve la URL ws://.
func newServer(t *testing.T) string {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	names, err := st.ChannelNames()
	if err != nil {
		t.Fatalf("ChannelNames: %v", err)
	}
	hub := chat.NewHub(st, names)
	go hub.Run()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chat.ServeWS(hub, w, r)
	}))
	t.Cleanup(srv.Close)

	return strings.Replace(srv.URL, "http://", "ws://", 1)
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func write(t *testing.T, conn *websocket.Conn, typ string, data any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.Write(ctx, websocket.MessageText, protocol.Marshal(typ, data)); err != nil {
		t.Fatalf("write %s: %v", typ, err)
	}
}

// expect lee hasta encontrar un mensaje del tipo pedido y deserializa su
// payload en dst.
func expect(t *testing.T, conn *websocket.Conn, typ string, dst any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 10; i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("esperando %s: %v", typ, err)
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("json inválido: %v", err)
		}
		if env.Type != typ {
			continue
		}
		if dst != nil {
			if err := json.Unmarshal(env.Data, dst); err != nil {
				t.Fatalf("payload de %s: %v", typ, err)
			}
		}
		return
	}
	t.Fatalf("nunca llegó un mensaje %q", typ)
}

// TestReclaimNick simula una reconexión: el cliente vuelve con el mismo
// token de sesión y recupera su nick, su canal y su historial, mientras
// la conexión zombi queda cerrada con código de política.
func TestReclaimNick(t *testing.T) {
	url := newServer(t)

	first := dial(t, url)
	write(t, first, "set_nick", protocol.SetNick{Nick: "ana"})

	var ok protocol.NickOK
	expect(t, first, "nick_ok", &ok)
	if ok.Session == "" {
		t.Fatal("nick_ok sin token de sesión")
	}
	if ok.Channel != "general" {
		t.Fatalf("canal inicial = %q, quería general", ok.Channel)
	}

	write(t, first, "create_channel", protocol.CreateChannel{Name: "Ramo Redes"})
	expect(t, first, "channel_joined", nil)
	write(t, first, "send_message", protocol.SendMessage{Content: "hola desde redes"})
	expect(t, first, "message", nil)

	// La red se cae y el cliente reconecta con su token y su canal.
	second := dial(t, url)
	write(t, second, "set_nick", protocol.SetNick{
		Nick:    "ana",
		Session: ok.Session,
		Channel: "ramo-redes",
	})

	var again protocol.NickOK
	expect(t, second, "nick_ok", &again)
	if again.Session != ok.Session {
		t.Errorf("el token cambió al reconectar: %q → %q", ok.Session, again.Session)
	}

	var hist protocol.History
	expect(t, second, "history", &hist)
	if hist.Channel != "ramo-redes" {
		t.Errorf("historial del canal %q, quería ramo-redes", hist.Channel)
	}

	// La conexión vieja debe morir con 1008 para que el cliente sepa
	// que no vale la pena reintentar.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := first.Read(ctx); err != nil {
			if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
				t.Fatalf("cierre de la conexión vieja = %v, quería 1008", got)
			}
			break
		}
	}
}

// TestNickTakenSinSesion verifica que sin el token nadie te puede robar
// el nick.
func TestNickTakenSinSesion(t *testing.T) {
	url := newServer(t)

	first := dial(t, url)
	write(t, first, "set_nick", protocol.SetNick{Nick: "ana"})
	expect(t, first, "nick_ok", nil)

	impostor := dial(t, url)
	write(t, impostor, "set_nick", protocol.SetNick{Nick: "ana", Session: "token-inventado"})

	var e protocol.ErrorMsg
	expect(t, impostor, "error", &e)
	if e.Code != "nick_taken" {
		t.Errorf("código = %q, quería nick_taken", e.Code)
	}
}

// TestCanalInexistenteAlReconectar: si el canal guardado ya no existe,
// el cliente aterriza en general en vez de quedar en la nada.
func TestCanalInexistenteAlReconectar(t *testing.T) {
	url := newServer(t)

	conn := dial(t, url)
	write(t, conn, "set_nick", protocol.SetNick{Nick: "ana", Channel: "fantasma"})

	var ok protocol.NickOK
	expect(t, conn, "nick_ok", &ok)
	if ok.Channel != "general" {
		t.Errorf("canal = %q, quería general", ok.Channel)
	}
}
