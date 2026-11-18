package signaling

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingEvery  = 30 * time.Second
	readLimit  = 32 << 10
	sendBuffer = 64
	maxBad     = 10
)

// Client is one WebSocket connection. Its send channel is never closed, so a send can never panic;
// done is closed exactly once and tells the write pump to stop.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	ip   string
	out  chan []byte
	done chan struct{}
	once sync.Once

	// guarded by hub.mu
	room   *Room
	peerID string
}

func NewClient(h *Hub, conn *websocket.Conn, ip string) *Client {
	return &Client{hub: h, conn: conn, ip: ip, out: make(chan []byte, sendBuffer), done: make(chan struct{})}
}

// send never blocks: a client that cannot keep up is dropped instead of stalling the hub.
func (c *Client) send(b []byte) {
	select {
	case c.out <- b:
	default:
		c.close()
	}
}

func (c *Client) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// Serve runs the connection until it ends. It blocks, so call it from the HTTP handler.
func (c *Client) Serve() {
	go c.writePump()
	c.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.close()
		c.hub.leave(c)
	}()
	c.conn.SetReadLimit(readLimit)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	bad := 0
	for {
		mt, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.TextMessage {
			if bad++; bad >= maxBad {
				return
			}
			c.send(errMsg(ErrBadRequest, "text frames only"))
			continue
		}
		c.hub.handle(c, data)
	}
}

func (c *Client) writePump() {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	defer c.close()
	for {
		select {
		case b := <-c.out:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if c.conn.WriteMessage(websocket.TextMessage, b) != nil {
				return
			}
		case <-t.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
