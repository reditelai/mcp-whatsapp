package wa

import (
	"context"
	"fmt"
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
	"google.golang.org/protobuf/reflect/protoreflect"

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

	// Current WhatsApp sends edits encrypted with the original message's
	// secret; whatsmeow keeps the secrets but does not decrypt by itself.
	if enc := msg.GetSecretEncryptedMessage(); enc != nil {
		m.onSecretEncrypted(ctx, cli, evt, chatKey, enc)
		return
	}

	if pm := msg.GetProtocolMessage(); pm != nil {
		target := pm.GetKey().GetID()
		// Edits arrive here too: UnwrapRaw leaves the ProtocolMessage inside
		// the edited-message wrapper.
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			if !m.mayChange(ctx, cli, evt.Info, chatKey, target, true) {
				return
			}
			path, found, err := m.st.DeleteMessage(ctx, chatKey, target)
			switch {
			case err != nil:
				m.log.Warnf("delete %s: %v", target, err)
			case !found:
				m.log.Infof("delete %s in %s: message not stored", target, chatKey)
			}
			if path != "" {
				_ = os.Remove(path)
			}
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if m.mayChange(ctx, cli, evt.Info, chatKey, target, false) {
				m.applyEdit(ctx, chatKey, target, pm.GetEditedMessage())
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
	// A fresh voice note that gets transcribed is stored as pending right
	// away, so that the waiting mode (--wait) wakes the assistant only once
	// its text is there.
	transcribe := nm.Kind == "voice" && !history && m.stt.Ready() && len(nm.MediaRef) > 0 && nm.MediaSize <= autoDownloadLimit
	if transcribe {
		nm.TranscriptStatus = "pending"
	}
	if nm.Kind == "" {
		// Key distribution and other protocol noise lands here, but so would
		// a new message type: name it in the log, never drop it silently.
		if f := setFields(msg); f != "" && !onlyNoise(f) {
			m.log.Infof("message %s in %s not stored: unsupported content (%s)", evt.Info.ID, chatKey, f)
		}
		return
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
				if transcribe {
					_ = m.st.SetTranscript(context.Background(), chatKey, nm.ID, "", "failed: download: "+err.Error())
				}
				return
			}
			if transcribe {
				m.enqueueTranscript(chatKey, nm.ID)
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
		nm.Forwarded = ci.GetIsForwarded()
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
			ci := msg.GetExtendedTextMessage().GetContextInfo()
			nm.QuotedID = ci.GetStanzaID()
			nm.Forwarded = ci.GetIsForwarded()
		}
	}
}

// applyEdit stores the new text of an edited message. An edit without text
// (media without caption) must not wipe the stored text.
func (m *Manager) applyEdit(ctx context.Context, chatKey, target string, edited *waE2E.Message) {
	t := editedText(edited)
	if t == "" {
		m.log.Infof("edit %s in %s: no text in the new content (%s)", target, chatKey, setFields(edited))
		return
	}
	found, err := m.st.EditMessage(ctx, chatKey, target, t)
	switch {
	case err != nil:
		m.log.Warnf("edit %s: %v", target, err)
	case !found:
		m.log.Infof("edit %s in %s: message not stored", target, chatKey)
	}
}

func (m *Manager) onSecretEncrypted(ctx context.Context, cli *whatsmeow.Client, evt *events.Message, chatKey string, enc *waE2E.SecretEncryptedMessage) {
	if enc.GetSecretEncType() != waE2E.SecretEncryptedMessage_MESSAGE_EDIT {
		m.log.Debugf("secret encrypted %s ignored", enc.GetSecretEncType())
		return
	}
	target := enc.GetTargetMessageKey().GetID()
	if cli == nil {
		return
	}
	dec, err := cli.DecryptSecretEncryptedMessage(ctx, evt)
	if err != nil {
		m.log.Warnf("edit %s in %s: cannot decrypt: %v", target, chatKey, err)
		return
	}
	// The plaintext is either the new content, or a protocol message
	// carrying it.
	if pm := dec.GetProtocolMessage(); pm != nil && pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		if id := pm.GetKey().GetID(); id != "" {
			target = id
		}
		dec = pm.GetEditedMessage()
	}
	if m.mayChange(ctx, cli, evt.Info, chatKey, target, false) {
		m.applyEdit(ctx, chatKey, target, dec)
	}
}

// mayChange: an edit must come from whoever sent the message, a delete for
// everyone from them or a group admin. WhatsApp itself does not stop anyone
// in a group from sending an edit for someone else's message, and a forged
// edit of the owner's message would read as the owner's instruction.
// A message that is not stored passes (the change then finds nothing).
func (m *Manager) mayChange(ctx context.Context, cli *whatsmeow.Client, info types.MessageInfo, chatKey, target string, revoke bool) bool {
	stored, err := m.st.GetMessage(ctx, chatKey, target)
	if err != nil {
		return true
	}
	if stored.FromMe || info.IsFromMe {
		if stored.FromMe == info.IsFromMe {
			return true
		}
	} else if m.sameSender(ctx, cli, info, stored) {
		return true
	}
	if revoke && info.Chat.Server == types.GroupServer && m.isGroupAdmin(ctx, cli, info.Chat, info) {
		return true
	}
	kind := "edit"
	if revoke {
		kind = "delete"
	}
	m.log.Warnf("%s of %s in %s ignored: not sent by the author of the message", kind, target, chatKey)
	return false
}

func (m *Manager) sameSender(ctx context.Context, cli *whatsmeow.Client, info types.MessageInfo, stored *appstore.Message) bool {
	who := m.resolvePerson(ctx, cli, info.Sender, info.SenderAlt)
	if who.phone != "" && stored.SenderPhone != "" {
		return "+"+who.phone == stored.SenderPhone
	}
	if who.jid.String() == stored.SenderJID {
		return true
	}
	raw, err := types.ParseJID(stored.RawSender)
	return err == nil && !raw.IsEmpty() && raw.ToNonAD() == info.Sender.ToNonAD()
}

func (m *Manager) isGroupAdmin(ctx context.Context, cli *whatsmeow.Client, group types.JID, info types.MessageInfo) bool {
	if cli == nil {
		return false
	}
	g, err := cli.GetGroupInfo(ctx, group)
	if err != nil {
		m.log.Warnf("group %s: cannot check admins: %v", group, err)
		return false
	}
	who := m.resolvePerson(ctx, cli, info.Sender, info.SenderAlt)
	sender := info.Sender.ToNonAD()
	for _, p := range g.Participants {
		if !p.IsAdmin && !p.IsSuperAdmin {
			continue
		}
		if (who.phone != "" && (p.JID.User == who.phone || p.PhoneNumber.User == who.phone)) ||
			p.JID.ToNonAD() == sender || p.LID.ToNonAD() == sender {
			return true
		}
	}
	return false
}

// onlyNoise reports whether a field list holds only protocol bookkeeping.
func onlyNoise(fields string) bool {
	for _, f := range strings.Split(fields, ",") {
		switch f {
		case "messageContextInfo", "senderKeyDistributionMessage", "fastRatchetKeySenderKeyDistributionMessage":
		default:
			return false
		}
	}
	return true
}

// setFields names the top-level fields set in a message, for logs. Never
// the content itself.
func setFields(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	var names []string
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		names = append(names, string(fd.Name()))
		return true
	})
	return strings.Join(names, ",")
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
	var since time.Time
	if d := m.cfg.HistoryDays; d > 0 {
		since = time.Now().AddDate(0, 0, -d)
	}
	for _, conv := range evt.Data.GetConversations() {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		for _, hm := range conv.GetMessages() {
			parsed, err := cli.ParseWebMessage(chatJID, hm.GetMessage())
			if err != nil || (!since.IsZero() && parsed.Info.Timestamp.Before(since)) {
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

// Forbidden in file names on Windows, macOS or Linux.
var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

// kindNames are the Czech words in media file names: the folder is for the
// user too (vstupy/whatsapp/ in Miládka's folder).
var kindNames = map[string]string{
	"image": "fotka", "video": "video", "voice": "hlasovka", "audio": "zvuk",
	"sticker": "nalepka", "video_note": "videozprava", "document": "dokument",
}

// mediaPath is <media_dir>/<chat name or number>/<date>_<time>_<kind or file name>.<ext>,
// readable by a person browsing the folder.
func (m *Manager) mediaPath(ctx context.Context, msg *appstore.Message) (string, error) {
	folder := strings.SplitN(msg.Chat, "@", 2)[0]
	if c, err := m.st.ChatByJID(ctx, msg.Chat); err == nil {
		switch {
		case c.Name != "":
			folder = c.Name
		case c.Phone != "":
			folder = "+" + c.Phone
		}
	}
	folder, err := m.st.MediaFolder(ctx, msg.Chat, safeName(folder, 60))
	if err != nil {
		return "", err
	}
	dir := filepath.Join(m.cfg.MediaDir, folder)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := msg.Timestamp().Local().Format("2006-01-02_1504")
	ext := extFor(msg.MediaMime, msg.MediaName)
	label := kindNames[msg.Kind]
	if msg.MediaName != "" {
		label = strings.TrimSuffix(msg.MediaName, filepath.Ext(msg.MediaName))
	}
	if label == "" {
		label = msg.Kind
	}
	base := stamp + "_" + safeName(label, 80)
	path := filepath.Join(dir, base+ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
		path = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
	}
}

func safeName(s string, max int) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, "_"))
	s = strings.Trim(s, ". ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	if s == "" {
		s = "_"
	}
	return s
}

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
	path, err := m.mediaPath(ctx, msg)
	if err != nil {
		return "", err
	}
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
