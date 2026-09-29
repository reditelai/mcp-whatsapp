package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveSearchCursor(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	for i, txt := range []string{"Ahoj, zítra v devět", "Přijedu pozdě", "Díky"} {
		ins, err := s.SaveMessage(ctx, NewMessage{Chat: "420777000111@s.whatsapp.net", ChatKind: "direct",
			ChatPhone: "420777000111", ID: string(rune('a' + i)), Time: now.Add(time.Duration(i) * time.Second),
			Kind: "text", Text: txt})
		if err != nil || !ins {
			t.Fatalf("save %d: %v %v", i, ins, err)
		}
	}
	if ins, _ := s.SaveMessage(ctx, NewMessage{Chat: "420777000111@s.whatsapp.net", ChatKind: "direct", ID: "a", Time: now, Kind: "text", Text: "x"}); ins {
		t.Fatal("duplicate inserted")
	}
	res, err := s.Search(ctx, "prijedu", "", time.Time{}, time.Time{}, 10)
	if err != nil || len(res) != 1 || res[0].ID != "b" {
		t.Fatalf("search without diacritics: %v %+v", err, res)
	}
	after, _ := s.After(ctx, 1, 10)
	if len(after) != 2 || after[0].ID != "b" {
		t.Fatalf("after: %+v", after)
	}
	if found, err := s.EditMessage(ctx, "420777000111@s.whatsapp.net", "c", "Díky moc"); err != nil || !found {
		t.Fatal(err)
	}
	if found, _ := s.EditMessage(ctx, "420777000111@s.whatsapp.net", "zzz", "x"); found {
		t.Fatal("edit of unknown message reported as found")
	}
	// The edit moves the message to the end of the change log.
	if ch, _ := s.After(ctx, 3, 10); len(ch) != 1 || ch[0].ID != "c" || !ch[0].Edited {
		t.Fatalf("edit not in change log: %+v", ch)
	}
	if res, _ := s.Search(ctx, "moc", "", time.Time{}, time.Time{}, 10); len(res) != 1 || !res[0].Edited {
		t.Fatalf("edit not searchable: %+v", res)
	}
	if _, _, err := s.DeleteMessage(ctx, "420777000111@s.whatsapp.net", "a"); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.Search(ctx, "devet", "", time.Time{}, time.Time{}, 10); len(res) != 0 {
		t.Fatalf("deleted still found: %+v", res)
	}
	chats, _ := s.Chats(ctx, "+420 777", 10)
	if len(chats) != 1 || chats[0].Messages != 3 || chats[0].Phone != "+420777000111" {
		t.Fatalf("chats: %+v", chats)
	}
}

func TestMigrateAddsRev(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(schema, "\trev          INTEGER NOT NULL DEFAULT 0,\n", "", 1)
	if _, err := db.Exec(old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages (chat_jid, id, ts, kind, text) VALUES ('c', 'a', 1, 'text', 'x'), ('c', 'b', 2, 'text', 'y')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after, err := s.After(context.Background(), 0, 10)
	if err != nil || len(after) != 2 || after[0].Rev != 1 || after[1].Rev != 2 {
		t.Fatalf("migration: %v %+v", err, after)
	}
}

func TestRelativeMedia(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	media := filepath.Join(dir, "vault", "vstupy", "whatsapp")
	s.SetMediaBase(media)
	s.SaveMessage(ctx, NewMessage{Chat: "c", ChatKind: "direct", ID: "a", Time: time.Now(), Kind: "voice"})
	abs := filepath.Join(media, "Jana", "2026-09-29_1415_hlasovka.ogg")
	if ok, err := s.SetMediaPath(ctx, "c", "a", abs); err != nil || !ok {
		t.Fatal(err)
	}
	var raw string
	s.db.QueryRow(`SELECT media_path FROM messages WHERE id = 'a'`).Scan(&raw)
	if raw != "Jana/2026-09-29_1415_hlasovka.ogg" {
		t.Fatalf("stored %q", raw)
	}
	// The folder moves: the path follows.
	moved := filepath.Join(dir, "elsewhere", "vstupy", "whatsapp")
	s.SetMediaBase(moved)
	m, _ := s.GetMessage(ctx, "c", "a")
	if m.MediaPath != filepath.Join(moved, "Jana", "2026-09-29_1415_hlasovka.ogg") {
		t.Fatalf("after move %q", m.MediaPath)
	}
}

func TestMediaFolderStays(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SaveMessage(ctx, NewMessage{Chat: "c", ChatKind: "direct", ChatName: "Jana", ID: "a", Time: time.Now(), Kind: "image"})
	if f, _ := s.MediaFolder(ctx, "c", "Jana"); f != "Jana" {
		t.Fatalf("first: %q", f)
	}
	if f, _ := s.MediaFolder(ctx, "c", "Jana Nová"); f != "Jana" {
		t.Fatalf("after rename: %q", f)
	}
}

func TestServerStateAndReader(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.db")
	if _, err := OpenReader(path); err == nil {
		t.Fatal("reader opened a missing database")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.GetServerState(ctx); err != ErrNotFound {
		t.Fatalf("empty: %v", err)
	}
	now := time.Now().Truncate(time.Second)
	for _, st := range []string{"connecting", "connected"} {
		if err := s.SetServerState(ctx, ServerState{State: st, Since: now, PID: 7, Version: "0.3.0", Updated: now}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.GetServerState(ctx)
	if err != nil || got.State != "connected" || got.PID != 7 || !got.Updated.Equal(now) {
		t.Fatalf("%v %+v", err, got)
	}
	if err := r.SetServerState(ctx, ServerState{State: "x"}); err == nil {
		t.Fatal("reader could write")
	}
}
