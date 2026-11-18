// Package logging builds the process-wide slog.Logger. All output goes through a slog.Handler,
// so a new look (e.g. tinted colors) is one more case in handler(), with no change at call sites.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

// Formats accepted by New.
const (
	FormatJSON  = "json"  // machine-readable, for production and log shippers
	FormatText  = "text"  // key=value lines, plain
	FormatColor = "color" // tinted, human-friendly; colors only when w is a terminal
)

// New returns a logger writing to w. An unknown level falls back to info and an unknown format to json.
func New(w io.Writer, level, format string) *slog.Logger {
	return slog.New(handler(w, format, &slog.HandlerOptions{Level: ParseLevel(level)}))
}

// Setup builds a logger with New and makes it the slog default.
func Setup(w io.Writer, level, format string) *slog.Logger {
	l := New(w, level, format)
	slog.SetDefault(l)
	return l
}

// ParseLevel maps debug|info|warn|error (case-insensitive, optional +/-N offset) to a slog.Level.
func ParseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.TrimSpace(s))); err != nil {
		return slog.LevelInfo
	}
	return l
}

// handler is the single place to plug in another output style.
func handler(w io.Writer, format string, o *slog.HandlerOptions) slog.Handler {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatText:
		return slog.NewTextHandler(w, o)
	case FormatColor:
		f, ok := w.(*os.File)
		return tint.NewTextHandler(w, &tint.Options{Level: o.Level, TimeFormat: time.DateTime, NoColor: !ok || !isatty.IsTerminal(f.Fd()) && !isatty.IsCygwinTerminal(f.Fd())})
	default:
		return slog.NewJSONHandler(w, o)
	}
}
