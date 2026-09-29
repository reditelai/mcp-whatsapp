package wa

import (
	"context"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/reditelai/mcp-whatsapp/internal/policy"
	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
)

// Media larger than this is not downloaded automatically; wa_download_media
// can still fetch it on request.
const autoDownloadLimit = 64 << 20

// resolved is a chat or sender with its phone number, when known.
type resolved struct {
	jid   types.JID // canonical: phone JID for people when the number is known
	phone string    // digits only, "" for groups or unresolved LIDs
}

// resolvePerson maps a person JID (phone or LID) to its phone number. alt is
// the alternative address WhatsApp sent along with the message, if any.
func (m *Manager) resolvePerson(ctx context.Context, cli *whatsmeow.Client, jid, alt types.JID) resolved {
	jid = jid.ToNonAD()
	switch jid.Server {
	case types.DefaultUserServer:
		return resolved{jid: jid, phone: jid.User}
	case types.HiddenUserServer:
		if alt.Server == types.DefaultUserServer {
			pn := alt.ToNonAD()
			return resolved{jid: pn, phone: pn.User}
		}
		if cli != nil && cli.Store != nil && cli.Store.LIDs != nil {
			if pn, err := cli.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
				pn = pn.ToNonAD()
				return resolved{jid: pn, phone: pn.User}
			}
		}
	}
	return resolved{jid: jid}
}

// resolveChat returns the canonical chat and whether it may be read.
func (m *Manager) resolveChat(ctx context.Context, cli *whatsmeow.Client, info types.MessageInfo) (resolved, policy.Kind, bool) {
	chat := info.Chat.ToNonAD()
	kind := policy.Classify(chat)
	switch kind {
	case policy.KindGroup:
		return resolved{jid: chat}, kind, m.pol.CanRead(chat, "")
	case policy.KindDirect:
		alt := info.SenderAlt
		if info.IsFromMe {
			alt = info.RecipientAlt
		}
		r := m.resolvePerson(ctx, cli, chat, alt)
		return r, kind, m.pol.CanRead(r.jid, r.phone)
	default:
		return resolved{jid: chat}, kind, false
	}
}

func (m *Manager) onMessage(evt *events.Message, history bool) {
	if evt.Message == nil || evt.Info.Chat.IsBroadcastList() || evt.Info.Chat.Server == types.NewsletterServer {
		return
	}
	ctx := context.Background()
	m.mu.Lock()
	cli := m.cli
	m.mu.Unlock()

	chat, kind, ok := m.resolveChat(ctx, cli, evt.Info)
	if !ok {
		return
	}
	chatKey := chat.jid.String()
	msg := evt.Message

	if pm := msg.GetProtocolMessage(); pm != nil {
		target := pm.GetKey().GetID()
		// Edits arrive here too: UnwrapRaw leaves the ProtocolMessage inside
		// the edited-message wrapper.
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			path, err := m.st.DeleteMessage(ctx, chatKey, target)
			if err != nil {
				m.log.Warnf("delete %s: %v", target, err)
			}
			if path != "" {
				_ = os.Remove(path)
			}
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			// An edit without text (media without caption) must not wipe
			// the stored text.
			if t := editedText(pm.GetEditedMessage()); t != "" {
				if err := m.st.EditMessage(ctx, chatKey, target, t); err != nil {
					m.log.Warnf("edit %s: %v", target, err)
				}
			}
		}
		return
	}

	sender := m.resolvePerson(ctx, cli, evt.Info.Sender, evt.Info.SenderAlt)
	nm := appstore.NewMessage{
		Chat:        chatKey,
		ChatKind:    kindName(kind),
		ChatPhone:   chat.phone,
		ID:          evt.Info.ID,
		SenderJID:   sender.jid.String(),
		SenderPhone: sender.phone,
		SenderName:  evt.Info.PushName,
		FromMe:      evt.Info.IsFromMe,
		Time:        evt.Info.Timestamp,
		RawChat:     evt.Info.Chat.String(),
		RawSender:   evt.Info.Sender.String(),
	}
	if kind == policy.KindDirect && !evt.Info.IsFromMe {
		nm.ChatName = evt.Info.PushName
	}
	fillContent(&nm, msg)
	if nm.Kind == "" {
		return // receipts, key distribution and other protocol noise
	}
	inserted, err := m.st.SaveMessage(ctx, nm)
	if err != nil {
		m.log.Errorf("save message %s: %v", nm.ID, err)
		return
	}
	if kind == policy.KindGroup && cli != nil {
		m.fetchGroupName(cli, chat.jid)
	}
	if inserted && len(nm.MediaRef) > 0 && nm.MediaSize <= autoDownloadLimit && !history {
		go func() {
			if _, err := m.downloadMedia(context.Background(), chatKey, nm.ID); err != nil {
				m.log.Warnf("media %s: %v", nm.ID, err)
			}
		}()
	}
}

func kindName(k policy.Kind) string {
	if k == policy.KindGroup {
		return "group"
	}
	return "direct"
}

// fillContent sets kind, text and media fields from the message content.
func fillContent(nm *appstore.NewMessage, msg *waE2E.Message) {
	if r := msg.GetReactionMessage(); r != nil {
		nm.Kind, nm.Text, nm.QuotedID = "reaction", r.GetText(), r.GetKey().GetID()
		return
	}
	media := func(kind string, parent *waE2E.Message, mimeType, name, caption string, size uint64, ci *waE2E.ContextInfo) {
		nm.Kind, nm.Text, nm.MediaMime, nm.MediaName, nm.MediaSize = kind, caption, mimeType, name, int64(size)
		nm.QuotedID = ci.GetStanzaID()
		if raw, err := proto.Marshal(parent); err == nil {
			nm.MediaRef = raw
		}
	}
	switch {
	case msg.GetImageMessage() != nil:
		im := msg.GetImageMessage()
		media("image", &waE2E.Message{ImageMessage: im}, im.GetMimetype(), "", im.GetCaption(), im.GetFileLength(), im.GetContextInfo())
	case msg.GetVideoMessage() != nil:
		vm := msg.GetVideoMessage()
		media("video", &waE2E.Message{VideoMessage: vm}, vm.GetMimetype(), "", vm.GetCaption(), vm.GetFileLength(), vm.GetContextInfo())
	case msg.GetAudioMessage() != nil:
		am := msg.GetAudioMessage()
		kind := "audio"
		if am.GetPTT() {
			kind = "voice"
		}
		media(kind, &waE2E.Message{AudioMessage: am}, am.GetMimetype(), "", "", am.GetFileLength(), am.GetContextInfo())
	case msg.GetDocumentMessage() != nil:
		dm := msg.GetDocumentMessage()
		media("document", &waE2E.Message{DocumentMessage: dm}, dm.GetMimetype(), dm.GetFileName(), dm.GetCaption(), dm.GetFileLength(), dm.GetContextInfo())
	case msg.GetPtvMessage() != nil:
		vm := msg.GetPtvMessage()
		media("video_note", &waE2E.Message{PtvMessage: vm}, vm.GetMimetype(), "", "", vm.GetFileLength(), vm.GetContextInfo())
	case msg.GetStickerMessage() != nil:
		sm := msg.GetStickerMessage()
		media("sticker", &waE2E.Message{StickerMessage: sm}, sm.GetMimetype(), "", "", sm.GetFileLength(), sm.GetContextInfo())
	case msg.GetLocationMessage() != nil:
		l := msg.GetLocationMessage()
		nm.Kind = "location"
		nm.Text = strings.TrimSpace(l.GetName() + " " + l.GetAddress())
		if nm.Text == "" {
			nm.Text = formatCoords(l.GetDegreesLatitude(), l.GetDegreesLongitude())
		}
	case msg.GetLiveLocationMessage() != nil:
		l := msg.GetLiveLocationMessage()
		nm.Kind, nm.Text = "live_location", formatCoords(l.GetDegreesLatitude(), l.GetDegreesLongitude())
	case msg.GetContactMessage() != nil:
		nm.Kind, nm.Text = "contact", msg.GetContactMessage().GetDisplayName()
	case msg.GetContactsArrayMessage() != nil:
		var names []string
		for _, c := range msg.GetContactsArrayMessage().GetContacts() {
			names = append(names, c.GetDisplayName())
		}
		nm.Kind, nm.Text = "contact", strings.Join(names, ", ")
	case msg.GetPollCreationMessage() != nil:
		nm.Kind, nm.Text = "poll", msg.GetPollCreationMessage().GetName()
	case msg.GetPollCreationMessageV2() != nil:
		nm.Kind, nm.Text = "poll", msg.GetPollCreationMessageV2().GetName()
	case msg.GetPollCreationMessageV3() != nil:
		nm.Kind, nm.Text = "poll", msg.GetPollCreationMessageV3().GetName()
	case msg.GetPollCreationMessageV5() != nil:
		nm.Kind, nm.Text = "poll", msg.GetPollCreationMessageV5().GetName()
	default:
		if t := textOf(msg); t != "" {
			nm.Kind, nm.Text = "text", t
			nm.QuotedID = msg.GetExtendedTextMessage().GetContextInfo().GetStanzaID()
		}
	}
}

// editedText is the new text of an edit: plain text or a media caption.
func editedText(msg *waE2E.Message) string {
	if t := textOf(msg); t != "" {
		return t
	}
	switch {
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetCaption()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetCaption()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetCaption()
	}
	return ""
}

func textOf(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if c := msg.GetConversation(); c != "" {
		return c
	}
	return msg.GetExtendedTextMessage().GetText()
}

func formatCoords(lat, lon float64) string {
	return strconv.FormatFloat(lat, 'f', 6, 64) + ", " + strconv.FormatFloat(lon, 'f', 6, 64)
}

func (m *Manager) onHistorySync(evt *events.HistorySync) {
	m.mu.Lock()
	cli := m.cli
	m.mu.Unlock()
	if cli == nil || evt.Data == nil {
		return
	}
	for _, conv := range evt.Data.GetConversations() {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		for _, hm := range conv.GetMessages() {
			parsed, err := cli.ParseWebMessage(chatJID, hm.GetMessage())
			if err != nil {
				continue
			}
			m.onMessage(parsed, true)
		}
	}
}

func (m *Manager) onPushName(e *events.PushName) {
	if e.NewPushName == "" {
		return
	}
	// JID is usually the LID and JIDAlt the phone number; chats are stored
	// under the phone number when it is known.
	for _, j := range []types.JID{e.JID, e.JIDAlt} {
		if !j.IsEmpty() {
			_ = m.st.SetChatName(context.Background(), j.ToNonAD().String(), e.NewPushName)
		}
	}
}

func (m *Manager) fetchGroupName(cli *whatsmeow.Client, jid types.JID) {
	key := jid.String()
	m.mu.Lock()
	seen := m.groupNames[key]
	m.groupNames[key] = true
	m.mu.Unlock()
	if seen {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		info, err := cli.GetGroupInfo(ctx, jid)
		if err != nil {
			m.log.Debugf("group info %s: %v", key, err)
			return
		}
		_ = m.st.SetChatName(ctx, key, info.Name)
	}()
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// DownloadMedia returns the path of a message's media file, downloading it
// first when needed.
func (m *Manager) DownloadMedia(ctx context.Context, chat, id string) (string, error) {
	return m.downloadMedia(ctx, chat, id)
}

func (m *Manager) downloadMedia(ctx context.Context, chat, id string) (string, error) {
	msg, err := m.st.GetMessage(ctx, chat, id)
	if err != nil {
		return "", err
	}
	if msg.Deleted {
		return "", appstore.ErrNotFound
	}
	if msg.MediaPath != "" {
		if _, err := os.Stat(msg.MediaPath); err == nil {
			return msg.MediaPath, nil
		}
	}
	ref := msg.MediaRef()
	if len(ref) == 0 {
		return "", appstore.ErrNotFound
	}
	cli, err := m.connectedClient()
	if err != nil {
		return "", err
	}
	var parent waE2E.Message
	if err := proto.Unmarshal(ref, &parent); err != nil {
		return "", err
	}
	var dl whatsmeow.DownloadableMessage
	switch {
	case parent.GetImageMessage() != nil:
		dl = parent.GetImageMessage()
	case parent.GetVideoMessage() != nil:
		dl = parent.GetVideoMessage()
	case parent.GetAudioMessage() != nil:
		dl = parent.GetAudioMessage()
	case parent.GetDocumentMessage() != nil:
		dl = parent.GetDocumentMessage()
	case parent.GetStickerMessage() != nil:
		dl = parent.GetStickerMessage()
	case parent.GetPtvMessage() != nil:
		dl = parent.GetPtvMessage()
	default:
		return "", appstore.ErrNotFound
	}
	data, err := cli.Download(ctx, dl)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(m.cfg.DataDir, "media", unsafeName.ReplaceAllString(strings.SplitN(chat, "@", 2)[0], "_"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := unsafeName.ReplaceAllString(id, "_") + extFor(msg.MediaMime, msg.MediaName)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	ok, err := m.st.SetMediaPath(ctx, chat, id, path)
	if err != nil || !ok {
		// Revoked while downloading: do not keep the file.
		_ = os.Remove(path)
		if err == nil {
			err = appstore.ErrNotFound
		}
		return "", err
	}
	return path, nil
}

func extFor(mimeType, name string) string {
	if e := filepath.Ext(name); e != "" && len(e) <= 6 {
		return strings.ToLower(e)
	}
	base := strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	switch base {
	case "audio/ogg":
		return ".ogg"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	}
	if exts, _ := mime.ExtensionsByType(base); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}
