// Package log provides slog adapters for code running on Google Cloud Run.
//
// NewGCPHandler returns an slog.Handler that emits JSON in the field shape
// Cloud Logging expects ("severity", "timestamp", "message" rather than
// stdlib's "level", "time", "msg"), with WARN-level records mapped to
// "WARNING" so GCP severity classification works.
//
// The user owns the *slog.Logger; this package only constructs the handler:
//
//	h := log.NewGCPHandler(os.Stdout, nil)
//	logger := slog.New(h)
package log

import (
	"io"
	"log/slog"
)

// NewGCPHandler returns an slog.Handler that emits Cloud Logging-formatted
// JSON to w.
//
// It wraps slog.NewJSONHandler with a ReplaceAttr that renames the canonical
// slog keys to Cloud Logging's expected names:
//
//	"level" → "severity" (with the value mapped to a Cloud Logging severity string)
//	"time"  → "timestamp"
//	"msg"   → "message"
//
// Severity strings follow Cloud Logging's LogSeverity enum: DEBUG, INFO,
// WARNING (note: not slog's "WARN"), ERROR.
//
// If opts is nil, a zero-value slog.HandlerOptions is used. If opts.ReplaceAttr
// is set, it runs AFTER the GCP renaming so callers can layer further
// transformations on the renamed attributes (e.g. add fields, redact values).
//
// opts is shallow-copied; the caller's struct is not mutated.
func NewGCPHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	var local slog.HandlerOptions
	if opts != nil {
		local = *opts
	}
	user := local.ReplaceAttr
	local.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
		a = gcpRename(groups, a)
		if user != nil {
			a = user(groups, a)
		}
		return a
	}
	return slog.NewJSONHandler(w, &local)
}

// gcpRename renames the standard slog keys to their Cloud Logging equivalents.
// It only acts on top-level attributes; user-provided attributes inside a
// group keep their original keys, even if those keys happen to be "level",
// "time", or "msg".
func gcpRename(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		level, ok := a.Value.Any().(slog.Level)
		if !ok {
			return a
		}
		a.Key = "severity"
		a.Value = slog.StringValue(gcpSeverity(level))
	case slog.TimeKey:
		a.Key = "timestamp"
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

// gcpSeverity maps slog levels to Cloud Logging severity strings. See
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity.
//
// Only the four levels slog ships out of the box are mapped here. Custom
// levels in between (e.g. NOTICE between INFO and WARNING) are not currently
// addressed; if a real consumer needs them, the mapping is a one-line change.
func gcpSeverity(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO"
	case l < slog.LevelError:
		return "WARNING"
	default:
		return "ERROR"
	}
}
