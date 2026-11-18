package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "WARN": slog.LevelWarn, " error ": slog.LevelError, "": slog.LevelInfo, "nope": slog.LevelInfo,
	} {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewFormatAndLevel(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "warn", "text")
	l.Info("hidden")
	l.Warn("shown", "k", "v")
	if out := b.String(); strings.Contains(out, "hidden") || !strings.Contains(out, "msg=shown") {
		t.Fatalf("text/warn output = %q", out)
	}

	b.Reset()
	New(&b, "info", "color").Info("pretty", "k", "v")
	if out := b.String(); strings.Contains(out, "\x1b") || !strings.Contains(out, "pretty") {
		t.Fatalf("color to a non-terminal must have no escape codes, got %q", out)
	}

	b.Reset()
	New(&b, "info", "").Info("x")
	if !strings.HasPrefix(b.String(), "{") {
		t.Fatalf("default format should be json, got %q", b.String())
	}
}
