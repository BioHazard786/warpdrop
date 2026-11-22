// Package nearby is the client for the server's same-network device list: who else is online on this network,
// and invites to start a transfer with one of them. The transfer itself still runs through internal/transfer.
package nearby

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/BioHazard786/Warpdrop/cli/internal/lan"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
	"github.com/BioHazard786/Warpdrop/cli/internal/signaling"
)

// Device is another client in the list. ID is only valid for this lobby connection ("lan:..." when the device was
// found on the local network but is not in the server's list). LAN is set when it can also be reached directly.
type Device struct {
	ID   string
	Name string
	LAN  *lan.Peer
}

// InLobby says whether the server can invite this device.
func (d Device) InLobby() bool { return !strings.HasPrefix(d.ID, "lan:") }

// Merge combines the server's list with devices found on the local network. One device in both lists (same name)
// appears once, reachable either way: callers prefer the direct route.
// shortcut: matched by name because the server does not know a device's mDNS id; two devices with the same name
// on one network collapse into one entry, add the id to the lobby join message if that bites.
func Merge(lobby []Device, peers []lan.Peer) []Device {
	out := append([]Device(nil), lobby...)
	byName := map[string]int{}
	for i, d := range out {
		byName[strings.ToLower(d.Name)] = i
	}
	for _, p := range peers {
		if i, ok := byName[strings.ToLower(p.Name)]; ok && out[i].LAN == nil {
			out[i].LAN = &p
			continue
		}
		out = append(out, Device{ID: "lan:" + p.ID, Name: p.Name, LAN: &p})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || (out[i].Name == out[j].Name && out[i].ID < out[j].ID)
	})
	return out
}

// Event is a change in the list, or an invite to receive from Device.
type Event struct {
	Kind   string // "joined" | "left" | "invite"
	Device Device
	// invite only: pass both to transfer.RunReceiveNearby
	RoomID, Ticket string
}

// Lobby is a live membership in the network's device list. Events is closed when the connection ends.
type Lobby struct {
	Events <-chan Event
	sig    *signaling.Client
	mu     sync.Mutex
	list   map[string]Device
}

type wireDevice struct {
	PeerID string `json:"peer_id"`
	Device string `json:"device"`
}

func (w wireDevice) device() Device {
	name := protocol.DisplayName(w.Device)
	if name == "" {
		name = "device"
	}
	return Device{ID: w.PeerID, Name: name}
}

// Join enters the network's device list under the given name. With hidden it can see and invite others, but is
// not listed to them (so it cannot be invited).
func Join(ctx context.Context, cfg config.Config, device string, hidden bool) (*Lobby, error) {
	sig, err := signaling.Dial(ctx, cfg.WSURL("/ws?lobby=1"))
	if err != nil {
		return nil, fmt.Errorf("cannot connect to the server: %w", err)
	}
	if err := sig.Send(map[string]any{"t": "nearby_join", "v": 2, "device": device, "hidden": hidden, "client": map[string]any{"device": device}}); err != nil {
		sig.Close()
		return nil, err
	}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case m, ok := <-sig.In:
			if !ok {
				return nil, errors.New("the server closed the connection")
			}
			switch m.T {
			case "nearby_joined":
				return start(ctx, sig, m), nil
			case "error":
				sig.Close()
				return nil, fmt.Errorf("server: %s", m.Message)
			}
		case <-timeout:
			sig.Close()
			return nil, errors.New("the server did not answer")
		case <-ctx.Done():
			sig.Close()
			return nil, ctx.Err()
		}
	}
}

func start(ctx context.Context, sig *signaling.Client, joined signaling.Message) *Lobby {
	events := make(chan Event, 16)
	l := &Lobby{Events: events, sig: sig, list: map[string]Device{}}
	var first []wireDevice
	_ = json.Unmarshal(joined.Devices, &first)
	for _, w := range first {
		l.list[w.PeerID] = w.device()
	}
	emit := func(e Event) {
		select {
		case events <- e:
		case <-ctx.Done():
		}
	}
	go func() {
		defer close(events)
		for m := range sig.In {
			var w wireDevice
			switch m.T {
			case "nearby_peer":
				var p wireDevice
				if json.Unmarshal(m.Peer, &p) == nil {
					d := p.device()
					l.mu.Lock()
					l.list[d.ID] = d
					l.mu.Unlock()
					emit(Event{Kind: "joined", Device: d})
				}
			case "nearby_peer_left":
				l.mu.Lock()
				d, ok := l.list[m.PeerID]
				delete(l.list, m.PeerID)
				l.mu.Unlock()
				if ok {
					emit(Event{Kind: "left", Device: d})
				}
			case "nearby_invite":
				if json.Unmarshal(m.Peer, &w) == nil && m.RoomID != "" && m.Ticket != "" {
					emit(Event{Kind: "invite", Device: w.device(), RoomID: m.RoomID, Ticket: m.Ticket})
				}
			}
		}
	}()
	return l
}

// Devices lists everyone currently visible, sorted by name.
func (l *Lobby) Devices() []Device {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Device, 0, len(l.list))
	for _, d := range l.list {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || (out[i].Name == out[j].Name && out[i].ID < out[j].ID)
	})
	return out
}

// Invite asks the device to join the room this client just created (roomID and token come from room_created).
func (l *Lobby) Invite(deviceID, roomID, token string) error {
	return l.sig.Send(map[string]any{"t": "nearby_invite", "to": deviceID, "room_id": roomID, "token": token})
}

// Decline turns an invite down so the sender stops waiting. reason is "declined" or "busy".
func (l *Lobby) Decline(roomID, ticket, reason string) error {
	return l.sig.Send(map[string]any{"t": "nearby_decline", "room_id": roomID, "ticket": ticket, "reason": reason})
}

// Close removes this client from the list.
func (l *Lobby) Close() { l.sig.Close() }
