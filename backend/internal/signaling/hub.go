// Package signaling implements the room/peer registry and message routing for /ws.
package signaling

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/backend/internal/ratelimit"
)

const (
	Version      = 2
	MaxReceivers = 10
	Grace        = 30 * time.Second
)

// Error codes sent to clients.
const (
	ErrNotFound    = "not_found"
	ErrRoomFull    = "room_full"
	ErrRateLimited = "rate_limited"
	ErrBadRequest  = "bad_request"
	ErrUnsupported = "unsupported_version"
	ErrRelayOff    = "relay_disabled"
)

// Tickets issues relay tickets; nil disables the relay.
type Tickets interface{ Issue() string }

type Room struct {
	id        string
	token     string
	sender    *Client
	receivers map[string]*Client
	grace     *time.Timer
}

type Hub struct {
	Grace   time.Duration
	Tickets Tickets
	Create  *ratelimit.IP
	Join    *ratelimit.IP
	Relay   *ratelimit.IP

	mu    sync.Mutex
	rooms map[string]*Room
}

func NewHub(t Tickets) *Hub {
	return &Hub{
		Grace:   Grace,
		Tickets: t,
		Create:  ratelimit.NewIP(10, 5),
		Join:    ratelimit.NewIP(30, 10),
		Relay:   ratelimit.NewIP(30, 10),
		rooms:   map[string]*Room{},
	}
}

type in struct {
	T       string          `json:"t"`
	V       int             `json:"v"`
	Client  json.RawMessage `json:"client"`
	RoomID  string          `json:"room_id"`
	Token   string          `json:"token"`
	To      string          `json:"to"`
	Payload json.RawMessage `json:"payload"`
}

func msg(v map[string]any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func errMsg(code, text string) []byte {
	return msg(map[string]any{"t": "error", "code": code, "message": text})
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// handle routes one message from c. Never blocks: all sends are non-blocking.
func (h *Hub) handle(c *Client, raw []byte) {
	var m in
	if json.Unmarshal(raw, &m) != nil || len(m.Client) > 1024 {
		c.send(errMsg(ErrBadRequest, "malformed message"))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch m.T {
	case "create_room":
		h.create(c, m)
	case "resume_room":
		h.resume(c, m)
	case "join_room":
		h.join(c, m)
	case "signal":
		h.signal(c, m)
	case "relay_request":
		h.relayRequest(c, m)
	case "close_room":
		if c.room != nil && c.room.sender == c {
			h.closeRoom(c.room)
		}
	default:
		c.send(errMsg(ErrBadRequest, "unknown message type"))
	}
}

func (h *Hub) create(c *Client, m in) {
	if m.V != Version {
		c.send(errMsg(ErrUnsupported, "protocol v2 required"))
		return
	}
	if c.room != nil {
		c.send(errMsg(ErrBadRequest, "already in a room"))
		return
	}
	if !h.Create.Allow(c.ip) {
		c.send(errMsg(ErrRateLimited, "too many rooms"))
		return
	}
	id := h.newID()
	if id == "" {
		c.send(errMsg(ErrRateLimited, "server busy"))
		return
	}
	r := &Room{id: id, token: randHex(16), sender: c, receivers: map[string]*Client{}}
	h.rooms[id] = r
	c.room, c.peerID = r, "sender"
	c.send(msg(map[string]any{"t": "room_created", "room_id": id, "token": r.token}))
}

// newID picks a free 4-digit ID, widening to 6 digits when 4-digit picks keep colliding.
func (h *Hub) newID() string {
	for _, digits := range []int{4, 6} {
		max := big.NewInt(1)
		for i := 0; i < digits; i++ {
			max.Mul(max, big.NewInt(10))
		}
		for try := 0; try < 20; try++ {
			n, _ := rand.Int(rand.Reader, max)
			id := fmt.Sprintf("%0*d", digits, n.Int64())
			if h.rooms[id] == nil {
				return id
			}
		}
	}
	return ""
}

func (h *Hub) resume(c *Client, m in) {
	r := h.rooms[m.RoomID]
	if r == nil || subtle.ConstantTimeCompare([]byte(r.token), []byte(m.Token)) != 1 {
		c.send(errMsg(ErrNotFound, "room not found"))
		return
	}
	if c.room != nil {
		c.send(errMsg(ErrBadRequest, "already in a room"))
		return
	}
	if r.grace != nil {
		r.grace.Stop()
		r.grace = nil
	}
	if old := r.sender; old != nil && old != c {
		old.room = nil
		old.close() // a half-dead socket the server has not noticed yet
	}
	r.sender, c.room, c.peerID = c, r, "sender"
	ids := make([]string, 0, len(r.receivers))
	for id := range r.receivers {
		ids = append(ids, id)
	}
	c.send(msg(map[string]any{"t": "room_resumed", "room_id": r.id, "peers": ids}))
}

func (h *Hub) join(c *Client, m in) {
	if m.V != Version {
		c.send(errMsg(ErrUnsupported, "protocol v2 required"))
		return
	}
	if c.room != nil {
		c.send(errMsg(ErrBadRequest, "already in a room"))
		return
	}
	if !h.Join.Allow(c.ip) {
		c.send(errMsg(ErrRateLimited, "too many attempts"))
		return
	}
	r := h.rooms[m.RoomID]
	if r == nil || r.sender == nil {
		c.send(errMsg(ErrNotFound, "room not found"))
		return
	}
	if len(r.receivers) >= MaxReceivers {
		c.send(errMsg(ErrRoomFull, "room is full"))
		return
	}
	id := randHex(4)
	r.receivers[id] = c
	c.room, c.peerID = r, id
	c.send(msg(map[string]any{"t": "joined", "peer_id": id}))
	r.sender.send(msg(map[string]any{"t": "peer_joined", "peer_id": id, "client": m.Client}))
}

// peer resolves the destination of a message from c: a receiver always talks to the sender.
func (h *Hub) peer(c *Client, to string) *Client {
	r := c.room
	if r == nil {
		return nil
	}
	if r.sender == c {
		return r.receivers[to]
	}
	return r.sender
}

func (h *Hub) signal(c *Client, m in) {
	dst := h.peer(c, m.To)
	if dst == nil || len(m.Payload) == 0 {
		c.send(errMsg(ErrBadRequest, "no such peer"))
		return
	}
	dst.send(msg(map[string]any{"t": "signal", "from": c.peerID, "payload": m.Payload}))
}

func (h *Hub) relayRequest(c *Client, m in) {
	if h.Tickets == nil {
		c.send(errMsg(ErrRelayOff, "relay disabled"))
		return
	}
	dst := h.peer(c, m.To)
	if dst == nil {
		c.send(errMsg(ErrBadRequest, "no such peer"))
		return
	}
	if !h.Relay.Allow(c.ip) {
		c.send(errMsg(ErrRateLimited, "too many relay requests"))
		return
	}
	t := h.Tickets.Issue()
	c.send(msg(map[string]any{"t": "relay_ticket", "peer_id": dst.peerID, "ticket": t}))
	dst.send(msg(map[string]any{"t": "relay_ticket", "peer_id": c.peerID, "ticket": t}))
}

// leave runs once per connection after its socket is gone.
func (h *Hub) leave(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := c.room
	if r == nil {
		return
	}
	c.room = nil
	if r.sender == c {
		r.sender = nil
		r.grace = time.AfterFunc(h.Grace, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.rooms[r.id] == r && r.sender == nil {
				h.closeRoom(r)
			}
		})
		return
	}
	delete(r.receivers, c.peerID)
	if r.sender != nil {
		r.sender.send(msg(map[string]any{"t": "peer_left", "peer_id": c.peerID}))
	}
}

// closeRoom must be called with h.mu held.
func (h *Hub) closeRoom(r *Room) {
	delete(h.rooms, r.id)
	if r.grace != nil {
		r.grace.Stop()
	}
	bye := msg(map[string]any{"t": "room_closed"})
	if r.sender != nil {
		r.sender.room = nil
		r.sender.send(bye)
	}
	for _, rc := range r.receivers {
		rc.room = nil
		rc.send(bye)
	}
	slog.Debug("room closed", "receivers", len(r.receivers))
}

// Stats is used by tests and health output.
func (h *Hub) Rooms() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}
