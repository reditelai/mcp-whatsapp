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
// 60 s, the rest for 20 s). whatsmeow disconnects when nobody reads them,
// so a goroutine drains the channel and keeps the latest code; wa_pair then
// answers instantly with whatever is current.
type pairing struct {
	mu      sync.Mutex
	code    string
	expires time.Time
	done    bool   // success, timeout or error: a new call starts over
	result  string // "success", "timeout", error text
	first   chan struct{}
	cancel  context.CancelFunc
}

// PairResult is what wa_pair returns.
type PairResult struct {
	Method    string    `json:"method"`
	QRPath    string    `json:"qr_path,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	Code      string    `json:"code,omitempty"` // pairing code for method "code"
	PNG       []byte    `json:"-"`
}

const (
	firstCodeWait = 15 * time.Second
	phonePairWait = 20 * time.Second
)

// Pair returns a scannable QR code (method "qr") or a code to type into the
// phone (method "code", needs the phone number).
func (m *Manager) Pair(ctx context.Context, method, phone string) (*PairResult, error) {
	cli, err := m.client()
	if err != nil {
		return nil, err
	}
	if cli.Store.ID != nil {
		return nil, ErrAlreadyPair
	}
	p, err := m.ensurePairing(cli)
	if err != nil {
		return nil, err
	}
	select {
	case <-p.first:
	case <-time.After(firstCodeWait):
		return nil, errors.New("WhatsApp did not send a pairing code in time; check the internet connection and call wa_pair again")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.Lock()
	code, expires, done, result := p.code, p.expires, p.done, p.result
	p.mu.Unlock()
	if done && result != "success" {
		return nil, fmt.Errorf("pairing ended: %s; call wa_pair again", result)
	}

	if method == "code" {
		digits := strings.TrimPrefix(strings.ReplaceAll(phone, " ", ""), "+")
		if digits == "" {
			return nil, errors.New("method \"code\" needs the phone number of the WhatsApp account, e.g. +420777123456")
		}
		pctx, cancel := context.WithTimeout(ctx, phonePairWait)
		defer cancel()
		linkCode, err := cli.PairPhone(pctx, digits, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil {
			return nil, fmt.Errorf("pairing code request failed: %v", err)
		}
		return &PairResult{Method: "code", Code: linkCode, ExpiresAt: time.Now().Add(2 * time.Minute)}, nil
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

// ensurePairing returns the running pairing or starts a new one.
func (m *Manager) ensurePairing(cli *whatsmeow.Client) (*pairing, error) {
	m.mu.Lock()
	p := m.pair
	m.mu.Unlock()
	if p != nil {
		p.mu.Lock()
		alive := !p.done && (p.code == "" || time.Now().Before(p.expires))
		p.mu.Unlock()
		if alive {
			return p, nil
		}
		p.cancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	ch, err := cli.GetQRChannel(ctx)
	if err != nil {
		cancel()
		if errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
			return nil, ErrAlreadyPair
		}
		return nil, fmt.Errorf("pairing start failed: %v", err)
	}
	p = &pairing{first: make(chan struct{}), cancel: cancel}
	m.mu.Lock()
	m.pair = p
	m.mu.Unlock()

	if !cli.IsConnected() {
		if err := cli.Connect(); err != nil {
			cancel()
			return nil, fmt.Errorf("connection for pairing failed: %v", err)
		}
	}
	m.setState(StatePairing, "")
	go m.drainQR(p, ch)
	return p, nil
}

func (m *Manager) drainQR(p *pairing, ch <-chan whatsmeow.QRChannelItem) {
	var once sync.Once
	signal := func() { once.Do(func() { close(p.first) }) }
	defer signal()
	for item := range ch {
		p.mu.Lock()
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			p.code = item.Code
			p.expires = time.Now().Add(item.Timeout)
			p.mu.Unlock()
			signal()
			continue
		case whatsmeow.QRChannelSuccess.Event:
			p.done, p.result = true, "success"
		case whatsmeow.QRChannelTimeout.Event:
			p.done, p.result = true, "timeout (all codes expired)"
		case whatsmeow.QRChannelClientOutdated.Event:
			p.done, p.result = true, "client outdated"
		case whatsmeow.QRChannelEventError:
			p.done, p.result = true, fmt.Sprintf("error: %v", item.Error)
		case whatsmeow.QRChannelEventPasskeyRequest, whatsmeow.QRChannelEventPasskeyResponse:
			// Passkey pairing is not supported; the QR flow goes on.
			p.mu.Unlock()
			m.log.Infof("pairing: ignoring %s", item.Event)
			continue
		default:
			p.done, p.result = true, item.Event
		}
		result := p.result
		p.mu.Unlock()
		signal()
		switch result {
		case "success":
			// PairSuccess / Connected events set the state.
		case "client outdated":
			go m.onOutdated()
		default:
			m.setState(StateNotPaired, "Párování skončilo ("+result+"). Nové spustí wa_pair.")
		}
		return
	}
}
