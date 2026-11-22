// Package signaling is a thin context-aware client for the server's /ws endpoint.
package signaling

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// Message is any server→client message; unused fields stay zero.
type Message struct {
	T       string          `json:"t"`
	RoomID  string          `json:"room_id"`
	Token   string          `json:"token"`
	PeerID  string          `json:"peer_id"`
	From    string          `json:"from"`
	Ticket  string          `json:"ticket"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"` // invite_declined: "declined" or "busy"
	Peers   []string        `json:"peers"`
	Client  json.RawMessage `json:"client"`
	Payload json.RawMessage `json:"payload"`
	// nearby lobby: the full device list, or the one device an event is about
	Devices json.RawMessage `json:"devices"`
	Peer    json.RawMessage `json:"peer"`
}

// Client delivers messages on In, which is closed when the connection ends.
type Client struct {
	In   chan Message
	conn *websocket.Conn
	wmu  sync.Mutex
	once sync.Once
}

func Dial(ctx context.Context, url string) (*Client, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(64 << 10)
	c := &Client{In: make(chan Message, 32), conn: conn}
	go c.read(ctx)
	return c, nil
}

func (c *Client) read(ctx context.Context) {
	defer close(c.In)
	for {
		var m Message
		if err := c.conn.ReadJSON(&m); err != nil {
			return
		}
		select {
		case c.In <- m:
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) Send(v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.conn.WriteJSON(v)
}

func (c *Client) Close() { c.once.Do(func() { _ = c.conn.Close() }) }
