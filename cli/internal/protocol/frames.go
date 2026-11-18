// Package protocol implements WarpDrop protocol v2 (see protocol/PROTOCOL.md): framing, handshake crypto,
// the pure transfer state machines, sessions, and transports.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	Version          = 2
	ChunkSize        = 256 << 10
	AckEvery         = 4 << 20
	MaxUnacked       = 32 << 20
	HighWater        = 16 << 20
	LowWater         = 4 << 20
	MaxFiles         = 10_000
	MaxNameBytes     = 255
	MaxManifestBytes = 60_000
	HandshakeTimeout = 20 * time.Second
	StallTimeout     = 30 * time.Second
)

type Reason string

const (
	ReasonDeclined      Reason = "declined"
	ReasonUser          Reason = "user"
	ReasonAuthFailed    Reason = "auth_failed"
	ReasonHashMismatch  Reason = "hash_mismatch"
	ReasonProtocolError Reason = "protocol_error"
	ReasonClosed        Reason = "closed" // local only: transport lost
)

func validReason(r Reason) bool {
	switch r {
	case ReasonDeclined, ReasonUser, ReasonAuthFailed, ReasonHashMismatch, ReasonProtocolError:
		return true
	}
	return false
}

type FileEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// Frame types. Every frame carries its type in "t".
type (
	Hello struct {
		T      string `json:"t"`
		V      int    `json:"v"`
		Client string `json:"client"`
		Device string `json:"device"`
		Pub    string `json:"pub"`
	}
	Auth struct {
		T   string `json:"t"`
		Mac string `json:"mac"`
	}
	Manifest struct {
		T     string      `json:"t"`
		Files []FileEntry `json:"files"`
	}
	Request struct {
		T      string `json:"t"`
		ID     int    `json:"id"`
		Offset int64  `json:"offset"`
	}
	FileStart struct {
		T      string `json:"t"`
		ID     int    `json:"id"`
		Offset int64  `json:"offset"`
	}
	FileEnd struct {
		T    string `json:"t"`
		ID   int    `json:"id"`
		Hash string `json:"hash"`
	}
	FileDone struct {
		T  string `json:"t"`
		ID int    `json:"id"`
	}
	Ack struct {
		T     string `json:"t"`
		ID    int    `json:"id"`
		Bytes int64  `json:"bytes"`
	}
	Cancel struct {
		T      string `json:"t"`
		Reason Reason `json:"reason"`
	}
)

func NewHello(client, device, pubHex string) Hello {
	return Hello{"hello", Version, client, device, pubHex}
}
func NewAuth(mac string) Auth                  { return Auth{"auth", mac} }
func NewManifest(f []FileEntry) Manifest       { return Manifest{"manifest", f} }
func NewRequest(id int, off int64) Request     { return Request{"request", id, off} }
func NewFileStart(id int, off int64) FileStart { return FileStart{"file_start", id, off} }
func NewFileEnd(id int, hash string) FileEnd   { return FileEnd{"file_end", id, hash} }
func NewFileDone(id int) FileDone              { return FileDone{"file_done", id} }
func NewAck(id int, b int64) Ack               { return Ack{"ack", id, b} }
func NewCancel(r Reason) Cancel                { return Cancel{"cancel", r} }

// Encode serializes any frame to its JSON text form.
func Encode(f any) string {
	b, err := json.Marshal(f)
	if err != nil {
		panic(err) // frames are plain structs
	}
	return string(b)
}

var (
	hex16 = regexp.MustCompile(`^[0-9a-f]{16}$`)
	hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// ErrBadFrame means the peer sent something outside the protocol: treat as protocol_error.
	ErrBadFrame = errors.New("invalid frame")
)

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrBadFrame, fmt.Sprintf(format, a...))
}

// ParseFrame decodes and validates one text frame, returning a typed value (Hello, Auth, ...).
// Unknown extra fields are ignored for forward compatibility; wrong types and out-of-range values are rejected.
func ParseFrame(text string) (any, error) {
	var head struct {
		T string `json:"t"`
	}
	if err := json.Unmarshal([]byte(text), &head); err != nil {
		return nil, bad("not JSON")
	}
	dec := func(v any) error {
		if err := json.Unmarshal([]byte(text), v); err != nil {
			return bad("%s: %v", head.T, err)
		}
		return nil
	}
	switch head.T {
	case "hello":
		var m Hello
		if err := dec(&m); err != nil {
			return nil, err
		}
		if !hex64.MatchString(m.Pub) || len(m.Client) > 64 || len(m.Device) > 128 {
			return nil, bad("hello fields")
		}
		return m, nil
	case "auth":
		var m Auth
		if err := dec(&m); err != nil {
			return nil, err
		}
		if !hex64.MatchString(m.Mac) {
			return nil, bad("auth mac")
		}
		return m, nil
	case "manifest":
		var m Manifest
		if err := dec(&m); err != nil {
			return nil, err
		}
		if len(m.Files) == 0 || len(m.Files) > MaxFiles {
			return nil, bad("manifest size")
		}
		seen := map[int]bool{}
		for _, f := range m.Files {
			if f.ID < 0 || f.ID > MaxFiles || seen[f.ID] || f.Size < 0 || f.Name == "" ||
				len(f.Name) > MaxNameBytes || !utf8.ValidString(f.Name) || len(f.Type) > 255 {
				return nil, bad("manifest entry")
			}
			seen[f.ID] = true
		}
		return m, nil
	case "request":
		var m Request
		if err := dec(&m); err != nil {
			return nil, err
		}
		if m.ID < 0 || m.ID > MaxFiles || m.Offset < 0 {
			return nil, bad("request range")
		}
		return m, nil
	case "file_start":
		var m FileStart
		if err := dec(&m); err != nil {
			return nil, err
		}
		if m.ID < 0 || m.ID > MaxFiles || m.Offset < 0 {
			return nil, bad("file_start range")
		}
		return m, nil
	case "file_end":
		var m FileEnd
		if err := dec(&m); err != nil {
			return nil, err
		}
		if m.ID < 0 || m.ID > MaxFiles || !hex16.MatchString(m.Hash) {
			return nil, bad("file_end")
		}
		return m, nil
	case "file_done":
		var m FileDone
		if err := dec(&m); err != nil {
			return nil, err
		}
		if m.ID < 0 || m.ID > MaxFiles {
			return nil, bad("file_done range")
		}
		return m, nil
	case "ack":
		var m Ack
		if err := dec(&m); err != nil {
			return nil, err
		}
		if m.ID < 0 || m.ID > MaxFiles || m.Bytes < 0 {
			return nil, bad("ack range")
		}
		return m, nil
	case "cancel":
		var m Cancel
		if err := dec(&m); err != nil {
			return nil, err
		}
		if !validReason(m.Reason) {
			return nil, bad("cancel reason")
		}
		return m, nil
	}
	return nil, bad("unknown type %q", head.T)
}

var (
	windowsReserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])$`)
	illegalChars    = strings.NewReplacer("<", "_", ">", "_", ":", "_", `"`, "_", "|", "_", "?", "_", "*", "_")
)

func unsafeRune(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0x200e || r == 0x200f ||
		(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

// SanitizeName makes a peer-supplied name safe as a single file name on any OS. Mirrors the web app's sanitizeName.
func SanitizeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimRight(illegalChars.Replace(name), ". ")
	if name == "" {
		name = "file"
	}
	if windowsReserved.MatchString(strings.SplitN(name, ".", 2)[0]) {
		name = "_" + name
	}
	for len(name) > MaxNameBytes {
		_, n := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-n]
	}
	return name
}

// DisplayName is SanitizeName for terminal output: also strips anything that could drive the terminal.
func DisplayName(name string) string { return SanitizeName(name) }

func hexU64(v uint64) string { return fmt.Sprintf("%016x", v) }
