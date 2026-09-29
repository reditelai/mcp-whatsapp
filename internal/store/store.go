// Package store keeps messages, chats and contacts in app.db.
//
// Only chats the config allows to read ever get here. The instance holding
// the lock writes; other instances (other Claude Code conversations) only
// read, which SQLite in WAL mode allows.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DSN builds a modernc sqlite DSN. foreign_keys must be set here: the
// whatsmeow sqlstore refuses to upgrade without it.
func DSN(path string) string {
	v := url.Values{}
	v.Add("_pragma", "journal_mode(WAL)")
	v.Add("_pragma", "busy_timeout(5000)")
	v.Add("_pragma", "foreign_keys(1)")
	return "file:" + path + "?" + v.Encode()
}

// Store is app.db.
type Store struct {
	db *sql.DB
}

// Open opens (and migrates) app.db.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS chats (
	jid             TEXT PRIMARY KEY,
	kind            TEXT NOT NULL,
	phone           TEXT NOT NULL DEFAULT '',
	name            TEXT NOT NULL DEFAULT '',
	last_message_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	seq          INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_jid     TEXT NOT NULL,
	id           TEXT NOT NULL,
	sender_jid   TEXT NOT NULL DEFAULT '',
	sender_phone TEXT NOT NULL DEFAULT '',
	sender_name  TEXT NOT NULL DEFAULT '',
	from_me      INTEGER NOT NULL DEFAULT 0,
	ts           INTEGER NOT NULL,
	kind         TEXT NOT NULL,
	text         TEXT NOT NULL DEFAULT '',
	quoted_id    TEXT NOT NULL DEFAULT '',
	media_mime   TEXT NOT NULL DEFAULT '',
	media_name   TEXT NOT NULL DEFAULT '',
	media_size   INTEGER NOT NULL DEFAULT 0,
	media_ref    BLOB,
	media_path   TEXT NOT NULL DEFAULT '',
	edited       INTEGER NOT NULL DEFAULT 0,
	deleted      INTEGER NOT NULL DEFAULT 0,
	raw_chat     TEXT NOT NULL DEFAULT '',
	raw_sender   TEXT NOT NULL DEFAULT '',
	rev          INTEGER NOT NULL DEFAULT 0,
	transcript   TEXT NOT NULL DEFAULT '',
	transcript_status TEXT NOT NULL DEFAULT '',
	UNIQUE (chat_jid, id)
);
CREATE INDEX IF NOT EXISTS messages_chat_ts ON messages (chat_jid, ts);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5 (
	text, content='messages', content_rowid='seq', tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts (rowid, text) VALUES (new.seq, new.text);
END;
CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE OF text ON messages BEGIN
	INSERT INTO messages_fts (messages_fts, rowid, text) VALUES ('delete', old.seq, old.text);
	INSERT INTO messages_fts (rowid, text) VALUES (new.seq, new.text);
END;
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Columns added after the first installs (0.1.0).
	for _, c := range []struct{ name, ddl string }{
		{"rev", `ALTER TABLE messages ADD COLUMN rev INTEGER NOT NULL DEFAULT 0; UPDATE messages SET rev = seq`},
		{"transcript", `ALTER TABLE messages ADD COLUMN transcript TEXT NOT NULL DEFAULT ''`},
		{"transcript_status", `ALTER TABLE messages ADD COLUMN transcript_status TEXT NOT NULL DEFAULT ''`},
	} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name = ?`, c.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := s.db.Exec(c.ddl); err != nil {
				return err
			}
		}
	}
	_, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS messages_rev ON messages (rev)`)
	return err
}

// nextRev is the change counter: every insert, edit and delete gets a new
// rev, so wa_new_messages reports changes too, not only new messages. Only
// the instance holding the lock writes, so MAX()+1 cannot race.
const nextRev = `(SELECT COALESCE(MAX(rev), 0) + 1 FROM messages)`

// Chat is a stored chat.
type Chat struct {
	JID           string `json:"chat"`
	Kind          string `json:"kind"`
	Phone         string `json:"phone,omitempty"`
	Name          string `json:"name,omitempty"`
	LastMessageAt string `json:"last_message_at,omitempty"`
	Messages      int    `json:"messages"`
}

// Message is a stored message.
type Message struct {
	Rev         int64  `json:"-"`
	Chat        string `json:"chat"`
	ID          string `json:"id"`
	SenderJID   string `json:"sender,omitempty"`
	SenderPhone string `json:"sender_phone,omitempty"`
	SenderName  string `json:"sender_name,omitempty"`
	FromMe      bool   `json:"from_me"`
	Time        string `json:"time"`
	Kind        string `json:"kind"`
	Text        string `json:"text,omitempty"`
	QuotedID    string `json:"reply_to,omitempty"`
	MediaMime   string `json:"media_mime,omitempty"`
	MediaName   string `json:"media_name,omitempty"`
	MediaSize   int64  `json:"media_size,omitempty"`
	MediaPath   string `json:"media_path,omitempty"`
	Edited      bool   `json:"edited,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`
	// Voice notes: machine transcript and its state ("pending", "done",
	// "failed: …", "" when transcription is not installed).
	Transcript       string `json:"transcript,omitempty"`
	TranscriptStatus string `json:"transcript_status,omitempty"`

	// Addresses exactly as WhatsApp sent them (possibly LIDs); receipts and
	// reply quotes must use these, not the rewritten phone-number form.
	RawChat   string `json:"-"`
	RawSender string `json:"-"`

	ts       time.Time
	mediaRef []byte
}

// NewMessage is what the WhatsApp side hands over for storing.
type NewMessage struct {
	Chat, ChatKind, ChatPhone, ChatName string
	ID                                  string
	SenderJID, SenderPhone, SenderName  string
	FromMe                              bool
	Time                                time.Time
	Kind, Text, QuotedID                string
	MediaMime, MediaName                string
	MediaSize                           int64
	MediaRef                            []byte
	RawChat, RawSender                  string
}

// SaveMessage stores a message and upserts its chat. A message that is
// already stored (history sync, retries) is left as it is.
func (s *Store) SaveMessage(ctx context.Context, m NewMessage) (inserted bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := upsertChat(ctx, tx, m.Chat, m.ChatKind, m.ChatPhone, m.ChatName, m.Time); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO messages
		(chat_jid, id, sender_jid, sender_phone, sender_name, from_me, ts, kind, text, quoted_id,
		 media_mime, media_name, media_size, media_ref, raw_chat, raw_sender, rev)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,`+nextRev+`)`,
		m.Chat, m.ID, m.SenderJID, m.SenderPhone, m.SenderName, b2i(m.FromMe), m.Time.Unix(),
		m.Kind, m.Text, m.QuotedID, m.MediaMime, m.MediaName, m.MediaSize, m.MediaRef, m.RawChat, m.RawSender)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, tx.Commit()
}

func upsertChat(ctx context.Context, tx *sql.Tx, jid, kind, phone, name string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO chats (jid, kind, phone, name, last_message_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT (jid) DO UPDATE SET
			phone = CASE WHEN excluded.phone <> '' THEN excluded.phone ELSE chats.phone END,
			name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE chats.name END,
			last_message_at = MAX(chats.last_message_at, excluded.last_message_at)`,
		jid, kind, phone, name, at.Unix())
	return err
}

// SetChatName updates a chat's display name (contact or group name).
func (s *Store) SetChatName(ctx context.Context, jid, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET name = ? WHERE jid = ? AND ? <> ''`, name, jid, name)
	return err
}

// EditMessage replaces the text of a stored message. found is false when
// the message is not stored (the caller logs it: an edit must not vanish
// silently).
func (s *Store) EditMessage(ctx context.Context, chat, id, text string) (found bool, err error) {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET text = ?, edited = 1, rev = `+nextRev+` WHERE chat_jid = ? AND id = ?`, text, chat, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteMessage marks a message as deleted for everyone and drops its text
// and media reference: the sender took it back, so it is not kept. It
// returns the path of the downloaded media file, which the caller removes.
func (s *Store) DeleteMessage(ctx context.Context, chat, id string) (mediaPath string, found bool, err error) {
	_ = s.db.QueryRowContext(ctx, `SELECT media_path FROM messages WHERE chat_jid = ? AND id = ?`, chat, id).Scan(&mediaPath)
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET text = '', deleted = 1, media_ref = NULL, media_path = '', rev = `+nextRev+` WHERE chat_jid = ? AND id = ?`, chat, id)
	if err != nil {
		return "", false, err
	}
	n, _ := res.RowsAffected()
	return mediaPath, n > 0, nil
}

// SetMediaPath records where the downloaded media file is.
// A deleted message keeps no media: the update does not apply to it.
func (s *Store) SetMediaPath(ctx context.Context, chat, id, path string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET media_path = ? WHERE chat_jid = ? AND id = ? AND deleted = 0`, path, chat, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

const msgCols = `rev, chat_jid, id, sender_jid, sender_phone, sender_name, from_me, ts, kind, text,
	quoted_id, media_mime, media_name, media_size, media_path, edited, deleted, media_ref, raw_chat, raw_sender,
	transcript, transcript_status`

func scanMessages(rows *sql.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var fromMe, edited, deleted int
		var ts int64
		if err := rows.Scan(&m.Rev, &m.Chat, &m.ID, &m.SenderJID, &m.SenderPhone, &m.SenderName, &fromMe,
			&ts, &m.Kind, &m.Text, &m.QuotedID, &m.MediaMime, &m.MediaName, &m.MediaSize, &m.MediaPath,
			&edited, &deleted, &m.mediaRef, &m.RawChat, &m.RawSender, &m.Transcript, &m.TranscriptStatus); err != nil {
			return nil, err
		}
		m.FromMe, m.Edited, m.Deleted = fromMe == 1, edited == 1, deleted == 1
		m.SenderPhone = plus(m.SenderPhone)
		m.ts = time.Unix(ts, 0)
		m.Time = m.ts.UTC().Format(time.RFC3339)
		out = append(out, m)
	}
	return out, rows.Err()
}

// MediaRef returns the stored download reference of a message.
func (m Message) MediaRef() []byte { return m.mediaRef }

// Timestamp returns the message time.
func (m Message) Timestamp() time.Time { return m.ts }

// GetMessage returns one message.
func (s *Store) GetMessage(ctx context.Context, chat, id string) (*Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages WHERE chat_jid = ? AND id = ?`, chat, id)
	if err != nil {
		return nil, err
	}
	ms, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, ErrNotFound
	}
	return &ms[0], nil
}

// ErrNotFound is returned when a message or chat does not exist.
var ErrNotFound = errors.New("not found")

// ChatMessages returns up to limit messages of a chat older than before
// (zero = newest), newest first.
func (s *Store) ChatMessages(ctx context.Context, chat string, before time.Time, limit int) ([]Message, error) {
	q := `SELECT ` + msgCols + ` FROM messages WHERE chat_jid = ?`
	args := []any{chat}
	if !before.IsZero() {
		q += ` AND ts < ?`
		args = append(args, before.Unix())
	}
	q += ` ORDER BY ts DESC, rev DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// After returns messages new or changed (edited, deleted) after cursor,
// in order of the change.
func (s *Store) After(ctx context.Context, cursor int64, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages WHERE rev > ? ORDER BY rev LIMIT ?`, cursor, limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// Latest returns the newest limit messages, oldest first.
func (s *Store) Latest(ctx context.Context, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (SELECT `+msgCols+` FROM messages ORDER BY rev DESC LIMIT ?) ORDER BY rev`, limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// SetTranscript stores a voice note transcript (or its state). A finished
// transcript is a change: the message comes again in wa_new_messages.
func (s *Store) SetTranscript(ctx context.Context, chat, id, text, status string) error {
	bump := ""
	if status != "pending" {
		bump = ", rev = " + nextRev
	}
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET transcript = ?, transcript_status = ?`+bump+` WHERE chat_jid = ? AND id = ? AND deleted = 0`, text, status, chat, id)
	return err
}

// VoiceBacklog returns voice notes since t that have a file but no
// transcript yet, oldest first.
func (s *Store) VoiceBacklog(ctx context.Context, since time.Time, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages
		WHERE kind = 'voice' AND deleted = 0 AND media_path <> '' AND transcript_status IN ('', 'pending') AND ts >= ?
		ORDER BY ts LIMIT ?`, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// MaxRev returns the current end of the change log.
func (s *Store) MaxRev(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(rev) FROM messages`).Scan(&n)
	return n.Int64, err
}

// Search runs a full-text query. Words are matched as prefixes; diacritics
// are ignored.
func (s *Store) Search(ctx context.Context, query, chat string, since, until time.Time, limit int) ([]Message, error) {
	fts := ftsQuery(query)
	if fts == "" {
		return nil, fmt.Errorf("empty query")
	}
	q := `SELECT ` + prefixed(msgCols, "m.") + ` FROM messages_fts f JOIN messages m ON m.seq = f.rowid
		WHERE messages_fts MATCH ?`
	args := []any{fts}
	if chat != "" {
		q += ` AND m.chat_jid = ?`
		args = append(args, chat)
	}
	if !since.IsZero() {
		q += ` AND m.ts >= ?`
		args = append(args, since.Unix())
	}
	if !until.IsZero() {
		q += ` AND m.ts < ?`
		args = append(args, until.Unix())
	}
	q += ` ORDER BY m.ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func prefixed(cols, p string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = p + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// ftsQuery turns free text into an FTS5 query of quoted prefix terms, so
// user input can never be read as FTS syntax.
func ftsQuery(q string) string {
	var terms []string
	for _, w := range strings.Fields(q) {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			terms = append(terms, `"`+w+`"*`)
		}
	}
	return strings.Join(terms, " ")
}

// Chats lists stored chats, most recent first. query filters by name or phone.
func (s *Store) Chats(ctx context.Context, query string, limit int) ([]Chat, error) {
	q := `SELECT c.jid, c.kind, c.phone, c.name, c.last_message_at,
		(SELECT COUNT(*) FROM messages m WHERE m.chat_jid = c.jid)
		FROM chats c`
	var args []any
	if query != "" {
		q += ` WHERE c.name LIKE ? OR c.phone LIKE ?`
		args = append(args, "%"+query+"%", "%"+strings.TrimPrefix(strings.ReplaceAll(query, " ", ""), "+")+"%")
	}
	q += ` ORDER BY c.last_message_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var at int64
		if err := rows.Scan(&c.JID, &c.Kind, &c.Phone, &c.Name, &at, &c.Messages); err != nil {
			return nil, err
		}
		c.Phone = plus(c.Phone)
		if at > 0 {
			c.LastMessageAt = time.Unix(at, 0).UTC().Format(time.RFC3339)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChatByJID returns one stored chat.
func (s *Store) ChatByJID(ctx context.Context, jid string) (*Chat, error) {
	var c Chat
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT jid, kind, phone, name, last_message_at FROM chats WHERE jid = ?`, jid).
		Scan(&c.JID, &c.Kind, &c.Phone, &c.Name, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if at > 0 {
		c.LastMessageAt = time.Unix(at, 0).UTC().Format(time.RFC3339)
	}
	return &c, nil
}

// Counts returns numbers of chats and messages.
func (s *Store) Counts(ctx context.Context) (chats, messages int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM chats), (SELECT COUNT(*) FROM messages)`).Scan(&chats, &messages)
	return
}

// plus shows a stored phone number (digits) in international form.
func plus(phone string) string {
	if phone == "" {
		return ""
	}
	return "+" + phone
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
