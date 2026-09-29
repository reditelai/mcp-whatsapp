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
	"strings"
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

	lock *flock.Flock

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
		if b, err := os.ReadFile(m.lock.Path() + ".pid"); err == nil {
			st.LockHolder = strings.TrimSpace(string(b))
		}
	}
	return st
}

// Start takes the lock and connects, or waits for the lock in the background.
func (m *Manager) Start(ctx context.Context) {
	if m.tryLock() {
		m.startSession(ctx)
		return
	}
	m.setState(StateLocked, "Spojení s WhatsAppem drží jiná instance serveru (jiná konverzace). Čtení uložených zpráv funguje, odesílání a párování ne.")
	go func() {
		t := time.NewTicker(lockRetryInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if m.tryLock() {
					m.log.Infof("lock acquired, taking over the connection")
					m.startSession(ctx)
					return
				}
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
		_ = os.WriteFile(m.lock.Path()+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
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
	if err := m.newClient(ctx); err != nil {
		m.setState(StateError, err.Error())
		return
	}
	m.mu.Lock()
	paired := m.cli.Store.ID != nil
	m.mu.Unlock()
	if !paired {
		m.setState(StateNotPaired, "")
		return
	}
	m.connect()
}

// newClient builds a client for the first stored device, or a fresh device.
func (m *Manager) newClient(ctx context.Context) error {
	device, err := m.container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("nejde načíst zařízení ze session.db: %v", err)
	}
	cli := whatsmeow.NewClient(device, m.log.Sub("client"))
	cli.EnableAutoReconnect = true
	cli.InitialAutoReconnect = true
	cli.AddEventHandler(m.handleEvent)
	m.mu.Lock()
	m.cli = cli
	m.mu.Unlock()
	return nil
}

func (m *Manager) connect() {
	m.setState(StateConnecting, "")
	m.mu.Lock()
	cli := m.cli
	m.mu.Unlock()
	if err := cli.Connect(); err != nil {
		m.setState(StateConnecting, "Připojení se nepovedlo, zkouší se znovu: "+err.Error())
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
		m.setState(StateConnecting, "Spojení spadlo, připojuje se znovu.")
	case *events.KeepAliveTimeout:
		if e.ErrorCount >= 3 {
			m.setState(StateConnecting, "WhatsApp neodpovídá, spojení se obnovuje.")
		}
	case *events.KeepAliveRestored:
		m.setState(StateConnected, "")
	case *events.PairSuccess:
		m.log.Infof("paired as %s", e.ID)
		m.setState(StateConnecting, "Spárováno, dokončuje se připojení.")
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

func (m *Manager) onLoggedOut() {
	m.setState(StateLoggedOut, "Zařízení bylo odebráno v telefonu (nebo WhatsApp přihlášení zrušil). Je potřeba znovu spárovat přes wa_pair.")
	// whatsmeow has already deleted the device keys; prepare a fresh device
	// so that wa_pair can start right away.
	go func() {
		time.Sleep(time.Second)
		if err := m.newClient(context.Background()); err != nil {
			m.log.Errorf("new device after logout: %v", err)
		}
	}()
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
		return err
	}
	if err := m.newClient(ctx); err != nil {
		return err
	}
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
	m.connect()
	return nil
}
