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
