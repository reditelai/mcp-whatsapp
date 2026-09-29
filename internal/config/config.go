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
	DataDir     string // session.db, app.db, media/, lock
	Read        Scope
	Send        Scope
	HistorySync bool
	DeviceName  string

	Transcription Transcription
}

// Transcription of voice notes (SPEC.md, 0.2).
type Transcription struct {
	Enabled bool // default true; takes effect once wa_transcription_setup installed the engine
	Threads int  // engine threads, default 2
	Batch   int  // segments per engine run, default 2 (about 1.3 GB of memory)
}

type rawScope struct {
	Chats  json.RawMessage `json:"chats"`
	Groups json.RawMessage `json:"groups"`
	Files  []string        `json:"files"`
}

type rawConfig struct {
	DataDir     string    `json:"data_dir"`
	Read        *rawScope `json:"read"`
	Send        *rawScope `json:"send"`
	HistorySync bool      `json:"history_sync"`
	DeviceName  string    `json:"device_name"`

	Transcription *struct {
		Enabled *bool `json:"enabled"`
		Threads int   `json:"threads"`
		Batch   int   `json:"batch"`
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

	switch {
	case raw.DataDir == "":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("Nejde zjistit domovskou složku pro data serveru, nastav \"data_dir\": %v", err)
		}
		cfg.DataDir = filepath.Join(home, ".mcp-whatsapp")
	case filepath.IsAbs(raw.DataDir):
		cfg.DataDir = filepath.Clean(raw.DataDir)
	default:
		cfg.DataDir = filepath.Join(filepath.Dir(path), raw.DataDir)
	}

	var errs []string
	cfg.Transcription = Transcription{Enabled: true, Threads: 2, Batch: 2}
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
			if !filepath.IsAbs(d) {
				errs = append(errs, fmt.Sprintf("send.files: %q musí být celá cesta ke složce", d))
				continue
			}
			cfg.Send.FileDirs = append(cfg.Send.FileDirs, filepath.Clean(d))
		}
	}
	errs = checkSendWithinRead(cfg.Read, cfg.Send, errs)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s není platná konfigurace:\n  %s", path, strings.Join(errs, "\n  "))
	}
	return cfg, nil
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
