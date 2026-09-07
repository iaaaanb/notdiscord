// Package store persiste canales y mensajes en SQLite.
//
// El hub llama a estas funciones desde su única goroutine, así que no
// hay concurrencia sobre el *Store. En un chat local las escrituras
// toman microsegundos; si esto creciera, el paso siguiente sería mover
// las escrituras a una goroutine propia con una cola.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3" // registra el driver "sqlite3"

	"github.com/iaaaanb/notdiscord/internal/protocol"
)

// driverName está aislado aquí: para cambiar a modernc.org/sqlite basta
// reemplazar el import de arriba y poner "sqlite" en esta constante.
const driverName = "sqlite3"

const schema = `
CREATE TABLE IF NOT EXISTS channels (
	id   INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY,
	channel_id INTEGER NOT NULL REFERENCES channels(id),
	author     TEXT NOT NULL,
	content    TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_channel
	ON messages(channel_id, id);
`

type Store struct {
	db *sql.DB
}

// Open abre (o crea) la base y deja el esquema listo.
func Open(path string) (*Store, error) {
	// WAL permite lecturas concurrentes con escrituras; foreign_keys
	// viene apagado por defecto en SQLite y hay que pedirlo.
	db, err := sql.Open(driverName, path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("abrir %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("crear esquema: %w", err)
	}
	s := &Store{db: db}
	if err := s.CreateChannel("general"); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ChannelNames devuelve todos los canales guardados.
func (s *Store) ChannelNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM channels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CreateChannel inserta un canal; si ya existe no hace nada.
func (s *Store) CreateChannel(name string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO channels (name) VALUES (?)`, name)
	return err
}

// SaveMessage guarda un mensaje en su canal.
func (s *Store) SaveMessage(m protocol.Message) error {
	res, err := s.db.Exec(`
		INSERT INTO messages (channel_id, author, content, created_at)
		SELECT id, ?, ?, ? FROM channels WHERE name = ?`,
		m.Author, m.Content, m.SentAt.Format(time.RFC3339Nano), m.Channel)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("canal %q no existe en la base", m.Channel)
	}
	return nil
}

// History devuelve los últimos limit mensajes del canal, del más
// antiguo al más nuevo (listos para pintar de arriba hacia abajo).
func (s *Store) History(channel string, limit int) ([]protocol.Message, error) {
	rows, err := s.db.Query(`
		SELECT m.author, m.content, m.created_at
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE c.name = ?
		ORDER BY m.id DESC
		LIMIT ?`, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []protocol.Message
	for rows.Next() {
		var m protocol.Message
		var ts string
		if err := rows.Scan(&m.Author, &m.Content, &ts); err != nil {
			return nil, err
		}
		m.Channel = channel
		m.SentAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// La query trae DESC (para cortar con LIMIT); invertimos a ASC.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
