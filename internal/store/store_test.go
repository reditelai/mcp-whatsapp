package store

import (
	"context"
	"path/filepath"
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
	if err := s.EditMessage(ctx, "420777000111@s.whatsapp.net", "c", "Díky moc"); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.Search(ctx, "moc", "", time.Time{}, time.Time{}, 10); len(res) != 1 || !res[0].Edited {
		t.Fatalf("edit not searchable: %+v", res)
	}
	if _, err := s.DeleteMessage(ctx, "420777000111@s.whatsapp.net", "a"); err != nil {
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
