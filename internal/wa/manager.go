// Package wa owns the WhatsApp connection: the lock, the connection state,
// pairing, incoming messages and sending.
package wa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/reditelai/mcp-whatsapp/internal/config"
	"github.com/reditelai/mcp-whatsapp/internal/policy"
	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
	"github.com/reditelai/mcp-whatsapp/internal/stt"
)

// State of the connection, as reported by wa_status. See SPEC.md, Stav.
type State string

const (
	StateStarting     State = "starting"
	StateConnected    State = "connected"
	StateConnecting   State = "connecting"
	StateNotPaired    State = "not_paired"
	StatePairing      State = "pairing"
	StateLoggedOut    State = "logged_out"
	StateOutdated     State = "client_outdated"
	StateReplaced     State = "replaced"
	StateTempBan      State = "temporary_ban"
	StateLocked       State = "locked_by_other_instance"
	StateError        State = "error"
	lockRetryInterval       = 30 * time.Second
)

// Status is the snapshot returned by wa_status.
type Status struct {
	State       State  `json:"state"`
	Error       string `json:"error,omitempty"`
	Since       string `json:"since"`
	Phone       string `json:"phone,omitempty"`
	LastEventAt string `json:"last_event_at,omitempty"`
	LockHolder  string `json:"lock_holder_pid,omitempty"`
	DataDir     string `json:"data_dir"`
}

// Manager is the single owner of the WhatsApp client in this process.
type Manager struct {
	cfg *config.Config
	log waLog.Logger
	pol *policy.Policy
	st  *appstore.Store
	stt *stt.Engine

	jobs chan transcriptJob

	lock *flock.Flock

	pairMu sync.Mutex // one wa_pair at a time

	mu           sync.Mutex
	state        State
	errText      string
	since        time.Time
	lastEvent    time.Time
	holdsLock    bool
	container    *sqlstore.Container
	cli          *whatsmeow.Client
	pair         *pairing
	versionTried bool
	groupNames   map[string]bool
}

// NewManager prepares the data folder and app.db. It does not connect.
func NewManager(cfg *config.Config, log waLog.Logger) (*Manager, error) {
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "media"), 0o700); err != nil {
		return nil, fmt.Errorf("Datová složka %s nejde založit: %v", cfg.DataDir, err)
	}
	st, err := appstore.Open(filepath.Join(cfg.DataDir, "app.db"))
	if err != nil {
		return nil, fmt.Errorf("Databáze zpráv v %s nejde otevřít: %v", cfg.DataDir, err)
	}
	return &Manager{
		cfg:        cfg,
		log:        log,
		pol:        policy.New(cfg),
		st:         st,
		stt:        stt.New(cfg.DataDir, cfg.Transcription.Enabled, cfg.Transcription.Threads, cfg.Transcription.Batch, log.Sub("stt")),
		jobs:       make(chan transcriptJob, 256),
		lock:       flock.New(filepath.Join(cfg.DataDir, "lock")),
		state:      StateStarting,
		since:      time.Now(),
		groupNames: map[string]bool{},
	}, nil
}

// Store returns app.db (readable from any instance).
func (m *Manager) Store() *appstore.Store { return m.st }

// Policy returns the access policy.
func (m *Manager) Policy() *policy.Policy { return m.pol }

// Config returns the loaded config.
func (m *Manager) Config() *config.Config { return m.cfg }

func (m *Manager) setState(s State, errText string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != s || m.errText != errText {
		m.log.Infof("state %s -> %s %s", m.state, s, errText)
		m.state, m.errText, m.since = s, errText, time.Now()
	}
}

// Status returns the current state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{State: m.state, Error: m.errText, Since: m.since.UTC().Format(time.RFC3339), DataDir: m.cfg.DataDir}
	if !m.lastEvent.IsZero() {
		st.LastEventAt = m.lastEvent.UTC().Format(time.RFC3339)
	}
	if m.cli != nil && m.cli.Store != nil && m.cli.Store.ID != nil {
		st.Phone = "+" + m.cli.Store.ID.User
	}
	if m.state == StateLocked {
		if h, ok := m.readHolder(); ok {
			st.LockHolder = strconv.Itoa(h.PID)
		}
	}
	return st
}

// Start decides about the lock right away (so the first wa_status already
// tells), then connects in the background, or waits for the lock.
func (m *Manager) Start(ctx context.Context) {
	if m.tryLock() {
		go m.watchHandover(ctx)
		go m.startSession(ctx)
		return
	}
	handover := m.requestHandover()
	if handover {
		m.setState(StateLocked, "Spojení drží jiná verze serveru. Požádala jsem ji o předání, převezmu ho během pár sekund.")
	} else {
		m.setState(StateLocked, "Spojení s WhatsAppem drží jiná instance serveru (jiná konverzace). Čtení uložených zpráv funguje, odesílání a párování ne.")
	}
	go func() {
		started := time.Now()
		interval := lockRetryInterval
		if handover {
			interval = handoverRetry
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		warned := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if m.tryLock() {
				m.log.Infof("lock acquired, taking over the connection")
				go m.watchHandover(ctx)
				m.startSession(ctx)
				return
			}
			if handover && !warned && time.Since(started) > handoverRetryFor {
				warned = true
				pid := ""
				if h, ok := m.readHolder(); ok {
					pid = strconv.Itoa(h.PID)
				}
				m.setState(StateLocked, "Spojení drží starší instance serveru, která předání nezná (verze 0.1.0), PID "+pid+". Je potřeba ji ukončit; pak spojení převezmu sama.")
			}
		}
	}()
}

func (m *Manager) tryLock() bool {
	ok, err := m.lock.TryLock()
	if err != nil {
		m.log.Warnf("lock: %v", err)
		return false
	}
	if ok {
		m.mu.Lock()
		m.holdsLock = true
		m.mu.Unlock()
		m.writeHolder()
	}
	return ok
}

func (m *Manager) startSession(ctx context.Context) {
	container, err := sqlstore.New(ctx, "sqlite", appstore.DSN(filepath.Join(m.cfg.DataDir, "session.db")), m.log.Sub("store"))
	if err != nil {
		m.setState(StateError, "Úložiště klíčů (session.db) nejde otevřít: "+err.Error())
		return
	}
	store.SetOSInfo(m.cfg.DeviceName, [3]uint32{0, 1, 0})
	m.mu.Lock()
	m.container = container
	m.mu.Unlock()
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		m.setState(StateError, "Nejde načíst zařízení ze session.db: "+err.Error())
		return
	}
	m.newClient(device)
	m.startTranscriber(ctx)
	m.mu.Lock()
	paired := m.cli.Store.ID != nil
	m.mu.Unlock()
	if !paired {
		m.setState(StateNotPaired, "")
		return
	}
	go m.connect()
}

// newClient replaces the client with one for device, retiring the old one.
func (m *Manager) newClient(device *store.Device) {
	cli := whatsmeow.NewClient(device, m.log.Sub("client"))
	cli.EnableAutoReconnect = true
	cli.InitialAutoReconnect = true
	// whatsmeow waits AutoReconnectErrors * 2 s with no upper bound; keep it
	// at most 5 minutes (SPEC.md, Stav).
	cli.AutoReconnectHook = func(error) bool {
		if cli.AutoReconnectErrors > maxReconnectErrors {
			cli.AutoReconnectErrors = maxReconnectErrors
		}
		return true
	}
	cli.AddEventHandler(m.handleEvent)
	m.mu.Lock()
	old := m.cli
	m.cli = cli
	m.mu.Unlock()
	if old != nil {
		old.RemoveEventHandlers()
		old.Disconnect()
	}
}

const (
	maxReconnectErrors = 150 // x 2 s = 5 min
	retryMax           = 5 * time.Minute
	pairedConnectWait  = 30 * time.Second
)

// connect connects the paired client. Errors whatsmeow does not retry by
// itself are retried here with a growing delay, up to 5 minutes.
func (m *Manager) connect() {
	m.setState(StateConnecting, "")
	delay := 5 * time.Second
	for {
		m.mu.Lock()
		cli := m.cli
		m.mu.Unlock()
		if cli == nil || cli.Store.ID == nil {
			return
		}
		err := cli.Connect()
		if err == nil || errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			return
		}
		m.setState(StateConnecting, fmt.Sprintf("Připojení se nepovedlo (%v), další pokus za %s.", err, delay))
		time.Sleep(delay)
		switch m.Status().State {
		case StateConnecting:
		default:
			return // connected meanwhile, or a permanent state that must not be retried
		}
		if delay *= 2; delay > retryMax {
			delay = retryMax
		}
	}
}

// Close disconnects and releases the lock.
func (m *Manager) Close() {
	m.mu.Lock()
	cli := m.cli
	holds := m.holdsLock
	m.mu.Unlock()
	if cli != nil {
		cli.Disconnect()
	}
	if holds {
		_ = os.Remove(m.lock.Path() + ".pid")
		_ = m.lock.Unlock()
	}
	_ = m.st.Close()
}

// errors returned to tools
var (
	ErrLocked      = errors.New("locked_by_other_instance")
	ErrNotReady    = errors.New("not_connected")
	ErrAlreadyPair = errors.New("already_paired")
	ErrNotVoice    = errors.New("not_voice")
)

// client returns the client if this instance owns the connection.
func (m *Manager) client() (*whatsmeow.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.holdsLock {
		return nil, ErrLocked
	}
	if m.cli == nil {
		return nil, ErrNotReady
	}
	return m.cli, nil
}

func (m *Manager) connectedClient() (*whatsmeow.Client, error) {
	cli, err := m.client()
	if err != nil {
		return nil, err
	}
	if !cli.IsConnected() || !cli.IsLoggedIn() {
		return nil, ErrNotReady
	}
	return cli, nil
}

func (m *Manager) handleEvent(evt any) {
	m.mu.Lock()
	m.lastEvent = time.Now()
	m.mu.Unlock()
	switch e := evt.(type) {
	case *events.Connected:
		m.mu.Lock()
		m.versionTried = false
		m.mu.Unlock()
		m.setState(StateConnected, "")
	case *events.Disconnected:
		if m.paired() {
			m.setState(StateConnecting, "Spojení spadlo, připojuje se znovu.")
		}
	case *events.KeepAliveTimeout:
		if e.ErrorCount >= 3 {
			m.setState(StateConnecting, "WhatsApp neodpovídá, spojení se obnovuje.")
		}
	case *events.KeepAliveRestored:
		m.setState(StateConnected, "")
	case *events.PairSuccess:
		m.log.Infof("paired as %s", e.ID)
		m.setState(StateConnecting, "Spárováno, dokončuje se připojení.")
		// whatsmeow reconnects by itself after pairing, but only logs when
		// that fails; make sure the connection comes up.
		go func() {
			time.Sleep(pairedConnectWait)
			if m.Status().State == StateConnecting {
				m.connect()
			}
		}()
	case *events.PairError:
		m.setState(StateError, "Párování selhalo: "+e.Error.Error())
	case *events.LoggedOut:
		m.onLoggedOut()
	case *events.StreamReplaced:
		m.setState(StateReplaced, "Spojení převzal jiný program se stejným zařízením. Server se znovu nepřipojuje, dokud ho uživatel nevyřeší (jiná instance, starý server).")
	case *events.ClientOutdated:
		go m.onOutdated()
	case *events.TemporaryBan:
		m.setState(StateTempBan, "WhatsApp číslo dočasně zablokoval: "+e.String())
	case *events.ConnectFailure:
		m.setState(StateError, fmt.Sprintf("WhatsApp odmítl připojení: %v %s", e.Reason, e.Message))
	case *events.CATRefreshError:
		m.setState(StateError, "Chyba obnovy přihlášení: "+e.Error.Error())
	case *events.Message:
		m.onMessage(e, false)
	case *events.HistorySync:
		if m.cfg.HistorySync {
			go m.onHistorySync(e)
		}
	case *events.PushName:
		m.onPushName(e)
	}
}

func (m *Manager) paired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cli != nil && m.cli.Store.ID != nil
}

func (m *Manager) onLoggedOut() {
	m.setState(StateLoggedOut, "Zařízení bylo odebráno v telefonu (nebo WhatsApp přihlášení zrušil). Je potřeba znovu spárovat přes wa_pair.")
	// whatsmeow deletes the old device keys itself; start from a new device
	// right away so that wa_pair can run, without waiting for that delete.
	m.mu.Lock()
	container := m.container
	m.mu.Unlock()
	if container != nil {
		go m.newClient(container.NewDevice())
	}
}

// onOutdated tries the current WhatsApp Web version once, then gives up.
func (m *Manager) onOutdated() {
	m.mu.Lock()
	tried := m.versionTried
	m.versionTried = true
	cli := m.cli
	m.mu.Unlock()
	if !tried && cli != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		v, err := whatsmeow.GetLatestVersion(ctx, &http.Client{Timeout: 15 * time.Second})
		if err == nil && v != nil {
			m.log.Infof("client outdated, retrying with WhatsApp Web version %s", v.String())
			store.SetWAVersion(*v)
			if cli.Store.ID == nil {
				// Happened during pairing: the new version applies to the
				// next pairing attempt.
				m.setState(StateNotPaired, "WhatsApp odmítl verzi klienta, server přešel na novější. Zavolej wa_pair znovu.")
				return
			}
			// The failed connection set expectDisconnect; a fresh Connect
			// needs the old socket gone.
			cli.Disconnect()
			m.connect()
			return
		}
		m.log.Warnf("latest version lookup failed: %v", err)
	}
	m.setState(StateOutdated, "WhatsApp odmítl verzi klienta (chyba 405). Je potřeba aktualizovat server mcp-whatsapp na novější verzi.")
}

// Logout unlinks the device and deletes its keys.
func (m *Manager) Logout(ctx context.Context) error {
	cli, err := m.client()
	if err != nil {
		return err
	}
	if cli.Store.ID == nil {
		return ErrNotReady
	}
	if err := cli.Logout(ctx); err != nil {
		// Logout needs a live connection (replaced, outdated, banned...).
		// whatsmeow's advice: disconnect and delete the keys locally; the
		// user then removes the device in the phone.
		m.log.Warnf("logout over the network failed (%v), deleting keys locally", err)
		cli.Disconnect()
		if derr := cli.Store.Delete(ctx); derr != nil {
			return fmt.Errorf("logout failed: %v; local delete failed: %v", err, derr)
		}
	}
	m.mu.Lock()
	container := m.container
	m.mu.Unlock()
	m.newClient(container.NewDevice())
	m.setState(StateNotPaired, "")
	return nil
}

// Reconnect drops and reopens the connection (after a config change or when
// the user fixed a "replaced" situation).
func (m *Manager) Reconnect() error {
	cli, err := m.client()
	if err != nil {
		return err
	}
	if cli.Store.ID == nil {
		return ErrNotReady
	}
	cli.Disconnect()
	go m.connect()
	return nil
}
