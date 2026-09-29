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
