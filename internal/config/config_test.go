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
	if err != nil || len(c.Owners) != 1 || c.Owners[0] != "420724000111" || c.Notify != "owner" {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := Parse([]byte(`{"read":{"chats":["+420777000111"]},"owner":["+420724000111"]}`), "x"); err == nil {
		t.Fatal("owner outside read accepted")
	}
	if _, err := Parse([]byte(`{"channel":{"notify":"vse"}}`), "x"); err == nil {
		t.Fatal("bad notify accepted")
	}
}
