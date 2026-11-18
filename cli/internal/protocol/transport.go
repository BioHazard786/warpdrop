package protocol

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// Wire is one frame: text (JSON control message) or binary (file chunk).
type Wire struct {
	Text   string
	Bin    []byte
	IsText bool
}

type Role int

const (
	RoleSender Role = iota
	RoleReceiver
)

var ErrClosed = errors.New("transport closed")

// Transport is an ordered, reliable channel carrying text and binary frames.
type Transport interface {
	Kind() string // "rtc", "relay" or "memory"
	MaxMessage() int
	// Buffered is the number of bytes queued but not yet on the wire.
	Buffered() uint64
	// Fingerprints are the DTLS digests bound into the handshake transcript (nil off DTLS).
	LocalFingerprint() []byte
	RemoteFingerprint() []byte
	SendText(string) error
	// SendBinary may retain b: callers must not modify it afterwards.
	SendBinary(b []byte) error
	// Drain blocks until Buffered has fallen to LowWater.
	Drain(ctx context.Context) error
	// EnableCrypto is called once after the handshake; only the relay encrypts.
	EnableCrypto(sessionKey []byte, role Role) error
	// Recv blocks for the next frame. It returns ErrClosed (after delivering everything already received) when the link ends.
	Recv(ctx context.Context) (Wire, error)
	Close() error
}

func chunkSizeFor(t Transport) int { return min(ChunkSize, t.MaxMessage()) }

// ------------------------------------------------------------------ in-memory pair (tests)

type MemTransport struct {
	in        chan Wire
	peer      *MemTransport
	done      chan struct{}
	closeOnce *sync.Once
	fp, pfp   []byte
	SentBytes atomic.Int64
	// Tamper may rewrite or drop (ok=false) outgoing frames.
	Tamper func(Wire) (Wire, bool)
}

func NewMemPair(fingerprints bool) (*MemTransport, *MemTransport) {
	done, once := make(chan struct{}), &sync.Once{}
	a := &MemTransport{in: make(chan Wire, 64), done: done, closeOnce: once}
	b := &MemTransport{in: make(chan Wire, 64), done: done, closeOnce: once}
	a.peer, b.peer = b, a
	if fingerprints {
		a.fp, b.fp = repeat(0xaa), repeat(0xbb)
		a.pfp, b.pfp = b.fp, a.fp
	}
	return a, b
}

func repeat(v byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = v
	}
	return b
}

func (m *MemTransport) Kind() string                    { return "memory" }
func (m *MemTransport) MaxMessage() int                 { return ChunkSize }
func (m *MemTransport) Buffered() uint64                { return 0 }
func (m *MemTransport) LocalFingerprint() []byte        { return m.fp }
func (m *MemTransport) RemoteFingerprint() []byte       { return m.pfp }
func (m *MemTransport) Drain(context.Context) error     { return nil }
func (m *MemTransport) EnableCrypto([]byte, Role) error { return nil }
func (m *MemTransport) Close() error {
	m.closeOnce.Do(func() { close(m.done) })
	return nil
}

func (m *MemTransport) send(w Wire) error {
	if m.Tamper != nil {
		var ok bool
		if w, ok = m.Tamper(w); !ok {
			return nil
		}
	}
	m.SentBytes.Add(int64(len(w.Text) + len(w.Bin)))
	if !w.IsText {
		w.Bin = append([]byte(nil), w.Bin...)
	}
	select {
	case m.peer.in <- w:
		return nil
	case <-m.done:
		return ErrClosed
	}
}

func (m *MemTransport) SendText(s string) error   { return m.send(Wire{Text: s, IsText: true}) }
func (m *MemTransport) SendBinary(b []byte) error { return m.send(Wire{Bin: b}) }

func (m *MemTransport) Recv(ctx context.Context) (Wire, error) {
	select { // deliver anything already queued before reporting the close
	case w := <-m.in:
		return w, nil
	default:
	}
	select {
	case w := <-m.in:
		return w, nil
	case <-m.done:
		select {
		case w := <-m.in:
			return w, nil
		default:
			return Wire{}, ErrClosed
		}
	case <-ctx.Done():
		return Wire{}, ctx.Err()
	}
}
