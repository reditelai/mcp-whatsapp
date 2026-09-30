package wa

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mcpwhatsapp "github.com/reditelai/mcp-whatsapp"
)

// Handover lets an updated binary take the connection from the old one.
//
// Claude Code does not always stop the old server process when the server
// is re-registered or reconnected (seen at the first update, 29. 9. 2026),
// and on Windows a running binary cannot even be replaced. So the lock
// holder writes its version next to the lock, and an instance of another
// version asks it to leave: the holder disconnects, releases the lock and
// exits. Two instances of the same version are two conversations, and
// nothing is handed over between them.

// holder is what the lock holder writes to lock.pid.
type holder struct {
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

const (
	handoverPoll     = 3 * time.Second
	handoverRetry    = 2 * time.Second
	handoverRetryFor = 60 * time.Second
	handoverFileName = "handover.json"
)

func (m *Manager) holderPath() string   { return m.lock.Path() + ".pid" }
func (m *Manager) handoverPath() string { return filepath.Join(m.conf().DataDir, handoverFileName) }

// readHolder reads lock.pid. Version 0.1.0 wrote a bare PID; its version is
// then unknown ("").
func (m *Manager) readHolder() (holder, bool) {
	b, err := os.ReadFile(m.holderPath())
	if err != nil {
		return holder{}, false
	}
	var h holder
	if json.Unmarshal(b, &h) == nil && h.PID > 0 {
		return h, true
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
		return holder{PID: pid}, true
	}
	return holder{}, false
}

func (m *Manager) writeHolder() {
	b, _ := json.Marshal(holder{PID: os.Getpid(), Version: mcpwhatsapp.Version()})
	_ = os.WriteFile(m.holderPath(), b, 0o600)
}

// requestHandover asks a holder of another version to leave. It reports
// whether a request was made.
func (m *Manager) requestHandover() bool {
	h, ok := m.readHolder()
	if !ok || h.Version == mcpwhatsapp.Version() {
		return false
	}
	b, _ := json.Marshal(holder{PID: os.Getpid(), Version: mcpwhatsapp.Version()})
	if err := os.WriteFile(m.handoverPath(), b, 0o600); err != nil {
		m.log.Warnf("handover request: %v", err)
		return false
	}
	m.log.Infof("lock held by version %q (pid %d), asked it to hand over to %s", h.Version, h.PID, mcpwhatsapp.Version())
	return true
}

// watchHandover runs in the lock holder: when an instance of another
// version asks, it leaves.
func (m *Manager) watchHandover(ctx context.Context) {
	t := time.NewTicker(handoverPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b, err := os.ReadFile(m.handoverPath())
		if err != nil {
			continue
		}
		_ = os.Remove(m.handoverPath())
		var req holder
		if json.Unmarshal(b, &req) != nil || req.Version == mcpwhatsapp.Version() {
			continue
		}
		m.log.Infof("version %s (pid %d) takes over the connection, this instance (%s) exits", req.Version, req.PID, mcpwhatsapp.Version())
		m.Close()
		os.Exit(0)
	}
}
