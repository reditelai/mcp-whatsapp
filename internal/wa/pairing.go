package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"rsc.io/qr"
)

// pairing keeps the QR codes coming while the user gets the phone ready.
//
// WhatsApp sends about six codes over one connection (the first valid for
// 60 s, the rest for 20 s, about 160 s in total). whatsmeow disconnects when
// nobody reads them, so a goroutine drains the channel and keeps the latest
// code; wa_pair then answers instantly with whatever is current.
type pairing struct {
	mu      sync.Mutex
	started time.Time
	code    string
	expires time.Time
	done    bool   // success, timeout or error: a new call starts over
	result  string // "success", "timeout", error text
	first   chan struct{}
	once    sync.Once
	stop    chan struct{} // closed when a newer pairing takes over
}

func (p *pairing) signal() { p.once.Do(func() { close(p.first) }) }

func (p *pairing) finish(result string) {
	p.mu.Lock()
	p.done, p.result = true, result
	p.mu.Unlock()
	p.signal()
}

// PairResult is what wa_pair returns.
type PairResult struct {
	Method    string
	QRPath    string
	ExpiresAt time.Time
	Code      string // pairing code for method "code"
	PNG       []byte
}

const (
	firstCodeWait = 15 * time.Second
	phonePairWait = 20 * time.Second
	pairingWindow = 160 * time.Second // qrchan.go: 60 s + 5 x 20 s
)

// Pair returns a scannable QR code (method "qr") or a code to type into the
// phone (method "code", needs the phone number).
func (m *Manager) Pair(ctx context.Context, method, phone string) (*PairResult, error) {
	// mcp-go runs tool calls concurrently; two pairings on one client would
	// kill each other's socket.
	m.pairMu.Lock()
	defer m.pairMu.Unlock()

	cli, err := m.client()
	if err != nil {
		return nil, err
	}
	if cli.Store.ID != nil {
		return nil, ErrAlreadyPair
	}

	var digits string
	if method == "code" {
		digits = strings.TrimPrefix(strings.ReplaceAll(phone, " ", ""), "+")
		if digits == "" {
			return nil, errors.New("method \"code\" needs the phone number of the WhatsApp account, e.g. +420777123456")
		}
	}

	// A pairing code is tied to the socket it was requested on, so it always
	// gets a fresh pairing; a QR request reuses the running one.
	p := m.currentPairing()
	if method == "code" || p == nil {
		p, err = m.startPairing(cli)
		if err != nil {
			return nil, err
		}
	}
	select {
	case <-p.first:
	case <-time.After(firstCodeWait):
		return nil, errors.New("WhatsApp did not send a pairing code in time; check the internet connection and call wa_pair again")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.Lock()
	code, expires, done, result, started := p.code, p.expires, p.done, p.result, p.started
	p.mu.Unlock()
	if done {
		return nil, fmt.Errorf("pairing ended: %s; call wa_pair again", result)
	}

	if method == "code" {
		pctx, cancel := context.WithTimeout(ctx, phonePairWait)
		defer cancel()
		linkCode, err := cli.PairPhone(pctx, digits, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil {
			return nil, fmt.Errorf("pairing code request failed: %v", err)
		}
		return &PairResult{Method: "code", Code: linkCode, ExpiresAt: started.Add(pairingWindow)}, nil
	}

	c, err := qr.Encode(code, qr.M)
	if err != nil {
		return nil, fmt.Errorf("QR encode: %v", err)
	}
	c.Scale = 8
	png := c.PNG()
	path := filepath.Join(m.cfg.DataDir, "pair-qr.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		path = ""
	}
	return &PairResult{Method: "qr", QRPath: path, ExpiresAt: expires, PNG: png}, nil
}

// currentPairing returns the running pairing, if it can still hand out codes.
func (m *Manager) currentPairing() *pairing {
	m.mu.Lock()
	p := m.pair
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done || (p.code != "" && time.Now().After(p.expires)) {
		return nil
	}
	return p
}

// startPairing drops any previous pairing and opens a new QR channel.
func (m *Manager) startPairing(cli *whatsmeow.Client) (*pairing, error) {
	m.mu.Lock()
	old := m.pair
	m.pair = nil
	m.mu.Unlock()
	if old != nil {
		close(old.stop)
		old.finish("replaced by a new pairing")
	}
	// A leftover QR handler from an earlier attempt would disconnect the new
	// socket (qrchan.go), and GetQRChannel refuses a connected client.
	// The old context is deliberately not cancelled: a cancelled context
	// makes the old QR emitter call Disconnect, which could hit the new
	// socket. The expected disconnect below stops that emitter instead.
	cli.Disconnect()
	cli.RemoveEventHandlers()
	cli.AddEventHandler(m.handleEvent)

	ctx, cancel := context.WithTimeout(context.Background(), pairingWindow+30*time.Second)
	ch, err := cli.GetQRChannel(ctx)
	if err != nil {
		cancel()
		if errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
			return nil, ErrAlreadyPair
		}
		return nil, fmt.Errorf("pairing start failed: %v", err)
	}
	p := &pairing{started: time.Now(), first: make(chan struct{}), stop: make(chan struct{})}
	if err := cli.Connect(); err != nil {
		cancel()
		cli.RemoveEventHandlers()
		cli.AddEventHandler(m.handleEvent)
		return nil, fmt.Errorf("connection for pairing failed: %v", err)
	}
	m.mu.Lock()
	m.pair = p
	m.mu.Unlock()
	m.setState(StatePairing, "")
	go m.drainQR(p, ch)
	go func() { <-ctx.Done(); cancel() }()
	return p, nil
}

func (m *Manager) drainQR(p *pairing, ch <-chan whatsmeow.QRChannelItem) {
	defer p.signal()
	for {
		var item whatsmeow.QRChannelItem
		var ok bool
		select {
		case item, ok = <-ch:
		case <-p.stop:
			return
		}
		if !ok {
			p.finish("pairing channel closed")
			return
		}
		var result string
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			p.mu.Lock()
			p.code = item.Code
			p.expires = time.Now().Add(item.Timeout)
			p.mu.Unlock()
			p.signal()
			continue
		case whatsmeow.QRChannelEventPasskeyRequest, whatsmeow.QRChannelEventPasskeyResponse:
			// Passkey pairing is not supported; the QR flow goes on.
			m.log.Infof("pairing: ignoring %s", item.Event)
			continue
		case whatsmeow.QRChannelSuccess.Event:
			result = "success"
		case whatsmeow.QRChannelTimeout.Event:
			result = "timeout (all codes expired)"
		case whatsmeow.QRChannelClientOutdated.Event:
			// handleEvent gets ClientOutdated too and deals with it.
			result = "client outdated"
		case whatsmeow.QRChannelEventError:
			result = fmt.Sprintf("error: %v", item.Error)
		default:
			result = item.Event
		}
		p.finish(result)
		switch result {
		case "success", "client outdated":
		default:
			m.setState(StateNotPaired, "Párování skončilo ("+result+"). Nové spustí wa_pair.")
		}
		return
	}
}
