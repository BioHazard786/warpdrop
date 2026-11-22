package protocol

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxLANFrame = 1 << 20
	lanLinger   = 500 * time.Millisecond // how long Close waits for the peer to read our last frame
)

// lanConn frames a TCP stream as type(1) || len(4, big endian) || data, so RelayTransport can run on it unchanged.
type lanConn struct {
	c  net.Conn
	r  *bufio.Reader // one reader goroutine (RelayTransport.readLoop), so no lock
	h  [5]byte
	mu sync.Mutex
}

// NewLANTransport carries the protocol over an already connected TCP stream between two devices on one network.
// The stream is plaintext until the handshake enables the frame cipher, exactly like the relay.
func NewLANTransport(c net.Conn) *RelayTransport {
	return newTransport(&lanConn{c: c, r: bufio.NewReaderSize(c, 64<<10)}, "lan")
}

func (l *lanConn) ReadMessage() (int, []byte, error) {
	if _, err := io.ReadFull(l.r, l.h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(l.h[1:])
	if n > maxLANFrame || (l.h[0] != websocket.TextMessage && l.h[0] != websocket.BinaryMessage) {
		return 0, nil, errors.New("lan: bad frame")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(l.r, data); err != nil {
		return 0, nil, err
	}
	return int(l.h[0]), data, nil
}

// WriteMessage sends header and data with one writev, without copying data.
func (l *lanConn) WriteMessage(typ int, data []byte) error {
	var h [5]byte
	h[0] = byte(typ)
	binary.BigEndian.PutUint32(h[1:], uint32(len(data)))
	bufs := net.Buffers{h[:], data}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := bufs.WriteTo(l.c)
	return err
}

// Close sends FIN after our last frame (often a cancel) and drains briefly before closing: closing a socket with
// unread data sends a reset instead, and a reset can make the peer drop that last frame unread.
func (l *lanConn) Close() error {
	if tc, ok := l.c.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
		_ = tc.SetReadDeadline(time.Now().Add(lanLinger))
		_, _ = io.Copy(io.Discard, tc)
	}
	return l.c.Close()
}
