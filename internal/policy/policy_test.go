package policy

import (
	"testing"

	"go.mau.fi/whatsmeow/types"

	"github.com/reditelai/mcp-whatsapp/internal/config"
)

func TestPolicy(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"read":{"chats":"all","groups":["120363000000000001@g.us"]},"send":{"chats":["+420777123456"]}}`), "x")
	if err != nil {
		t.Fatal(err)
	}
	p := New(cfg)
	pn := types.NewJID("420777123456", types.DefaultUserServer)
	other := types.NewJID("420600000000", types.DefaultUserServer)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	group := types.NewJID("120363000000000001", types.GroupServer)
	otherGroup := types.NewJID("120363000000000002", types.GroupServer)

	if !p.CanRead(other, "") || !p.CanRead(lid, "") {
		t.Fatal("read all must allow any person, also unresolved LID")
	}
	if !p.CanRead(group, "") || p.CanRead(otherGroup, "") {
		t.Fatal("group list")
	}
	if !p.CanSend(pn, "") || p.CanSend(other, "") {
		t.Fatal("send list by phone")
	}
	if p.CanSend(lid, "") {
		t.Fatal("unresolved LID must not be sendable")
	}
	if !p.CanSend(lid, "420777123456") {
		t.Fatal("LID resolved to allowed phone must be sendable")
	}
	if p.CanSend(group, "") {
		t.Fatal("groups not in send")
	}
	if p.CanRead(types.NewJID("status", types.BroadcastServer), "") {
		t.Fatal("broadcast never allowed")
	}
}
