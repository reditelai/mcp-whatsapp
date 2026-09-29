package wa

import (
	"context"
	"time"

	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
	"github.com/reditelai/mcp-whatsapp/internal/stt"
)

// Voice notes are transcribed one at a time by a single worker in the
// instance that holds the lock (it is the only one that writes app.db).

type transcriptJob struct{ chat, id string }

// backlogWindow: after the engine gets installed, or after a restart, voice
// notes from this long ago still get a transcript.
const backlogWindow = 7 * 24 * time.Hour

// Transcription returns the engine (for status and setup).
func (m *Manager) Transcription() *stt.Engine { return m.stt }

func (m *Manager) startTranscriber(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-m.jobs:
				m.transcribe(ctx, j)
			}
		}
	}()
	m.queueBacklog()
}

func (m *Manager) enqueueTranscript(chat, id string) {
	if !m.stt.Ready() {
		return
	}
	_ = m.st.SetTranscript(context.Background(), chat, id, "", "pending")
	select {
	case m.jobs <- transcriptJob{chat, id}:
	default:
		// Queue full: the note stays pending and the backlog picks it up.
		m.log.Warnf("transcription queue full, %s stays pending", id)
	}
}

// queueBacklog queues voice notes that are waiting for a transcript.
func (m *Manager) queueBacklog() {
	if !m.stt.Ready() {
		return
	}
	msgs, err := m.st.VoiceBacklog(context.Background(), time.Now().Add(-backlogWindow), cap(m.jobs))
	if err != nil {
		m.log.Warnf("transcription backlog: %v", err)
		return
	}
	for _, msg := range msgs {
		m.enqueueTranscript(msg.Chat, msg.ID)
	}
}

func (m *Manager) transcribe(ctx context.Context, j transcriptJob) {
	msg, err := m.st.GetMessage(ctx, j.chat, j.id)
	if err != nil || msg.Deleted || msg.MediaPath == "" {
		return
	}
	text, err := m.stt.Transcribe(ctx, msg.MediaPath)
	if err != nil {
		m.log.Warnf("transcription of %s: %v", j.id, err)
		_ = m.st.SetTranscript(ctx, j.chat, j.id, "", "failed: "+err.Error())
	} else if err := m.st.SetTranscript(ctx, j.chat, j.id, text, "done"); err != nil {
		m.log.Warnf("saving transcript of %s: %v", j.id, err)
	}
	// Fresh voice notes waited for their transcript before going into the
	// conversation; old ones from the backlog are not pushed.
	if time.Since(msg.Timestamp()) < pushWindow {
		m.push(ctx, j.chat, j.id)
	}
}

// TranscribeNow transcribes one voice note right away (wa_transcribe), for
// notes older than the backlog window or to retry a failed one.
func (m *Manager) TranscribeNow(ctx context.Context, chat, id string) (*appstore.Message, error) {
	if _, err := m.client(); err != nil {
		return nil, err
	}
	msg, err := m.st.GetMessage(ctx, chat, id)
	if err != nil {
		return nil, err
	}
	if msg.Kind != "voice" && msg.Kind != "audio" {
		return nil, ErrNotVoice
	}
	if msg.MediaPath == "" {
		if _, err := m.downloadMedia(ctx, chat, id); err != nil {
			return nil, err
		}
	}
	m.transcribe(ctx, transcriptJob{chat, id})
	return m.st.GetMessage(ctx, chat, id)
}

// SetupTranscription starts the engine download (wa_transcription_setup).
func (m *Manager) SetupTranscription() error {
	if _, err := m.client(); err != nil {
		return err
	}
	return m.stt.Install(m.queueBacklog)
}
