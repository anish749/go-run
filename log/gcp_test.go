package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	gorunlog "github.com/anish749/go-run/log"
)

// decode parses a single JSON log line, tolerating the trailing newline that
// slog.NewJSONHandler emits.
func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &got); err != nil {
		t.Fatalf("decode JSON: %v (output: %q)", err, b)
	}
	return got
}

func TestNewGCPHandler_RenamesTopLevelKeys(t *testing.T) {
	var buf bytes.Buffer
	h := gorunlog.NewGCPHandler(&buf, nil)
	slog.New(h).Info("hello")

	got := decode(t, buf.Bytes())

	for _, k := range []string{"severity", "timestamp", "message"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q in output: %v", k, got)
		}
	}
	for _, k := range []string{"level", "time", "msg"} {
		if _, ok := got[k]; ok {
			t.Errorf("stdlib key %q must not appear: %v", k, got)
		}
	}
	if got["severity"] != "INFO" {
		t.Errorf("severity: got %v, want INFO", got["severity"])
	}
	if got["message"] != "hello" {
		t.Errorf("message: got %v, want hello", got["message"])
	}
}

func TestNewGCPHandler_SeverityMapping(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelWarn, "WARNING"}, // critical: must be WARNING, not slog's "WARN"
		{slog.LevelError, "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			var buf bytes.Buffer
			h := gorunlog.NewGCPHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			slog.New(h).Log(context.Background(), tc.level, "msg")
			got := decode(t, buf.Bytes())
			if got["severity"] != tc.want {
				t.Errorf("level %v: severity %v, want %v", tc.level, got["severity"], tc.want)
			}
		})
	}
}

func TestNewGCPHandler_UserReplaceAttrComposesAfterRename(t *testing.T) {
	var buf bytes.Buffer
	called := false
	opts := &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			called = true
			// At this point the rename has already happened, so the user
			// observes "severity", not "level".
			if a.Key == "severity" {
				a.Value = slog.StringValue("CUSTOM_" + a.Value.String())
			}
			return a
		},
	}
	h := gorunlog.NewGCPHandler(&buf, opts)
	slog.New(h).Info("hello")

	if !called {
		t.Fatal("user ReplaceAttr was not invoked")
	}
	got := decode(t, buf.Bytes())
	if got["severity"] != "CUSTOM_INFO" {
		t.Errorf("user ReplaceAttr should run after rename; severity=%v", got["severity"])
	}
}

func TestNewGCPHandler_DoesNotMutateCallerOpts(t *testing.T) {
	opts := &slog.HandlerOptions{}
	var buf bytes.Buffer
	_ = gorunlog.NewGCPHandler(&buf, opts)
	if opts.ReplaceAttr != nil {
		t.Error("opts.ReplaceAttr was mutated; the handler must shallow-copy")
	}
}

func TestNewGCPHandler_NilOpts(t *testing.T) {
	var buf bytes.Buffer
	h := gorunlog.NewGCPHandler(&buf, nil)
	slog.New(h).Info("hello")
	if buf.Len() == 0 {
		t.Fatal("no output emitted with nil opts")
	}
}

func TestNewGCPHandler_AddSourcePassthrough(t *testing.T) {
	var buf bytes.Buffer
	h := gorunlog.NewGCPHandler(&buf, &slog.HandlerOptions{AddSource: true})
	slog.New(h).Info("hello")
	if !strings.Contains(buf.String(), "source") {
		t.Errorf("AddSource not passed through; output: %s", buf.String())
	}
}

func TestNewGCPHandler_GroupAttrsNotRenamed(t *testing.T) {
	// A user attribute named "level" inside a group must not be renamed to
	// "severity"; only the canonical top-level slog keys are renamed.
	var buf bytes.Buffer
	h := gorunlog.NewGCPHandler(&buf, nil)
	slog.New(h).WithGroup("foo").Info("hello", "level", "user-level")

	got := decode(t, buf.Bytes())
	foo, ok := got["foo"].(map[string]any)
	if !ok {
		t.Fatalf("expected foo group; got: %v", got)
	}
	if _, has := foo["level"]; !has {
		t.Errorf("user 'level' inside group should be preserved; got: %v", foo)
	}
	if _, has := foo["severity"]; has {
		t.Errorf("user 'level' inside group must not be renamed to 'severity'; got: %v", foo)
	}
}
