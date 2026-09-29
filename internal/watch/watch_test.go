package watch

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reditelai/mcp-whatsapp/internal/config"
	appstore "github.com/reditelai/mcp-whatsapp/internal/store"
)

const owner = "420777000111"

var fast = Options{Poll: 10 * time.Millisecond, VoiceWait: 300 * time.Millisecond, StaleAfter: time.Minute, ProblemAfter: 150 * time.Millisecond, MaxAge: 24 * time.Hour}

type env struct {
	t   *testing.T
	cfg *config.Config
	st  *appstore.Store
}

func newEnv(t *testing.T, extra string) *env {
	dir := t.TempDir()
	cfg, err := config.Parse([]byte(`{"read":{"chats":"all","groups":"all"},"owner":["+`+owner+`"],"data_dir":"`+filepath.ToSlash(dir)+`"`+extra+`}`), filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := appstore.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{t, cfg, st}
	e.state("connected")
	return e
}

func (e *env) state(s string) {
	if err := e.st.SetServerState(context.Background(), appstore.ServerState{State: s, Since: time.Now(), PID: 1, Version: "t", Updated: time.Now()}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) save(id, phone, kind string, fromMe bool, status string) {
	_, err := e.st.SaveMessage(context.Background(), appstore.NewMessage{Chat: phone + "@s.whatsapp.net", ChatKind: "direct", ID: id,
		SenderPhone: phone, FromMe: fromMe, Time: time.Now(), Kind: kind, Text: "x", TranscriptStatus: status})
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) cursor() int64 {
	c, err := e.st.MaxRev(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

// run starts a watcher and returns a function that waits for its result.
func (e *env) run(cursor int64, limit time.Duration) func() (int, string) {
	return e.runWith(fast, cursor, limit)
}

func (e *env) runWith(o Options, cursor int64, limit time.Duration) func() (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Run(ctx, e.cfg, cursor, o, &out) }()
	return func() (int, string) {
		defer cancel()
		code := <-done
		return code, strings.TrimSpace(out.String())
	}
}

func TestOwnerMessageWakes(t *testing.T) {
	e := newEnv(t, "")
	e.save("old", owner, "text", false, "") // before the cursor
	c := e.cursor()
	wait := e.run(c, 2*time.Second)
	time.Sleep(50 * time.Millisecond)
	e.save("mine", owner, "text", true, "")            // sent by the assistant
	e.save("other", "420600000000", "text", false, "") // someone else
	time.Sleep(50 * time.Millisecond)
	e.save("new", owner, "text", false, "")
	code, out := wait()
	if code != ExitMessages || !strings.Contains(out, "nové zprávy: 1") || !strings.Contains(out, "kurzorem") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestOthersDoNotWakeInOwnerMode(t *testing.T) {
	e := newEnv(t, "")
	wait := e.run(e.cursor(), 300*time.Millisecond)
	e.save("other", "420600000000", "text", false, "")
	e.save("mine", owner, "text", true, "")
	if code, out := wait(); code != ExitRestart {
		t.Fatalf("woke: %d %q", code, out)
	}
}

func TestAllMode(t *testing.T) {
	e := newEnv(t, `,"wake":"all"`)
	wait := e.run(e.cursor(), 2*time.Second)
	e.save("other", "420600000000", "text", false, "")
	if code, out := wait(); code != ExitMessages || !strings.Contains(out, "od majitele 0") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestVoiceWaitsForTranscript(t *testing.T) {
	e := newEnv(t, "")
	c := e.cursor()
	e.save("v", owner, "voice", false, "pending")
	slow := fast
	slow.VoiceWait = time.Minute // the transcript surely comes first
	wait := e.runWith(slow, c, 5*time.Second)
	time.Sleep(100 * time.Millisecond)
	if err := e.st.SetTranscript(context.Background(), owner+"@s.whatsapp.net", "v", "ahoj", "done"); err != nil {
		t.Fatal(err)
	}
	code, out := wait()
	if code != ExitMessages || strings.Contains(out, "bez přepisu") {
		t.Fatalf("%d %q", code, out)
	}
}

func TestVoiceTimeout(t *testing.T) {
	e := newEnv(t, "")
	c := e.cursor()
	e.save("v", owner, "voice", false, "pending")
	start := time.Now()
	code, out := e.run(c, 2*time.Second)()
	if code != ExitMessages || !strings.Contains(out, "bez přepisu 1") || time.Since(start) < fast.VoiceWait {
		t.Fatalf("%d %q after %s", code, out, time.Since(start))
	}
}

func TestDeletedDoesNotWake(t *testing.T) {
	e := newEnv(t, "")
	c := e.cursor()
	e.save("d", owner, "text", false, "")
	if _, _, err := e.st.DeleteMessage(context.Background(), owner+"@s.whatsapp.net", "d"); err != nil {
		t.Fatal(err)
	}
	if code, out := e.run(c, 300*time.Millisecond)(); code != ExitRestart {
		t.Fatalf("woke: %d %q", code, out)
	}
}

func TestProblemStates(t *testing.T) {
	e := newEnv(t, "")
	e.state("logged_out")
	code, out := e.run(e.cursor(), 2*time.Second)()
	if code != ExitProblem || !strings.Contains(out, "logged_out") {
		t.Fatalf("%d %q", code, out)
	}
	e.state("stopped")
	if code, out := e.run(e.cursor(), 2*time.Second)(); code != ExitProblem || !strings.Contains(out, "neběží") {
		t.Fatalf("%d %q", code, out)
	}
	// A short outage is not reported.
	e.state("connecting")
	if code, out := e.run(e.cursor(), 300*time.Millisecond)(); code != ExitRestart {
		t.Fatalf("connecting reported: %d %q", code, out)
	}
}

func TestNewerWatcherTakesOver(t *testing.T) {
	e := newEnv(t, "")
	first := e.run(e.cursor(), 5*time.Second)
	claim := claimPath(e.cfg.DataDir)
	for i := 0; ; i++ { // the first one must be running before the second starts
		if _, err := os.Stat(claim); err == nil {
			break
		}
		if i > 500 {
			t.Fatal("first watcher did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	second := e.run(e.cursor(), 300*time.Millisecond)
	if code, out := first(); code != ExitReplaced || !strings.Contains(out, "převzal") {
		t.Fatalf("first: %d %q", code, out)
	}
	second()
	if _, err := os.Stat(claim); !os.IsNotExist(err) {
		t.Fatalf("claim left behind: %v", err)
	}
}

func TestBadStart(t *testing.T) {
	e := newEnv(t, "")
	if code, out := e.run(e.cursor()+5, time.Second)(); code != ExitUsage || !strings.Contains(out, "za koncem") {
		t.Fatalf("cursor: %d %q", code, out)
	}
	cfg := *e.cfg
	cfg.Owners = nil
	var out bytes.Buffer
	if code := Run(context.Background(), &cfg, 0, fast, &out); code != ExitUsage || !strings.Contains(out.String(), "owner") {
		t.Fatalf("no owner: %d %q", code, out.String())
	}
}

func TestMaxRun(t *testing.T) {
	e := newEnv(t, "")
	o := fast
	o.MaxRun = 200 * time.Millisecond
	code, out := e.runWith(o, e.cursor(), 5*time.Second)()
	if code != ExitRestart || !strings.Contains(out, "vypršel") || !strings.Contains(out, "stejným kurzorem") {
		t.Fatalf("%d %q", code, out)
	}
	code, out = e.run(e.cursor(), 100*time.Millisecond)() // stopped from outside
	if code != ExitRestart || !strings.Contains(out, "zvenku") {
		t.Fatalf("stop: %d %q", code, out)
	}
}

func TestWakeList(t *testing.T) {
	e := newEnv(t, `,"wake":["+420600000000"]`)
	c := e.cursor()
	wait := e.run(c, 2*time.Second)
	e.save("x", "420555000000", "text", false, "") // not listed
	time.Sleep(50 * time.Millisecond)
	e.save("y", "420600000000", "text", false, "") // listed colleague
	if code, out := wait(); code != ExitMessages || !strings.Contains(out, "nové zprávy: 1, z toho od majitele 0") {
		t.Fatalf("%d %q", code, out)
	}
}
