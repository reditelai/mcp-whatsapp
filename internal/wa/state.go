package wa

import (
	"context"
	"os"
	"time"

	mcpwhatsapp "github.com/reditelai/mcp-whatsapp"
	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
)

// The instance holding the lock publishes its state in app.db for the
// waiting mode (--wait), which runs as a separate process: the state when
// it changes, otherwise a heartbeat, so that a watcher can tell a quiet
// line from a server that is gone. See SPEC.md, Hlídání.

const (
	statePoll      = 2 * time.Second
	stateHeartbeat = time.Minute
	stateStopped   = "stopped"
)

func (m *Manager) publishState(ctx context.Context) {
	t := time.NewTicker(statePoll)
	defer t.Stop()
	var last appstore.ServerState
	var lastWrite time.Time
	for {
		cur := m.serverState("")
		if cur.State != last.State || cur.Error != last.Error || time.Since(lastWrite) >= stateHeartbeat {
			if err := m.st.SetServerState(ctx, cur); err != nil {
				if ctx.Err() == nil {
					m.log.Warnf("publishing state: %v", err)
				}
			} else {
				last, lastWrite = cur, time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// writeState publishes one state right away (stopped, on exit).
func (m *Manager) writeState(ctx context.Context, state string) {
	if err := m.st.SetServerState(ctx, m.serverState(state)); err != nil {
		m.log.Warnf("publishing state: %v", err)
	}
}

// serverState is the current state; override replaces the state name.
func (m *Manager) serverState(override string) appstore.ServerState {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := appstore.ServerState{State: string(m.state), Error: m.errText, Since: m.since,
		PID: os.Getpid(), Version: mcpwhatsapp.Version(), Updated: time.Now()}
	if override != "" {
		st.State, st.Error, st.Since = override, "", time.Now()
	}
	return st
}
