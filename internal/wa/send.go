package wa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/reditelai/mcp-whatsapp/internal/policy"
	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
)

// ErrSendForbidden: the config does not allow sending there.
var ErrSendForbidden = errors.New("send_forbidden")

// maxSendFile is the upload size limit (WhatsApp documents go up to 2 GB,
// but an assistant has no business sending that).
const maxSendFile = 100 << 20

// ParseChat turns user input into a chat JID: "+420777123456", "420777123456",
// "420777123456@s.whatsapp.net", "…@g.us" or "…@lid".
func ParseChat(s string) (types.JID, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return types.JID{}, errors.New("empty chat")
	}
	if strings.Contains(t, "@") {
		j, err := types.ParseJID(t)
		if err != nil {
			return types.JID{}, fmt.Errorf("invalid chat %q: %v", s, err)
		}
		return j.ToNonAD(), nil
	}
	digits := strings.ReplaceAll(strings.TrimPrefix(t, "+"), " ", "")
	for _, r := range digits {
		if r < '0' || r > '9' {
			return types.JID{}, fmt.Errorf("invalid chat %q: use a phone number like +420777123456 or a chat id from wa_list_chats", s)
		}
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

// CanonicalChat resolves a chat to the form it is stored under.
func (m *Manager) CanonicalChat(ctx context.Context, chat types.JID) (types.JID, string) {
	m.mu.Lock()
	cli := m.cli
	m.mu.Unlock()
	if policy.Classify(chat) == policy.KindDirect {
		r := m.resolvePerson(ctx, cli, chat, types.EmptyJID)
		return r.jid, r.phone
	}
	return chat, ""
}

// CanRead reports whether a chat may be read, after resolving LIDs.
func (m *Manager) CanRead(ctx context.Context, chat types.JID) (types.JID, bool) {
	c, phone := m.CanonicalChat(ctx, chat)
	return c, m.policy().CanRead(c, phone)
}

func (m *Manager) sendTarget(ctx context.Context, chat string) (types.JID, error) {
	jid, err := ParseChat(chat)
	if err != nil {
		return types.JID{}, err
	}
	c, phone := m.CanonicalChat(ctx, jid)
	if !m.policy().CanSend(c, phone) {
		return types.JID{}, ErrSendForbidden
	}
	return c, nil
}

// SentMessage is the result of a send. Time is UTC in RFC 3339, the same
// form as stored messages.
type SentMessage struct {
	Chat string `json:"chat"`
	ID   string `json:"id"`
	Time string `json:"time"`
}

func sent(target types.JID, resp whatsmeow.SendResponse) *SentMessage {
	return &SentMessage{Chat: target.String(), ID: resp.ID, Time: resp.Timestamp.UTC().Format(time.RFC3339)}
}

// SendText sends a text message, optionally as a reply to a stored message.
func (m *Manager) SendText(ctx context.Context, chat, text, replyTo string) (*SentMessage, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty text")
	}
	target, err := m.sendTarget(ctx, chat)
	if err != nil {
		return nil, err
	}
	cli, err := m.connectedClient()
	if err != nil {
		return nil, err
	}
	msg := &waE2E.Message{Conversation: proto.String(text)}
	if replyTo != "" {
		ci, err := m.replyContext(ctx, target.String(), replyTo)
		if err != nil {
			return nil, err
		}
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: ci}}
	}
	resp, err := cli.SendMessage(ctx, target, msg)
	if err != nil {
		return nil, err
	}
	m.saveSent(ctx, target, resp, "text", text, replyTo, "", "", 0)
	return sent(target, resp), nil
}

func (m *Manager) replyContext(ctx context.Context, chat, id string) (*waE2E.ContextInfo, error) {
	q, err := m.st.GetMessage(ctx, chat, id)
	if err != nil {
		return nil, fmt.Errorf("reply_to %s: message not found in this chat", id)
	}
	ci := &waE2E.ContextInfo{
		StanzaID:      proto.String(q.ID),
		QuotedMessage: &waE2E.Message{Conversation: proto.String(q.Text)},
	}
	// The participant must be the address WhatsApp used (possibly a LID),
	// or the phone will not match the quote to the message.
	sender := q.RawSender
	if sender == "" {
		sender = q.SenderJID
	}
	if sender != "" && !q.FromMe {
		if j, err := types.ParseJID(sender); err == nil {
			sender = j.ToNonAD().String()
		}
		ci.Participant = proto.String(sender)
	} else if cli, err := m.client(); err == nil && cli.Store.ID != nil {
		ci.Participant = proto.String(cli.Store.ID.ToNonAD().String())
	}
	return ci, nil
}

// SendFile uploads a file from an allowed folder and sends it.
func (m *Manager) SendFile(ctx context.Context, chat, path, caption string) (*SentMessage, error) {
	target, err := m.sendTarget(ctx, chat)
	if err != nil {
		return nil, err
	}
	abs, err := m.allowedFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("file: %v", err)
	}
	if info.IsDir() || info.Size() > maxSendFile {
		return nil, fmt.Errorf("file must be a regular file up to %d MB", maxSendFile>>20)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	cli, err := m.connectedClient()
	if err != nil {
		return nil, err
	}
	mimeType := http.DetectContentType(data)
	name := filepath.Base(abs)
	kind, mediaType := "document", whatsmeow.MediaDocument
	switch {
	case strings.HasPrefix(mimeType, "image/jpeg"), strings.HasPrefix(mimeType, "image/png"):
		kind, mediaType = "image", whatsmeow.MediaImage
	case strings.HasPrefix(mimeType, "video/mp4"):
		kind, mediaType = "video", whatsmeow.MediaVideo
	}
	up, err := cli.Upload(ctx, data, mediaType)
	if err != nil {
		return nil, fmt.Errorf("upload: %v", err)
	}
	size := uint64(len(data))
	var msg *waE2E.Message
	switch kind {
	case "image":
		msg = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &size,
			Mimetype: proto.String(mimeType), Caption: proto.String(caption)}}
	case "video":
		msg = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &size,
			Mimetype: proto.String(mimeType), Caption: proto.String(caption)}}
	default:
		msg = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &size,
			Mimetype: proto.String(mimeType), FileName: proto.String(name), Caption: proto.String(caption)}}
	}
	resp, err := cli.SendMessage(ctx, target, msg)
	if err != nil {
		return nil, err
	}
	m.saveSent(ctx, target, resp, kind, caption, "", mimeType, name, int64(size))
	return sent(target, resp), nil
}

// allowedFile checks that path is inside one of send.files and not inside
// the data folder (session keys must never leave).
func (m *Manager) allowedFile(path string) (string, error) {
	if len(m.conf().Send.FileDirs) == 0 {
		return "", fmt.Errorf("%w: sending files is off (no folders in send.files)", ErrSendForbidden)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if within(abs, m.conf().DataDir) || secretPath(abs) {
		return "", fmt.Errorf("%w: files from the server data folder, add-on folders or secrets cannot be sent", ErrSendForbidden)
	}
	for _, d := range m.conf().Send.FileDirs {
		if within(abs, d) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("%w: %s is outside the folders in send.files", ErrSendForbidden, abs)
}

// secretPath: add-on folders (keys of this and other servers) and
// .miladka/secrets are never sent, whatever send.files allows.
func secretPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(foldCase(path)), "/")
	for i, p := range parts {
		if p == ".doplnky" || p == ".addons" || (p == ".miladka" && i+1 < len(parts) && parts[i+1] == "secrets") {
			return true
		}
	}
	return false
}

// foldCase: Windows and macOS file systems ignore case, so path checks must
// too (.doplnky/MCP-whatsapp/data is the same folder as .doplnky/mcp-whatsapp/data).
func foldCase(p string) string {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}

func within(path, dir string) bool {
	path, dir = foldCase(path), foldCase(dir)
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (m *Manager) saveSent(ctx context.Context, target types.JID, resp whatsmeow.SendResponse, kind, text, replyTo, mimeType, name string, size int64) {
	cli, _ := m.client()
	nm := appstore.NewMessage{
		Chat: target.String(), ChatKind: kindName(policy.Classify(target)), ID: resp.ID, FromMe: true,
		Time: resp.Timestamp, Kind: kind, Text: text, QuotedID: replyTo, MediaMime: mimeType, MediaName: name, MediaSize: size,
	}
	if policy.Classify(target) == policy.KindDirect {
		nm.ChatPhone = target.User
	}
	if cli != nil && cli.Store.ID != nil {
		nm.SenderJID = cli.Store.ID.ToNonAD().String()
		nm.SenderPhone = cli.Store.ID.User
	}
	if _, err := m.st.SaveMessage(ctx, nm); err != nil {
		m.log.Warnf("save sent %s: %v", resp.ID, err)
	}
}

// MarkRead marks the latest incoming messages of a chat as read on the phone.
func (m *Manager) MarkRead(ctx context.Context, chat string) (int, error) {
	jid, err := ParseChat(chat)
	if err != nil {
		return 0, err
	}
	c, ok := m.CanRead(ctx, jid)
	if !ok {
		return 0, ErrReadForbidden
	}
	cli, err := m.connectedClient()
	if err != nil {
		return 0, err
	}
	msgs, err := m.st.ChatMessages(ctx, c.String(), time.Time{}, 50)
	if err != nil {
		return 0, err
	}
	// Receipts go to the chat and sender exactly as WhatsApp addressed the
	// message (whatsmeow does the same for its own receipts).
	type key struct{ chat, sender string }
	groups := map[key][]types.MessageID{}
	for _, msg := range msgs {
		if msg.FromMe || msg.Kind == "reaction" {
			continue
		}
		k := key{msg.RawChat, msg.RawSender}
		if k.chat == "" {
			k = key{msg.Chat, msg.SenderJID}
		}
		if policy.Classify(c) == policy.KindDirect {
			k.sender = ""
		}
		groups[k] = append(groups[k], msg.ID)
	}
	n := 0
	for k, ids := range groups {
		cj, err := types.ParseJID(k.chat)
		if err != nil {
			continue
		}
		sj := types.EmptyJID
		if k.sender != "" {
			sj, _ = types.ParseJID(k.sender)
		}
		if err := cli.MarkRead(ctx, ids, time.Now(), cj, sj); err != nil {
			return n, err
		}
		n += len(ids)
	}
	return n, nil
}

// ErrReadForbidden: the config does not allow reading this chat.
var ErrReadForbidden = errors.New("read_forbidden")
