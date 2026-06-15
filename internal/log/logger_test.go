package log

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNonTTYEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	l := NewLoggerWithOutput(LevelInfo, &buf) // buffer is not a TTY -> JSON
	l.Info("hello %s", "world")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected JSON log line, got %q: %v", buf.String(), err)
	}
	if entry["msg"] != "hello world" {
		t.Fatalf("msg = %v, want %q", entry["msg"], "hello world")
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := NewLoggerWithOutput(LevelWarn, &buf)
	l.Info("should be filtered")
	l.Warn("should appear")
	out := buf.String()
	if strings.Contains(out, "should be filtered") {
		t.Fatalf("info line leaked at warn level: %q", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Fatalf("warn line missing: %q", out)
	}
}
