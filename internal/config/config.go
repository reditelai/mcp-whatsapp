// Package config loads and validates config.json.
//
// Errors here are read by a person in the server log when it fails to start,
// so they are in Czech (see CLAUDE.md, Konvence).
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Scope is one side of the access rules: which direct chats and which groups.
// An empty or missing scope allows nothing.
type Scope struct {
	AllChats  bool     // "chats": "all"
	Chats     []string // phone numbers in E.164 without "+" (e.g. 420777123456)
	AllGroups bool     // "groups": "all" (or true)
	Groups    []string // group JIDs, e.g. 1203630@g.us
	FileDirs  []string // send only: folders files may be sent from (absolute)
}

// Config is the validated server configuration.
type Config struct {
	Path        string // absolute path of the loaded file
	DataDir     string // session.db, app.db, lock
	MediaDir    string // downloaded photos, voice notes, documents
	MediaKeep   int    // days to keep downloaded media; 0 = forever
	Read        Scope
	Send        Scope
	HistorySync bool
	DeviceName  string

	// Owners are the phone numbers whose messages are the user's own
	// requests; everyone else's messages are data, never instructions.
	Owners []string
	// Wake: which new messages end the waiting mode (--wait) and so wake
	// the assistant: "owner" (default) or "all".
	Wake string

	Transcription Transcription
}

// Transcription of voice notes (SPEC.md, 0.2).
type Transcription struct {
	Enabled bool   // default true; takes effect once wa_transcription_setup installed the engine
	Threads int    // engine threads, default 2
	Batch   int    // segments per engine run, default 2 (about 1.3 GB of memory)
	Dir     string // engine and model; shared with other add-ons (see DefaultTranscriptionDir)
}

// addonsDirs are the names of Miládka's add-on folder (CS and EN package).
var addonsDirs = map[string]bool{".doplnky": true, ".addons": true}

// exeDir is the folder of the running binary.
func exeDir() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return filepath.Dir(p), nil
}

type rawScope struct {
	Chats  json.RawMessage `json:"chats"`
	Groups json.RawMessage `json:"groups"`
	Files  []string        `json:"files"`
}

type rawConfig struct {
	DataDir     string    `json:"data_dir"`
	MediaDir    string    `json:"media_dir"`
	MediaKeep   *int      `json:"media_keep_days"`
	Read        *rawScope `json:"read"`
	Send        *rawScope `json:"send"`
	HistorySync bool      `json:"history_sync"`
	DeviceName  string    `json:"device_name"`
	Owner       []string  `json:"owner"`
	Wake        string    `json:"wake"`

	Transcription *struct {
		Enabled *bool  `json:"enabled"`
		Threads int    `json:"threads"`
		Batch   int    `json:"batch"`
		Dir     string `json:"dir"`
	} `json:"transcription"`
}

var (
	phoneRe = regexp.MustCompile(`^\+?[0-9][0-9 ]{6,20}$`)
	groupRe = regexp.MustCompile(`^[0-9-]+@g\.us$`)
)

// Load reads and validates the config file.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("Konfigurační soubor %s nejde najít: %v", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("Konfigurační soubor %s nejde přečíst: %v", abs, err)
	}
	return Parse(data, abs)
}

// Parse validates config bytes. path is used for messages and relative paths.
func Parse(data []byte, path string) (*Config, error) {
	bin, err := exeDir()
	if err != nil {
		return nil, fmt.Errorf("Nejde zjistit složku serveru: %v", err)
	}
	return parseWith(data, path, bin)
}

// parseWith is Parse with the binary's folder given (for tests).
func parseWith(data []byte, path, bin string) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw rawConfig
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s není platná konfigurace: %v", path, err)
	}

	cfg := &Config{Path: path, HistorySync: raw.HistorySync}

	cfg.DeviceName = strings.TrimSpace(raw.DeviceName)
	if cfg.DeviceName == "" {
		cfg.DeviceName = "Miládka"
	}

	// Everything of the server lives next to its binary, so that in Miládka
	// it all stays inside her folder (<vault>/.doplnky/mcp-whatsapp/) and
	// moves with it (Karel, 29. 9. 2026: "vše mám v jedné složce").
	// Relative paths in the config are taken from the root of Miládka's
	// folder when the server lives in her add-on folder, so that the whole
	// setup moves with it; elsewhere from the config file's folder.
	base := filepath.Dir(path)
	if parent := filepath.Dir(bin); addonsDirs[filepath.Base(parent)] {
		base = filepath.Dir(parent)
	}
	cfg.DataDir = resolve(raw.DataDir, base, filepath.Join(bin, "data"))
	cfg.MediaDir = resolve(raw.MediaDir, base, filepath.Join(cfg.DataDir, "media"))
	cfg.MediaKeep = 30
	if raw.MediaKeep != nil {
		cfg.MediaKeep = *raw.MediaKeep
	}

	// The speech engine and model (about 700 MB) are shared by add-ons: in
	// the add-on folder they go to .doplnky/prepis, otherwise to data/stt.
	sttDefault := filepath.Join(cfg.DataDir, "stt")
	if parent := filepath.Dir(bin); addonsDirs[filepath.Base(parent)] {
		sttDefault = filepath.Join(parent, "prepis")
	}
	sttDir := ""
	if raw.Transcription != nil {
		sttDir = raw.Transcription.Dir
	}

	var errs []string
	cfg.Transcription = Transcription{Enabled: true, Threads: 2, Batch: 2, Dir: resolve(sttDir, base, sttDefault)}
	if cfg.MediaKeep < 0 || cfg.MediaKeep > 3650 {
		errs = append(errs, "media_keep_days: čeká počet dní 0 až 3650 (0 = nikdy nemazat)")
	}
	if t := raw.Transcription; t != nil {
		if t.Enabled != nil {
			cfg.Transcription.Enabled = *t.Enabled
		}
		if t.Threads != 0 {
			cfg.Transcription.Threads = t.Threads
		}
		if t.Batch != 0 {
			cfg.Transcription.Batch = t.Batch
		}
		if cfg.Transcription.Threads < 1 || cfg.Transcription.Threads > 16 {
			errs = append(errs, "transcription.threads: čeká číslo 1 až 16")
		}
		if cfg.Transcription.Batch < 1 || cfg.Transcription.Batch > 8 {
			errs = append(errs, "transcription.batch: čeká číslo 1 až 8")
		}
	}
	cfg.Read, errs = parseScope("read", raw.Read, errs)
	cfg.Send, errs = parseScope("send", raw.Send, errs)
	if raw.Read != nil && len(raw.Read.Files) > 0 {
		errs = append(errs, "read.files nemá význam - složky pro soubory patří do send.files")
	}
	if raw.Send != nil {
		for _, d := range raw.Send.Files {
			if strings.TrimSpace(d) == "" {
				continue
			}
			cfg.Send.FileDirs = append(cfg.Send.FileDirs, resolve(d, base, ""))
		}
	}
	errs = checkSendWithinRead(cfg.Read, cfg.Send, errs)
	for _, o := range raw.Owner {
		n, err := normalizePhone(o)
		if err != nil {
			errs = append(errs, "owner: "+err.Error())
			continue
		}
		if !cfg.Read.AllChats && !contains(cfg.Read.Chats, n) {
			errs = append(errs, fmt.Sprintf("owner +%s není v read.chats - server by jeho zprávy nečetl", n))
		}
		cfg.Owners = append(cfg.Owners, n)
	}
	cfg.Wake = "owner"
	if raw.Wake != "" {
		cfg.Wake = raw.Wake
	}
	if cfg.Wake != "owner" && cfg.Wake != "all" {
		errs = append(errs, `wake: čeká "owner" nebo "all"`)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s není platná konfigurace:\n  %s", path, strings.Join(errs, "\n  "))
	}
	return cfg, nil
}

// IsOwner reports whether phone (with or without "+") is an owner number.
func (c *Config) IsOwner(phone string) bool {
	return phone != "" && contains(c.Owners, strings.TrimPrefix(phone, "+"))
}

// resolve: empty = def, absolute as is, relative = from base.
func resolve(v, base, def string) string {
	switch {
	case v == "":
		return def
	case filepath.IsAbs(v):
		return filepath.Clean(v)
	default:
		return filepath.Join(base, filepath.FromSlash(v))
	}
}

func parseScope(name string, raw *rawScope, errs []string) (Scope, []string) {
	var s Scope
	if raw == nil {
		return s, errs
	}
	var err error
	s.AllChats, s.Chats, err = parseList(raw.Chats, normalizePhone)
	if err != nil {
		errs = append(errs, fmt.Sprintf("%s.chats: %v", name, err))
	}
	s.AllGroups, s.Groups, err = parseList(raw.Groups, normalizeGroup)
	if err != nil {
		errs = append(errs, fmt.Sprintf("%s.groups: %v", name, err))
	}
	return s, errs
}

// parseList accepts "all", true, false, null, missing, or a list of strings.
func parseList(raw json.RawMessage, norm func(string) (string, error)) (bool, []string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return false, nil, nil
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b, nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "all" {
			return true, nil, nil
		}
		return false, nil, fmt.Errorf("čeká \"all\", false nebo seznam, ne %q", s)
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return false, nil, errors.New("čeká \"all\", false nebo seznam textů")
	}
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		n, err := norm(item)
		if err != nil {
			return false, nil, err
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return false, out, nil
}

func normalizePhone(s string) (string, error) {
	t := strings.TrimSpace(s)
	if !phoneRe.MatchString(t) {
		return "", fmt.Errorf("%q není telefonní číslo v mezinárodním tvaru (třeba +420777123456)", s)
	}
	t = strings.ReplaceAll(strings.TrimPrefix(t, "+"), " ", "")
	return t, nil
}

func normalizeGroup(s string) (string, error) {
	t := strings.TrimSpace(s)
	if !groupRe.MatchString(t) {
		return "", fmt.Errorf("%q není JID skupiny (tvar 120363…@g.us, zjistí ho wa_list_chats)", s)
	}
	return t, nil
}

func checkSendWithinRead(read, send Scope, errs []string) []string {
	if send.AllChats && !read.AllChats {
		errs = append(errs, `send.chats je "all", ale read.chats ne - odesílat jde jen tam, kde server smí číst`)
	}
	if !read.AllChats {
		for _, c := range send.Chats {
			if !contains(read.Chats, c) {
				errs = append(errs, fmt.Sprintf("send.chats obsahuje +%s, které není v read.chats", c))
			}
		}
	}
	if send.AllGroups && !read.AllGroups {
		errs = append(errs, `send.groups je "all", ale read.groups ne`)
	}
	if !read.AllGroups {
		for _, g := range send.Groups {
			if !contains(read.Groups, g) {
				errs = append(errs, fmt.Sprintf("send.groups obsahuje %s, které není v read.groups", g))
			}
		}
	}
	return errs
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
