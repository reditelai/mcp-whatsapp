package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	c, err := Parse([]byte(`{"read":{"chats":"all","groups":false},"send":{"chats":["+420 777 123 456"]}}`), "/v/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Read.AllChats || c.Read.AllGroups || len(c.Send.Chats) != 1 || c.Send.Chats[0] != "420777123456" || c.DeviceName != "Miládka" {
		t.Fatalf("%+v", c)
	}
	if _, err := Parse([]byte(`{"read":{"chats":["+420777000111"]},"send":{"chats":["+420777123456"]}}`), "x"); err == nil || !strings.Contains(err.Error(), "není v read.chats") {
		t.Fatalf("send outside read accepted: %v", err)
	}
	if _, err := Parse([]byte(`{"read":{"chats":"vse"}}`), "x"); err == nil {
		t.Fatal("bad value accepted")
	}
	if _, err := Parse([]byte(`{"reed":{}}`), "x"); err == nil {
		t.Fatal("unknown key accepted")
	}
	c, _ = Parse([]byte(`{}`), "x")
	if c.Read.AllChats || len(c.Send.Chats) != 0 {
		t.Fatal("empty config must allow nothing")
	}
}

func TestDefaultDirs(t *testing.T) {
	c, err := parseWith([]byte(`{}`), "/v/.miladka/secrets/whatsapp/config.json", "/v/.doplnky/mcp-whatsapp")
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != "/v/.doplnky/mcp-whatsapp/data" || c.Transcription.Dir != "/v/.doplnky/prepis" {
		t.Fatalf("add-on folder: %s %s", c.DataDir, c.Transcription.Dir)
	}
	c, _ = parseWith([]byte(`{}`), "/h/mcp-whatsapp/config.json", "/h/mcp-whatsapp")
	if c.DataDir != "/h/mcp-whatsapp/data" || c.Transcription.Dir != "/h/mcp-whatsapp/data/stt" {
		t.Fatalf("plain folder: %s %s", c.DataDir, c.Transcription.Dir)
	}
	c, _ = parseWith([]byte(`{"data_dir":"d","transcription":{"dir":"/x/prepis"}}`), "/h/c/config.json", "/h/bin")
	if c.DataDir != "/h/c/d" || c.Transcription.Dir != "/x/prepis" || c.MediaDir != "/h/c/d/media" || c.MediaKeep != 30 {
		t.Fatalf("explicit: %+v", c)
	}
	// In Miládka's add-on folder relative paths start at her folder.
	c, err = parseWith([]byte(`{"media_dir":"vstupy/whatsapp","media_keep_days":0,"send":{"files":["poznamky"]}}`), "/v/.miladka/secrets/whatsapp/config.json", "/v/.doplnky/mcp-whatsapp")
	if err != nil || c.MediaDir != "/v/vstupy/whatsapp" || c.MediaKeep != 0 || len(c.Send.FileDirs) != 1 || c.Send.FileDirs[0] != "/v/poznamky" {
		t.Fatalf("vault relative: %v %+v", err, c)
	}
}

func TestOwner(t *testing.T) {
	c, err := Parse([]byte(`{"read":{"chats":"all"},"owner":["+420 724 000 111"]}`), "x")
	if err != nil || len(c.Owners) != 1 || c.Owners[0] != "420724000111" || c.Wake != "owner" {
		t.Fatalf("%v %+v", err, c)
	}
	if !c.IsOwner("+420724000111") || !c.IsOwner("420724000111") || c.IsOwner("+420600000000") || c.IsOwner("") {
		t.Fatal("IsOwner")
	}
	if _, err := Parse([]byte(`{"read":{"chats":["+420777000111"]},"owner":["+420724000111"]}`), "x"); err == nil {
		t.Fatal("owner outside read accepted")
	}
	if _, err := Parse([]byte(`{"wake":"vse"}`), "x"); err == nil {
		t.Fatal("bad wake accepted")
	}
}

func TestWake(t *testing.T) {
	c, err := Parse([]byte(`{"read":{"chats":"all","groups":["1203@g.us"]},"owner":["+420724000111"],"wake":["+420 600 000 000","1203@g.us"]}`), "x")
	if err != nil || c.Wake != "list" || len(c.WakeChats) != 1 || len(c.WakeGroups) != 1 {
		t.Fatalf("%v %+v", err, c)
	}
	for _, tc := range []struct {
		sender, chat string
		want         bool
	}{
		{"+420724000111", "420724000111@s.whatsapp.net", true}, // owner
		{"+420600000000", "420600000000@s.whatsapp.net", true}, // listed person
		{"+420600000000", "9999@g.us", true},                   // listed person in another group
		{"+420555000000", "1203@g.us", true},                   // anyone in a listed group
		{"+420555000000", "420555000000@s.whatsapp.net", false},
	} {
		if got := c.Wakes(tc.sender, tc.chat); got != tc.want {
			t.Errorf("Wakes(%s, %s) = %v", tc.sender, tc.chat, got)
		}
	}
	if s := c.WakeSummary(); s != "owner, +420600000000, 1203@g.us" {
		t.Fatalf("summary %q", s)
	}
	if _, err := Parse([]byte(`{"read":{"chats":"all"},"wake":["1203@g.us"]}`), "x"); err == nil {
		t.Fatal("unreadable wake group accepted")
	}
	if _, err := Parse([]byte(`{"read":{"chats":"all"},"wake":["kolega"]}`), "x"); err == nil {
		t.Fatal("bad wake item accepted")
	}
	if c, _ := Parse([]byte(`{"read":{"chats":"all"},"wake":"all"}`), "x"); c.Wake != "all" || !c.Wakes("+420555000000", "x") {
		t.Fatal("all")
	}
}
