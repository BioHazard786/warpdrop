package signaling

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/netip"
)

// MaxLobby caps how many devices one network's lobby lists.
const MaxLobby = 50

// nearbyPeer is a client that is visible in its network's lobby.
type nearbyPeer struct {
	id, net, device string
	client          json.RawMessage
	hidden          bool // sees the list and can send, but is not listed and cannot be invited
}

// NetworkKey groups clients that probably share a LAN: the same public IPv4, the same IPv6 /64, or any private,
// loopback or link-local address (a server that sees those is itself on the LAN, e.g. self-hosted at home).
// shortcut: same public IP also groups unrelated users behind one carrier-grade NAT or office; upgrade to
// opt-in rooms if that gets noisy.
func NetworkKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip // unknown format: a lobby of one
	}
	a = a.Unmap()
	switch {
	case a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast():
		return "lan"
	case a.Is6():
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}

func (h *Hub) nearbyJoin(c *Client, m in) {
	if m.V != Version {
		c.send(errMsg(ErrUnsupported, "protocol v2 required"))
		return
	}
	if c.room != nil || c.lobby != nil || len(m.Device) > 128 {
		c.send(errMsg(ErrBadRequest, "bad nearby join"))
		return
	}
	if !h.Lobby.Allow(c.ip) {
		slog.Warn("rate limited", "action", "nearby_join", "ip", c.ip)
		c.send(errMsg(ErrRateLimited, "too many attempts"))
		return
	}
	net := NetworkKey(c.ip)
	l := h.lobbies[net]
	if len(l) >= MaxLobby {
		c.send(errMsg(ErrRoomFull, "too many devices on this network"))
		return
	}
	if l == nil {
		l = map[string]*Client{}
		h.lobbies[net] = l
	}
	p := &nearbyPeer{id: randHex(4), net: net, device: m.Device, client: m.Client, hidden: m.Hidden}
	devices := make([]map[string]any, 0, len(l))
	for _, o := range l {
		if !o.lobby.hidden {
			devices = append(devices, peerInfo(o.lobby))
		}
	}
	l[p.id], c.lobby = c, p
	slog.Info("nearby joined", "ip", c.ip, "peers", len(l))
	c.send(msg(map[string]any{"t": "nearby_joined", "peer_id": p.id, "devices": devices}))
	if !p.hidden {
		joined := msg(map[string]any{"t": "nearby_peer", "peer": peerInfo(p)})
		for id, o := range l {
			if id != p.id {
				o.send(joined)
			}
		}
	}
}

func peerInfo(p *nearbyPeer) map[string]any {
	return map[string]any{"peer_id": p.id, "device": p.device, "client": p.client}
}

// nearbyLeave removes c from its lobby (it asked to hide, or its socket is gone).
func (h *Hub) nearbyLeave(c *Client) {
	p := c.lobby
	if p == nil {
		return
	}
	c.lobby = nil
	l := h.lobbies[p.net]
	delete(l, p.id)
	if len(l) == 0 {
		delete(h.lobbies, p.net)
		return
	}
	if p.hidden {
		return
	}
	gone := msg(map[string]any{"t": "nearby_peer_left", "peer_id": p.id})
	for _, o := range l {
		o.send(gone)
	}
}

// nearbyInvite asks lobby peer m.To to join the sender's room. Only the room's creator (it holds the token) can
// invite, and the room must have been created with nearby:true so nobody can join it without a ticket.
func (h *Hub) nearbyInvite(c *Client, m in) {
	p := c.lobby
	if p == nil || m.To == p.id {
		c.send(errMsg(ErrBadRequest, "join the nearby list first"))
		return
	}
	dst := h.lobbies[p.net][m.To]
	r := h.rooms[m.RoomID]
	if dst == nil || dst.lobby.hidden || r == nil || !r.gated || subtle.ConstantTimeCompare([]byte(r.token), []byte(m.Token)) != 1 {
		c.send(errMsg(ErrNotFound, "peer or room not found"))
		return
	}
	if len(r.tickets) >= MaxReceivers {
		c.send(errMsg(ErrRoomFull, "too many pending invites"))
		return
	}
	if !h.Invite.Allow(c.ip) {
		slog.Warn("rate limited", "action", "nearby_invite", "ip", c.ip)
		c.send(errMsg(ErrRateLimited, "too many invites"))
		return
	}
	ticket := randHex(8)
	r.tickets[ticket] = m.To
	dst.send(msg(map[string]any{"t": "nearby_invite", "peer": peerInfo(p), "room_id": r.id, "ticket": ticket}))
}

// nearbyDecline turns an invite down: the ticket is spent and the room's sender hears invite_declined with the
// lobby id it invited, so it can stop waiting. Only whoever holds the ticket can do this.
func (h *Hub) nearbyDecline(c *Client, m in) {
	p := c.lobby
	r := h.rooms[m.RoomID]
	invited, ok := "", false
	if r != nil {
		invited, ok = r.tickets[m.Ticket]
	}
	if p == nil || !ok || !r.gated {
		return // nothing to tell: the room or invite is already gone
	}
	delete(r.tickets, m.Ticket)
	reason := "declined"
	if m.Reason == "busy" {
		reason = "busy"
	}
	if r.sender != nil {
		r.sender.send(msg(map[string]any{"t": "invite_declined", "peer_id": invited, "device": p.device, "reason": reason}))
	}
}
