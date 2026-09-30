package wa

import (
	"reflect"

	"github.com/reditelai/mcp-whatsapp/internal/config"
	"github.com/reditelai/mcp-whatsapp/internal/policy"
)

// ReloadConfig reads config.json again and applies what can change while the
// server runs (wa_reload_config), so that a change of the settings does not
// need a new conversation - the Claude app on Windows cannot reconnect a
// server (Karel, 30. 9. 2026). Folders and the device name stay as they were
// until the next start: data, media and the speech engine must not move under
// a running server, and the device name only matters at pairing. A config
// that does not validate changes nothing.
func (m *Manager) ReloadConfig() (applied, needsRestart []string, err error) {
	old := m.conf()
	nc, err := config.Load(old.Path)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range []struct {
		name    string
		changed bool
	}{
		{"data_dir", nc.DataDir != old.DataDir},
		{"media_dir", nc.MediaDir != old.MediaDir},
		{"transcription.dir", nc.Transcription.Dir != old.Transcription.Dir},
		{"device_name", nc.DeviceName != old.DeviceName},
	} {
		if f.changed {
			needsRestart = append(needsRestart, f.name)
		}
	}
	nc.DataDir, nc.MediaDir, nc.Transcription.Dir, nc.DeviceName = old.DataDir, old.MediaDir, old.Transcription.Dir, old.DeviceName

	for _, f := range []struct {
		name string
		a, b any
	}{
		{"read", old.Read, nc.Read},
		{"send", old.Send, nc.Send},
		{"owner", old.Owners, nc.Owners},
		{"wake", []any{old.Wake, old.WakeChats, old.WakeGroups}, []any{nc.Wake, nc.WakeChats, nc.WakeGroups}},
		{"media_keep_days", old.MediaKeep, nc.MediaKeep},
		{"history_sync", []any{old.HistorySync, old.HistoryDays}, []any{nc.HistorySync, nc.HistoryDays}},
		{"transcription", old.Transcription, nc.Transcription},
	} {
		if !reflect.DeepEqual(f.a, f.b) {
			applied = append(applied, f.name)
		}
	}

	m.cfgMu.Lock()
	m.cfg, m.pol = nc, policy.New(nc)
	m.cfgMu.Unlock()
	t := nc.Transcription
	m.mu.Lock()
	holds := m.holdsLock
	m.mu.Unlock()
	// Only the instance holding the lock writes app.db (and transcribes).
	if m.stt.Configure(t.Enabled, t.Threads, t.Batch) && holds {
		m.queueBacklog()
	}
	m.log.Infof("config reloaded: applied %v, needs a restart %v", applied, needsRestart)
	return applied, needsRestart, nil
}
