// Package logging writes JSON logs to stdout and, on Linux, syslog.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	levelDebug = 10
	levelInfo  = 20
	levelWarn  = 30
	levelError = 40
)

// Logger is a small structured logger. Secret-like keys are redacted.
type Logger struct {
	mu     sync.Mutex
	level  int
	out    io.Writer
	extra  io.Writer
	closer io.Closer
}

// New builds a logger. extra may be nil.
func New(out io.Writer, level string, extra io.Writer) *Logger {
	if out == nil {
		out = os.Stdout
	}
	return &Logger{level: parseLevel(level), out: out, extra: extra}
}

// Nop discards all records.
func Nop() *Logger {
	return New(io.Discard, "error", nil)
}

// Close releases the optional syslog writer.
func (l *Logger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// SetExtra attaches an additional writer, typically syslog.
func (l *Logger) SetExtra(w io.WriteCloser) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.extra = w
	l.closer = w
}

func (l *Logger) Debug(msg string, kv ...any) { l.write(levelDebug, "debug", msg, kv...) }
func (l *Logger) Info(msg string, kv ...any)  { l.write(levelInfo, "info", msg, kv...) }
func (l *Logger) Warn(msg string, kv ...any)  { l.write(levelWarn, "warn", msg, kv...) }
func (l *Logger) Error(msg string, kv ...any) { l.write(levelError, "error", msg, kv...) }

func (l *Logger) write(lv int, name, msg string, kv ...any) {
	if l == nil || lv < l.level {
		return
	}
	fields := map[string]any{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"level": name,
		"msg":   msg,
	}
	for i := 0; i+1 < len(kv); i += 2 {
		k := fmt.Sprint(kv[i])
		if redactKey(k) {
			fields[k] = "[redacted]"
			continue
		}
		fields[k] = kv[i+1]
	}
	buf, err := json.Marshal(fields)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s\n", buf)
	if l.extra != nil {
		fmt.Fprintf(l.extra, "%s\n", buf)
	}
}

func parseLevel(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return levelDebug
	case "warn", "warning":
		return levelWarn
	case "error":
		return levelError
	default:
		return levelInfo
	}
}

func redactKey(k string) bool {
	k = strings.ToLower(k)
	for _, part := range []string{"secret", "token", "password", "authorization", "api_key", "apikey", "hmac"} {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}
