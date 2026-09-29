// Package policy decides which chats the server may read from and send to.
//
// WhatsApp addresses one person in two ways: by phone number
// (420777123456@s.whatsapp.net) and by an internal LID (…@lid). The config
// lists phone numbers, so a chat that arrives as a LID must be resolved to
// its phone number first. A LID that cannot be resolved is not allowed:
// an unknown identity must never slip past the filter.
package policy

import (
	"go.mau.fi/whatsmeow/types"

	"github.com/reditelai/mcp-whatsapp/internal/config"
)

// Policy holds the read and send scopes.
type Policy struct {
	read, send config.Scope
}

// New builds a policy from the validated config.
func New(cfg *config.Config) *Policy {
	return &Policy{read: cfg.Read, send: cfg.Send}
}

// Kind of chat for the policy.
type Kind int

const (
	KindOther  Kind = iota // broadcast lists, newsletters, status: never allowed
	KindDirect             // one person
	KindGroup
)

// Classify returns the chat kind of a JID.
func Classify(jid types.JID) Kind {
	switch jid.Server {
	case types.GroupServer:
		return KindGroup
	case types.DefaultUserServer, types.HiddenUserServer:
		return KindDirect
	default:
		return KindOther
	}
}

// CanRead reports whether messages in chat may be stored and returned.
// phone is the resolved phone number (digits only) of a direct chat, or ""
// when it is not known; group chats ignore it.
func (p *Policy) CanRead(chat types.JID, phone string) bool {
	return allowed(p.read, chat, phone)
}

// CanSend reports whether the server may send to chat.
func (p *Policy) CanSend(chat types.JID, phone string) bool {
	return allowed(p.send, chat, phone) && allowed(p.read, chat, phone)
}

func allowed(s config.Scope, chat types.JID, phone string) bool {
	switch Classify(chat) {
	case KindGroup:
		if s.AllGroups {
			return true
		}
		return contains(s.Groups, chat.ToNonAD().String())
	case KindDirect:
		if s.AllChats {
			return true
		}
		if phone == "" && chat.Server == types.DefaultUserServer {
			phone = chat.User
		}
		return phone != "" && contains(s.Chats, phone)
	default:
		return false
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
