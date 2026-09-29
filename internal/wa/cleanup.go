package wa

import (
	"context"
	"os"
	"time"
)

// cleanMedia removes downloaded media older than media_keep_days, once at
// start and then daily. Only files the server itself saved (recorded in
// app.db) and only inside media_dir; the message text and the transcript
// stay. What should be kept, Miládka moves elsewhere (zdroje/) before.
func (m *Manager) cleanMedia(ctx context.Context) {
	if m.cfg.MediaKeep == 0 {
		return
	}
	for {
		m.cleanMediaOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

func (m *Manager) cleanMediaOnce(ctx context.Context) {
	before := time.Now().AddDate(0, 0, -m.cfg.MediaKeep)
	removed := 0
	// A file that cannot be removed now (open in a viewer, held by antivirus
	// or a sync client, typical on Windows) stays for the next day's run;
	// without this the same rows would come back forever.
	failed := map[string]bool{}
	for {
		msgs, err := m.st.ExpiredMedia(ctx, before, 200+len(failed))
		if err != nil {
			m.log.Warnf("media cleanup: %v", err)
			return
		}
		progress := false
		for _, msg := range msgs {
			key := msg.Chat + "/" + msg.ID
			if failed[key] {
				continue
			}
			progress = true
			if within(msg.MediaPath, m.cfg.MediaDir) || within(msg.MediaPath, m.cfg.DataDir) {
				if err := os.Remove(msg.MediaPath); err == nil {
					removed++
				} else if !os.IsNotExist(err) {
					m.log.Warnf("media cleanup %s: %v (next try tomorrow)", msg.MediaPath, err)
					failed[key] = true
					continue
				}
			}
			// Moved away by the user, or removed now: either way no longer ours.
			_ = m.st.ClearMediaPath(ctx, msg.Chat, msg.ID)
		}
		if !progress {
			break
		}
	}
	if removed > 0 {
		m.log.Infof("media cleanup: removed %d files older than %d days", removed, m.cfg.MediaKeep)
	}
}
