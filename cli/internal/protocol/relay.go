package protocol

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const keepaliveEvery = 20 * time.Second

type rawMsg struct {
	typ  int
	data []byte
	err  error
}

// RelayTransport carries frames over a WebSocket through the server's /relay endpoint.
// After EnableCrypto every frame is AES-256-GCM sealed, so the server only sees ciphertext.
type RelayTransport struct {
	conn *websocket.Conn
	raw  chan rawMsg
	done chan struct{}
	once sync.Once

	wmu    sync.Mutex // gorilla allows one concurrent writer
	tx, rx *FrameCipher
	mu     sync.Mutex                 // guards tx/rx assignment
	wire   func(typ int, data []byte) // test hook: sees exactly what is written to the socket
}

// DialRelay connects to a ws(s)://host/relay/<ticket> URL.
func DialRelay(ctx context.Context, url string) (*RelayTransport, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(1 << 20)
	t := &RelayTransport{conn: conn, raw: make(chan rawMsg, 16), done: make(chan struct{})}
	go t.readLoop()
	return t, nil
}

// readLoop only moves raw messages; decryption happens in Recv, after EnableCrypto, so early ciphertext just queues.
func (t *RelayTransport) readLoop() {
	for {
		typ, data, err := t.conn.ReadMessage()
		select {
		case t.raw <- rawMsg{typ, data, err}:
		case <-t.done:
			return
		}
		if err != nil {
			return
		}
	}
}

func (t *RelayTransport) Kind() string                { return "relay" }
func (t *RelayTransport) MaxMessage() int             { return ChunkSize }
func (t *RelayTransport) Buffered() uint64            { return 0 }   // writes block on the socket, so there is no queue to watch
func (t *RelayTransport) LocalFingerprint() []byte    { return nil } // not DTLS: the transcript omits fingerprints
func (t *RelayTransport) RemoteFingerprint() []byte   { return nil }
func (t *RelayTransport) Drain(context.Context) error { return nil }

func (t *RelayTransport) EnableCrypto(key []byte, role Role) error {
	txDir, rxDir := DirSenderToReceiver, DirReceiverToSender
	if role == RoleReceiver {
		txDir, rxDir = rxDir, txDir
	}
	tx, err := NewFrameCipher(key, txDir)
	if err != nil {
		return err
	}
	rx, err := NewFrameCipher(key, rxDir)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.tx, t.rx = tx, rx
	t.mu.Unlock()
	go t.keepalive()
	return nil
}

func (t *RelayTransport) ciphers() (tx, rx *FrameCipher) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tx, t.rx
}

// keepalive stops the server's 60 s idle limit from ending a session that is paused (e.g. a user deciding).
func (t *RelayTransport) keepalive() {
	tick := time.NewTicker(keepaliveEvery)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if t.write(KindKeepalive, nil) != nil {
				return
			}
		case <-t.done:
			return
		}
	}
}

func (t *RelayTransport) write(kind byte, payload []byte) error {
	tx, _ := t.ciphers()
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if tx == nil { // handshake frames are plaintext text messages
		if kind != KindText {
			return errors.New("relay: binary before crypto is enabled")
		}
		if t.wire != nil {
			t.wire(websocket.TextMessage, payload)
		}
		return t.conn.WriteMessage(websocket.TextMessage, payload)
	}
	// seal under the write lock so counter order equals wire order
	sealed := tx.Seal(kind, payload)
	if t.wire != nil {
		t.wire(websocket.BinaryMessage, sealed)
	}
	return t.conn.WriteMessage(websocket.BinaryMessage, sealed)
}

func (t *RelayTransport) SendText(s string) error   { return t.write(KindText, []byte(s)) }
func (t *RelayTransport) SendBinary(b []byte) error { return t.write(KindBinary, b) }

func (t *RelayTransport) Recv(ctx context.Context) (Wire, error) {
	for {
		var m rawMsg
		select {
		case m = <-t.raw:
		case <-t.done:
			return Wire{}, ErrClosed
		case <-ctx.Done():
			return Wire{}, ctx.Err()
		}
		if m.err != nil {
			t.Close()
			return Wire{}, ErrClosed
		}
		_, rx := t.ciphers()
		if rx == nil { // plaintext phase: only text handshake frames are valid
			if m.typ != websocket.TextMessage {
				t.Close()
				return Wire{}, fmt.Errorf("%w: binary before handshake finished", ErrBadFrame)
			}
			return Wire{Text: string(m.data), IsText: true}, nil
		}
		if m.typ != websocket.BinaryMessage {
			t.Close()
			return Wire{}, ErrClosed // text after the cipher is on is a protocol violation
		}
		kind, payload, err := rx.Open(m.data)
		if err != nil {
			t.Close() // bad tag, replay or reorder
			return Wire{}, ErrClosed
		}
		switch kind {
		case KindKeepalive:
			continue
		case KindText:
			return Wire{Text: string(payload), IsText: true}, nil
		default:
			return Wire{Bin: payload}, nil
		}
	}
}

func (t *RelayTransport) Close() error {
	t.once.Do(func() {
		close(t.done)
		_ = t.conn.Close()
	})
	return nil
}
