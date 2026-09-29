// Package watch is the waiting mode (--wait): a small process the assistant
// runs in the background. It reads app.db and ends as soon as a message
// arrives that should wake the assistant, so waiting costs no model tokens;
// the end of the process is what wakes the conversation. It never connects
// to WhatsApp and never takes the lock. See SPEC.md, Hlídání.
//
// Its one output line is read by the model, but the words are Czech like the
// other messages of the binary a person may read in a log.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/reditelai/mcp-whatsapp/internal/config"
	"github.com/reditelai/mcp-whatsapp/internal/policy"
	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
)

// Exit codes; docs/pro-asistenta.md B7 says what the assistant does with
// each. 1 and 2 are left out: Go and the shell use them for crashes and bad
// flags, and those mean "start again" like any unknown end.
const (
	ExitMessages = 0 // messages to handle: call wa_new_messages
	ExitReplaced = 3 // a newer watcher took over, or the one who started it is gone; nothing to do
	ExitRestart  = 4 // time is up or it was stopped from outside; start it again with the same cursor
	ExitProblem  = 5 // the server is gone or WhatsApp needs the user; do not restart before it is fixed
	ExitUsage    = 6 // wrong start (config, cursor, database); the line says what
)

// Options are the timings; Defaults in production, short ones in tests.
type Options struct {
	Poll         time.Duration // how often app.db is read
	VoiceWait    time.Duration // how long a voice note may wait for its transcript
	StaleAfter   time.Duration // a server state older than this = the server is not running
	ProblemAfter time.Duration // a problem must last this long before it is reported
	MaxAge       time.Duration // older messages (history sync) do not wake
	// MaxRun ends the watcher before Claude Code stops its background
	// process (at most 2 hours), so that the end says "start again" instead
	// of looking like a stop.
	MaxRun time.Duration
}

// Defaults: reading app.db every 3 s costs nothing noticeable.
var Defaults = Options{
	Poll:         3 * time.Second,
	VoiceWait:    3 * time.Minute,
	StaleAfter:   3 * time.Minute,
	ProblemAfter: time.Minute,
	MaxAge:       24 * time.Hour,
	MaxRun:       115 * time.Minute,
}

// problemStates need the user; the others (starting, connecting, pairing,
// connected) are fine to wait through.
var problemStates = map[string]string{
	"not_paired":      "WhatsApp není spárovaný, spáruj ho přes wa_pair.",
	"logged_out":      "Zařízení bylo odebráno v telefonu, je potřeba znovu spárovat přes wa_pair.",
	"client_outdated": "WhatsApp odmítl verzi klienta, je potřeba aktualizovat mcp-whatsapp.",
	"temporary_ban":   "WhatsApp číslo dočasně zablokoval.",
	"replaced":        "Spojení převzal jiný program se stejným zařízením.",
	"error":           "Chyba spojení, podrobnosti ve wa_status.",
}

type msgKey struct{ chat, id string }

// pendingNote is a voice note waiting for its transcript.
type pendingNote struct {
	first time.Time
	owner bool
}

// Run waits and returns the exit code; the one line for the model goes to out.
func Run(ctx context.Context, cfg *config.Config, cursor int64, o Options, out io.Writer) int {
	say := func(code int, format string, a ...any) int {
		fmt.Fprintf(out, format+"\n", a...)
		return code
	}
	if cfg.Wake == "owner" && len(cfg.Owners) == 0 {
		return say(ExitUsage, "chyba: v configu chybí owner, hlídač by nikdy nic neohlásil")
	}
	dbPath := filepath.Join(cfg.DataDir, "app.db")
	st, err := appstore.OpenReader(dbPath)
	if err != nil {
		return say(ExitUsage, "chyba: databáze zpráv %s nejde otevřít (server ještě neběžel?): %v", dbPath, err)
	}
	defer st.Close()
	if max, err := st.MaxRev(ctx); err != nil {
		return say(ExitUsage, "chyba: databáze zpráv nejde číst: %v", err)
	} else if cursor > max {
		return say(ExitUsage, "chyba: kurzor %d je za koncem zpráv (%d), vezmi aktuální z wa_new_messages", cursor, max)
	}

	w := &watcher{cfg: cfg, pol: policy.New(cfg), st: st, o: o, scanned: cursor, pending: map[msgKey]pendingNote{}}
	claim := claimPath(cfg.DataDir)
	token, err := writeClaim(claim)
	if err != nil {
		return say(ExitUsage, "chyba: nejde zapsat %s: %v", claim, err)
	}
	defer func() {
		if b, err := os.ReadFile(claim); err == nil && string(b) == token {
			_ = os.Remove(claim)
		}
	}()
	ppid := os.Getppid()
	again := "Spusť hlídače znovu se stejným kurzorem " + strconv.FormatInt(cursor, 10) + "."
	started := time.Now()

	t := time.NewTicker(o.Poll)
	defer t.Stop()
	var problemSince time.Time
	for {
		// A newer watcher took over (the assistant started another one), or
		// the process that started this one is gone (Unix; an orphan must not
		// wait on).
		if b, err := os.ReadFile(claim); err == nil && string(b) != token {
			return say(ExitReplaced, "konec: hlídání převzal novější hlídač")
		}
		// Windows keeps the dead parent's PID and a failed lookup returns -1,
		// so the check would only ever misfire there.
		if runtime.GOOS != "windows" && ppid != 1 && os.Getppid() != ppid {
			return say(ExitReplaced, "konec: proces, který hlídače spustil, skončil")
		}

		now := time.Now()
		n, owner, untranscribed, err := w.scan(ctx, now)
		if ctx.Err() != nil {
			return say(ExitRestart, "konec: hlídač byl zastaven zvenku. %s", again)
		}
		if err == nil && n > 0 {
			line := fmt.Sprintf("nové zprávy: %d", n)
			if cfg.Wake != "owner" {
				line += fmt.Sprintf(", z toho od majitele %d", owner)
			}
			if untranscribed > 0 {
				line += fmt.Sprintf(", hlasovky zatím bez přepisu %d", untranscribed)
			}
			return say(ExitMessages, "%s. Zavolej wa_new_messages s kurzorem %s.", line, strconv.FormatInt(cursor, 10))
		}

		// A read error is a problem like the others: reported only when it
		// lasts, a moment of a busy database must not wake the assistant.
		var p string
		if err != nil {
			p = "databáze zpráv nejde číst: " + err.Error()
		} else {
			p = w.problem(ctx, now)
		}
		if ctx.Err() != nil {
			return say(ExitRestart, "konec: hlídač byl zastaven zvenku. %s", again)
		}
		if p == "" {
			problemSince = time.Time{}
		} else if problemSince.IsZero() {
			problemSince = now
		} else if now.Sub(problemSince) >= o.ProblemAfter {
			return say(ExitProblem, "problém: %s", p)
		}

		if o.MaxRun > 0 && time.Since(started) >= o.MaxRun {
			return say(ExitRestart, "konec: vypršel čas hlídání. %s", again)
		}

		select {
		case <-ctx.Done():
			return say(ExitRestart, "konec: hlídač byl zastaven zvenku. %s", again)
		case <-t.C:
		}
	}
}

type watcher struct {
	cfg     *config.Config
	pol     *policy.Policy
	st      *appstore.Store
	o       Options
	scanned int64                  // change log read up to here
	pending map[msgKey]pendingNote // voice notes waiting for a transcript
}

// scan reads the changes since the last scan and counts what wakes.
func (w *watcher) scan(ctx context.Context, now time.Time) (n, owner, untranscribed int, err error) {
	msgs, err := w.st.After(ctx, w.scanned, 500)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, m := range msgs {
		w.scanned = m.Rev
		k := msgKey{m.Chat, m.ID}
		delete(w.pending, k) // a newer revision replaces what was known
		if !w.wakes(&m, now) {
			continue
		}
		isOwner := w.cfg.IsOwner(m.SenderPhone)
		if m.Kind == "voice" && m.TranscriptStatus == "pending" {
			w.pending[k] = pendingNote{first: now, owner: isOwner}
			continue
		}
		n++
		if isOwner {
			owner++
		}
	}
	// A transcript that takes too long does not hold the message back.
	for _, p := range w.pending {
		if now.Sub(p.first) >= w.o.VoiceWait {
			n++
			untranscribed++
			if p.owner {
				owner++
			}
		}
	}
	return n, owner, untranscribed, nil
}

// wakes: someone else's new or edited message in a readable chat that the
// config's wake allows (config.Wakes). Own messages (including what the
// assistant sent), deletions and reactions never wake.
func (w *watcher) wakes(m *appstore.Message, now time.Time) bool {
	if m.FromMe || m.Deleted || m.Kind == "reaction" {
		return false
	}
	if now.Sub(m.Timestamp()) > w.o.MaxAge {
		return false
	}
	jid, err := types.ParseJID(m.Chat)
	if err != nil || !w.pol.CanRead(jid, "") {
		return false
	}
	return w.cfg.Wakes(m.SenderPhone, m.Chat)
}

// problem describes what keeps messages from coming, or "".
func (w *watcher) problem(ctx context.Context, now time.Time) string {
	s, err := w.st.GetServerState(ctx)
	switch {
	case errors.Is(err, appstore.ErrNotFound):
		return "server mcp-whatsapp neběží (ještě se nespustil). V Claude Code zkontroluj /mcp."
	case err != nil:
		return "stav serveru nejde přečíst: " + err.Error()
	case s.State == "stopped" || now.Sub(s.Updated) > w.o.StaleAfter:
		return "server mcp-whatsapp neběží (Claude Code ho nespustil, nebo spadl). V Claude Code zkontroluj /mcp."
	}
	if text, bad := problemStates[s.State]; bad {
		if s.Error != "" {
			text = s.Error
		}
		return s.State + ": " + strings.TrimSpace(text)
	}
	return ""
}

func claimPath(dataDir string) string { return filepath.Join(dataDir, "wait.owner") }

// writeClaim marks this watcher as the current one; an older one sees
// another token and ends.
func writeClaim(path string) (string, error) {
	token := fmt.Sprintf("%d %d", os.Getpid(), time.Now().UnixNano())
	return token, os.WriteFile(path, []byte(token), 0o600)
}
