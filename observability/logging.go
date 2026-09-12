// SPDX-License-Identifier: BUSL-1.1

package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/koten-ai/zeus_client_golang/security"
)

// Four family levels (LOGGING.md §3). No WARN / FATAL.
const (
	LevelError slog.Level = slog.LevelError
	LevelInfo  slog.Level = slog.LevelInfo
	LevelDebug slog.Level = slog.LevelDebug
	LevelTrace slog.Level = slog.LevelDebug - 4 // below DEBUG (Python TRACE=5)
)

var familyLevels = map[string]slog.Level{
	"error": LevelError,
	"info":  LevelInfo,
	"debug": LevelDebug,
	"trace": LevelTrace,
}

func parseLevel(level string) slog.Level {
	if lv, ok := familyLevels[strings.ToLower(strings.TrimSpace(level))]; ok {
		return lv
	}
	return LevelInfo
}

func levelName(lv slog.Level) string {
	switch {
	case lv >= LevelError:
		return "ERROR"
	case lv >= LevelInfo:
		return "INFO"
	case lv >= LevelDebug:
		return "DEBUG"
	default:
		return "TRACE"
	}
}

// LogRecord is one captured family event (Python CaptureLogHandler sink item).
type LogRecord struct {
	Level string
	Event string
	Attrs map[string]any
}

// FamilyLogger emits stable dotted events with structured primitive attrs.
// REDACT default true; secrets stay denied when redact is false.
type FamilyLogger struct {
	mu             sync.Mutex
	level          slog.Level
	redact         bool
	serviceName    string
	serviceVersion string
	redactor       security.Redactor
	preview        int
	handler        slog.Handler
	sink           []LogRecord
}

// FamilyLoggerOptions constructs FamilyLogger (Python FamilyLogger.__init__).
type FamilyLoggerOptions struct {
	Level          string
	Redact         bool
	ServiceName    string
	ServiceVersion string
	Redactor       security.Redactor
	PreviewMax     int
	Handler        slog.Handler
	Writer         io.Writer
	Capture        bool
}

// NewFamilyLogger binds a per-runtime logger. No process-global HTTP.
func NewFamilyLogger(opts FamilyLoggerOptions) *FamilyLogger {
	name := opts.ServiceName
	if name == "" {
		name = "zeus_client"
	}
	preview := opts.PreviewMax
	if preview == 0 {
		preview = security.PreviewMaxChars
	}
	red := opts.Redactor
	if red == nil {
		red = security.New()
	}
	h := opts.Handler
	if h == nil {
		w := opts.Writer
		if w == nil {
			w = io.Discard
		}
		h = slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug - 8})
	}
	return &FamilyLogger{
		level:          parseLevel(opts.Level),
		redact:         opts.Redact,
		serviceName:    name,
		serviceVersion: opts.ServiceVersion,
		redactor:       red,
		preview:        preview,
		handler:        h,
	}
}

// SetLevel updates the minimum family level (error/info/debug/trace).
func (l *FamilyLogger) SetLevel(level string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = parseLevel(level)
}

// Emit writes one family event. Nil attrs and nil values are omitted.
func (l *FamilyLogger) Emit(severity, event string, attrs map[string]any) {
	if l == nil {
		return
	}
	lv := parseLevel(severity)
	l.mu.Lock()
	defer l.mu.Unlock()
	if lv < l.level {
		return
	}
	merged := map[string]any{"service.name": l.serviceName}
	for k, v := range attrs {
		if v == nil {
			continue
		}
		if k == "level" {
			merged["log.level"] = v
			continue
		}
		merged[k] = v
	}
	if l.serviceVersion != "" {
		merged["service.version"] = l.serviceVersion
	}
	safe := security.RedactAttrsWith(merged, l.redact, l.redactor, l.preview)
	l.sink = append(l.sink, LogRecord{
		Level: levelName(lv),
		Event: event,
		Attrs: safe,
	})
	if l.handler != nil && l.handler.Enabled(context.Background(), lv) {
		logger := slog.New(l.handler)
		args := make([]any, 0, len(safe)*2)
		for k, v := range safe {
			args = append(args, k, v)
		}
		logger.Log(context.Background(), lv, event, args...)
	}
}

// Info is Emit("info", ...).
func (l *FamilyLogger) Info(event string, attrs map[string]any) {
	l.Emit("info", event, attrs)
}

// Error is Emit("error", ...).
func (l *FamilyLogger) Error(event string, attrs map[string]any) {
	l.Emit("error", event, attrs)
}

// Debug is Emit("debug", ...).
func (l *FamilyLogger) Debug(event string, attrs map[string]any) {
	l.Emit("debug", event, attrs)
}

// Trace is Emit("trace", ...). Below DEBUG.
func (l *FamilyLogger) Trace(event string, attrs map[string]any) {
	l.Emit("trace", event, attrs)
}

// Func is the adapter Log callback (level, event, attrs).
func (l *FamilyLogger) Func() func(level, msg string, attrs map[string]any) {
	return func(level, msg string, attrs map[string]any) {
		l.Emit(level, msg, attrs)
	}
}

// Records copies captured events (test helper).
func (l *FamilyLogger) Records() []LogRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LogRecord, len(l.sink))
	copy(out, l.sink)
	return out
}

// Configure emits zeus_client.logging.configured (Python configure_family_logger).
func (l *FamilyLogger) Configure() {
	if l == nil {
		return
	}
	l.mu.Lock()
	level := l.level
	redact := l.redact
	l.mu.Unlock()
	l.Info("zeus_client.logging.configured", map[string]any{
		"result": "ok",
		"level":  strings.ToLower(levelName(level)),
		"redact": redact,
	})
}
