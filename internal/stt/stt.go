// Package stt transcribes voice notes locally with sherpa-onnx and the
// Parakeet TDT 0.6B v3 model (Czech about as accurate as Whisper large-v3,
// several times faster on a CPU). The engine is an upstream prebuilt binary
// run as a subprocess, so this server stays pure Go.
//
// Measured on derfl-srv1 (2 vCPU) 29. 9. 2026: 13.6 s of Czech speech in
// 3.2 s plus 3.7 s model load, 1.1 GB of memory for one segment and about
// 0.23 GB more for every further segment in the same run.
package stt

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/reditelai/mcp-whatsapp/internal/audio"
)

type asset struct {
	url    string
	sha256 string
}

const engineVersion = "1.13.8"

// Upstream release assets, pinned with their SHA-256 (GitHub release
// digests, checked 29. 9. 2026). Windows uses the MT build: it carries its
// own C runtime, so no Visual C++ redistributable is needed.
var engines = map[string]asset{
	"linux/amd64":   {"sherpa-onnx-v1.13.8-linux-x64-shared-no-tts.tar.bz2", "d0f96c8b65c6cd0974fada22737e337de81bc8cd2abbec2e39caf358b1eec5fc"},
	"linux/arm64":   {"sherpa-onnx-v1.13.8-linux-aarch64-shared-cpu.tar.bz2", "4e3734f82bc1379fd91f219f5869c7e9d03b7a4f7561907d8abca4849c51a789"},
	"darwin/arm64":  {"sherpa-onnx-v1.13.8-osx-arm64-shared-no-tts.tar.bz2", "91b96512c4fa1960f8a9ed5360a6c8dda53a4b5015d0590244f14086a234557a"},
	"darwin/amd64":  {"sherpa-onnx-v1.13.8-osx-x64-shared-no-tts.tar.bz2", "03fd4cffd98b239d74b9253c270ff637661adb4d51a5a6c9e1f7486e48306db3"},
	"windows/amd64": {"sherpa-onnx-v1.13.8-win-x64-shared-MT-Release-no-tts.tar.bz2", "4b0a94f7b5c606b1b64a19a831c2127559e4b3d34e195465ebc7be73d9ed4783"},
	"windows/arm64": {"sherpa-onnx-v1.13.8-win-arm64-shared-MT-Release-no-tts.tar.bz2", "29a864324e658bef2a8b83bd3e12adae8b415a5a232d83902030e8c6efa37dbc"},
}

const (
	engineBase = "https://github.com/k2-fsa/sherpa-onnx/releases/download/v" + engineVersion + "/"
	modelName  = "sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8"
)

var model = asset{"https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/" + modelName + ".tar.bz2",
	"5793d0fd397c5778d2cf2126994d58e9d56b1be7c04d13c7a15bb1b4eafb16bf"}

// modelFiles are the files the engine needs from the model archive.
var modelFiles = []string{"encoder.int8.onnx", "decoder.int8.onnx", "joiner.int8.onnx", "tokens.txt"}

// Segmenting: short pieces transcribe better and with less memory than one
// long file (a two-minute note in one pass came out visibly worse).
const (
	segmentTarget = 15.0
	segmentMax    = 25.0
)

// State of the transcription engine.
type State string

const (
	StateOff          State = "off"           // turned off in config
	StateUnsupported  State = "unsupported"   // no engine for this system
	StateNotInstalled State = "not_installed" // wa_transcription_setup downloads it
	StateInstalling   State = "installing"
	StateReady        State = "ready"
	StateError        State = "error"
)

// Status is reported in wa_status.
type Status struct {
	State    State  `json:"state"`
	Error    string `json:"error,omitempty"`
	Progress string `json:"progress,omitempty"`
	Model    string `json:"model"`
	Download string `json:"download_size,omitempty"`
}

// Engine runs transcriptions, one at a time.
type Engine struct {
	dir     string // shared add-on folder (.doplnky/prepis) or <data>/stt
	threads int
	batch   int
	log     waLog.Logger

	mu       sync.Mutex
	state    State
	errText  string
	progress string

	run sync.Mutex // one transcription at a time: memory is the limit
}

// New prepares the engine in dir; it does not download anything.
func New(dir string, enabled bool, threads, batch int, log waLog.Logger) *Engine {
	e := &Engine{dir: dir, threads: threads, batch: batch, log: log}
	switch {
	case !enabled:
		e.state = StateOff
	case engines[platform()].url == "":
		e.state = StateUnsupported
	case e.installed():
		e.state = StateReady
	default:
		e.state = StateNotInstalled
	}
	return e
}

func platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

func (e *Engine) engineDir() string { return filepath.Join(e.dir, "engine-"+engineVersion) }
func (e *Engine) modelDir() string  { return filepath.Join(e.dir, modelName) }

func (e *Engine) exe() string {
	name := "sherpa-onnx-offline"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(e.engineDir(), "bin", name)
}

func (e *Engine) installed() bool {
	for _, p := range []string{filepath.Join(e.engineDir(), ".ready"), filepath.Join(e.modelDir(), ".ready")} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// Status returns the current state.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := Status{State: e.state, Error: e.errText, Progress: e.progress, Model: "Parakeet TDT 0.6B v3"}
	if e.state == StateNotInstalled {
		st.Download = "about 510 MB (engine about 20 MB, model 487 MB), about 700 MB on disk"
	}
	return st
}

// Ready reports whether transcriptions can run.
func (e *Engine) Ready() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state == StateReady
}

func (e *Engine) set(s State, errText, progress string) {
	e.mu.Lock()
	e.state, e.errText, e.progress = s, errText, progress
	e.mu.Unlock()
}

// Install downloads engine and model in the background. It returns at once;
// progress is in Status.
func (e *Engine) Install(onReady func()) error {
	e.mu.Lock()
	switch e.state {
	case StateOff:
		e.mu.Unlock()
		return errors.New("transcription is turned off in config.json (transcription.enabled)")
	case StateUnsupported:
		e.mu.Unlock()
		return fmt.Errorf("no transcription engine for %s", platform())
	case StateReady, StateInstalling:
		e.mu.Unlock()
		return nil
	}
	e.state, e.errText, e.progress = StateInstalling, "", "starting"
	e.mu.Unlock()
	go func() {
		if err := e.install(); err != nil {
			e.log.Errorf("transcription install: %v", err)
			e.set(StateError, err.Error(), "")
			return
		}
		e.set(StateReady, "", "")
		e.log.Infof("transcription engine ready")
		if onReady != nil {
			onReady()
		}
	}()
	return nil
}

func (e *Engine) install() error {
	if err := os.MkdirAll(e.dir, 0o700); err != nil {
		return err
	}
	eng := engines[platform()]
	if _, err := os.Stat(filepath.Join(e.engineDir(), ".ready")); err != nil {
		prefix := strings.TrimSuffix(eng.url, ".tar.bz2") + "/"
		keep := func(name string) (string, bool) {
			rel, ok := strings.CutPrefix(name, prefix)
			if !ok {
				return "", false
			}
			base := path.Base(rel)
			switch {
			case strings.HasPrefix(rel, "bin/") && (base == "sherpa-onnx-offline" || base == "sherpa-onnx-offline.exe" || strings.HasSuffix(base, ".dll")):
				return rel, true
			case strings.HasPrefix(rel, "lib/") && (strings.Contains(base, ".so") || strings.HasSuffix(base, ".dylib") || strings.HasSuffix(base, ".dll")):
				return rel, true
			}
			return "", false
		}
		if err := e.fetch("engine", engineBase+eng.url, eng.sha256, e.engineDir(), keep); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(e.modelDir(), ".ready")); err != nil {
		keep := func(name string) (string, bool) {
			base := path.Base(name)
			for _, f := range modelFiles {
				if base == f && path.Dir(name) == modelName {
					return f, true
				}
			}
			return "", false
		}
		if err := e.fetch("model", model.url, model.sha256, e.modelDir(), keep); err != nil {
			return err
		}
	}
	return nil
}

// fetch downloads a .tar.bz2, checks its SHA-256 before anything of it is
// used, and extracts the files keep selects into dir.
func (e *Engine) fetch(what, url, sum, dir string, keep func(string) (string, bool)) error {
	tmp := dir + ".download"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	resp, err := (&http.Client{Timeout: 60 * time.Minute}).Get(url)
	if err != nil {
		f.Close()
		return fmt.Errorf("%s download: %v", what, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		f.Close()
		return fmt.Errorf("%s download: HTTP %d", what, resp.StatusCode)
	}
	h := sha256.New()
	pr := &progressReader{r: resp.Body, total: resp.ContentLength, report: func(done, total int64) {
		e.set(StateInstalling, "", fmt.Sprintf("%s %d/%d MB", what, done>>20, total>>20))
	}}
	if _, err := io.Copy(io.MultiWriter(f, h), pr); err != nil {
		f.Close()
		return fmt.Errorf("%s download: %v", what, err)
	}
	f.Close()
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("%s download: checksum mismatch (got %s), nothing was installed", what, got)
	}

	e.set(StateInstalling, "", what+" extracting")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	in, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer in.Close()
	tr := tar.NewReader(bzip2.NewReader(bufio.NewReader(in)))
	n := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%s extract: %v", what, err)
		}
		rel, ok := keep(hdr.Name)
		if !ok || strings.Contains(rel, "..") {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink:
			// Shared libraries are linked by version on Linux and macOS.
			if strings.Contains(hdr.Linkname, "/") {
				continue
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		default:
			continue
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("%s extract: no expected files in the archive", what)
	}
	return os.WriteFile(filepath.Join(dir, ".ready"), []byte(url+"\n"), 0o600)
}

type progressReader struct {
	r      io.Reader
	done   int64
	total  int64
	last   time.Time
	report func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if time.Since(p.last) > time.Second {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return n, err
}

// Transcribe returns the text of an Ogg/Opus voice note.
func (e *Engine) Transcribe(ctx context.Context, oggPath string) (string, error) {
	if !e.Ready() {
		return "", fmt.Errorf("transcription engine is %s", e.Status().State)
	}
	e.run.Lock()
	defer e.run.Unlock()

	in, err := os.Open(oggPath)
	if err != nil {
		return "", err
	}
	pcm, err := audio.OggOpusToPCM(in)
	in.Close()
	if err != nil {
		return "", err
	}
	if len(pcm) < audio.SampleRate/4 {
		return "", nil // under 250 ms: nothing to transcribe
	}
	tmp, err := os.MkdirTemp(e.dir, "run-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	var files []string
	for i, seg := range audio.Segment(pcm, segmentTarget, segmentMax) {
		p := filepath.Join(tmp, fmt.Sprintf("%03d.wav", i))
		f, err := os.Create(p)
		if err != nil {
			return "", err
		}
		err = audio.WriteWAV(f, seg)
		f.Close()
		if err != nil {
			return "", err
		}
		files = append(files, p)
	}

	var parts []string
	for start := 0; start < len(files); start += e.batch {
		end := start + e.batch
		if end > len(files) {
			end = len(files)
		}
		texts, err := e.runEngine(ctx, files[start:end])
		if err != nil {
			return "", err
		}
		parts = append(parts, texts...)
	}
	return strings.TrimSpace(strings.Join(parts, " ")), nil
}

func (e *Engine) runEngine(ctx context.Context, wavs []string) ([]string, error) {
	m := e.modelDir()
	args := []string{
		fmt.Sprintf("--num-threads=%d", e.threads),
		"--encoder=" + filepath.Join(m, "encoder.int8.onnx"),
		"--decoder=" + filepath.Join(m, "decoder.int8.onnx"),
		"--joiner=" + filepath.Join(m, "joiner.int8.onnx"),
		"--tokens=" + filepath.Join(m, "tokens.txt"),
		"--model-type=nemo_transducer",
	}
	cmd := exec.CommandContext(ctx, e.exe(), append(args, wavs...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		tail := stderr.String()
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		return nil, fmt.Errorf("transcription engine failed: %v: %s", err, strings.TrimSpace(tail))
	}
	// One JSON line per input file, in order.
	var texts []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var r struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(line), &r) == nil {
			texts = append(texts, strings.TrimSpace(r.Text))
		}
	}
	if len(texts) != len(wavs) {
		return nil, fmt.Errorf("transcription engine returned %d results for %d segments", len(texts), len(wavs))
	}
	return texts, nil
}
