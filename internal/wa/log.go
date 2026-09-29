package wa

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// Level of log output. stdout carries the MCP protocol, so logs go to stderr.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ParseLevel reads MCP_WHATSAPP_LOG ("debug", "info", "warn", "error").
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

type logger struct {
	mu     *sync.Mutex
	out    io.Writer
	module string
	min    Level
}

// NewLogger returns a whatsmeow logger writing to stderr.
func NewLogger(min Level) waLog.Logger {
	return &logger{mu: &sync.Mutex{}, out: os.Stderr, min: min}
}

func (l *logger) log(lvl Level, tag, msg string, args ...any) {
	if lvl < l.min {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s [%s %s] %s\n", time.Now().Format("15:04:05.000"), l.module, tag, fmt.Sprintf(msg, args...))
}

func (l *logger) Debugf(msg string, args ...any) { l.log(LevelDebug, "DEBUG", msg, args...) }
func (l *logger) Infof(msg string, args ...any)  { l.log(LevelInfo, "INFO", msg, args...) }
func (l *logger) Warnf(msg string, args ...any)  { l.log(LevelWarn, "WARN", msg, args...) }
func (l *logger) Errorf(msg string, args ...any) { l.log(LevelError, "ERROR", msg, args...) }

func (l *logger) Sub(module string) waLog.Logger {
	m := module
	if l.module != "" {
		m = l.module + "/" + module
	}
	return &logger{mu: l.mu, out: l.out, module: m, min: l.min}
}
