package wa

import (
	"context"
	"strings"
	"time"

	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
)

// Incoming messages go straight into the running conversation through the
// Claude Code "channel" notification (claude/channel). It works where the
// client has channels enabled - today Claude Code in a terminal, started
// with --dangerously-load-development-channels server:whatsapp. Elsewhere
// the notification is ignored and wa_new_messages (a periodic check) is the
// way in. See SPEC.md, Zprávy do konverzace.

// ChannelMethod is the notification Claude Code listens to.
const ChannelMethod = "notifications/claude/channel"

// pushWindow: only messages this fresh are pushed (a backlog transcript of
// an old voice note is not news).
const pushWindow = time.Hour

// SetNotifier connects the MCP server's notification sender.
func (m *Manager) SetNotifier(fn func(method string, params map[string]any)) {
	m.mu.Lock()
	m.notify = fn
	m.mu.Unlock()
}

// isOwner: the sender's own requests come only from the owner numbers
// (config "owner"). Checked on the sender, not the chat: in a group anyone
// could otherwise speak in the owner's name.
func (m *Manager) isOwner(msg *appstore.Message) bool {
	phone := strings.TrimPrefix(msg.SenderPhone, "+")
	for _, o := range m.cfg.Owners {
		if phone == o {
			return true
		}
	}
	return false
}

// push delivers one stored message into the conversation, once.
func (m *Manager) push(ctx context.Context, chat, id string) {
	m.mu.Lock()
	notify := m.notify
	m.mu.Unlock()
	if notify == nil || m.cfg.Notify == "off" {
		return
	}
	msg, err := m.st.GetMessage(ctx, chat, id)
	if err != nil || msg.FromMe || msg.Deleted || msg.Kind == "reaction" {
		return
	}
	owner := m.isOwner(msg)
	if !owner && m.cfg.Notify != "all" {
		return
	}
	if first, err := m.st.MarkPushed(ctx, chat, id); err != nil || !first {
		return
	}
	meta := map[string]string{
		"chat":       msg.Chat,
		"message_id": msg.ID,
		"kind":       msg.Kind,
		"time":       msg.Time,
		"from_owner": boolStr(owner),
	}
	if msg.SenderPhone != "" {
		meta["sender_phone"] = msg.SenderPhone
	}
	if msg.SenderName != "" {
		meta["sender_name"] = msg.SenderName
	}
	if msg.Forwarded {
		meta["forwarded"] = "true"
	}
	if msg.MediaPath != "" {
		meta["media_path"] = msg.MediaPath
	}
	notify(ChannelMethod, map[string]any{"content": channelContent(msg), "meta": meta})
}

func channelContent(msg *appstore.Message) string {
	switch msg.Kind {
	case "text":
		return msg.Text
	case "voice":
		switch {
		case msg.Transcript != "":
			return "[hlasovka, strojový přepis] " + msg.Transcript
		case strings.HasPrefix(msg.TranscriptStatus, "failed"):
			return "[hlasovka, přepis se nepovedl]"
		default:
			return "[hlasovka bez přepisu]"
		}
	default:
		label := "[" + msg.Kind + "]"
		if msg.MediaName != "" {
			label = "[" + msg.Kind + ": " + msg.MediaName + "]"
		}
		if msg.Text != "" {
			return label + " " + msg.Text
		}
		return label
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
