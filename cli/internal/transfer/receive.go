package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
	"github.com/BioHazard786/Warpdrop/cli/internal/signaling"
)

const (
	rtcTimeout    = 8 * time.Second
	maxReconnects = 5
)

type ReceiveHooks struct {
	OnStatus   func(status, transport string) // connecting | negotiating | authenticating | offered | receiving | reconnecting
	OnOffer    func(files []protocol.NamedFile) bool
	OnProgress func(idx int, bytes int64)
	OnFileDone func(idx int)
}

var serverErrors = map[string]string{
	"not_found":           "no transfer with that code: it may have finished or expired",
	"room_full":           "this transfer already has the maximum number of receivers",
	"rate_limited":        "too many attempts: wait a minute and try again",
	"unsupported_version": "this link was made by an incompatible WarpDrop version",
}

type receiveClient struct {
	cfg    config.Config
	srv    config.Server
	roomID string
	secret string
	device string
	h      ReceiveHooks

	sig     *signaling.Client
	sigMu   sync.Mutex
	dead    atomic.Bool
	rtc     atomic.Pointer[protocol.RTCPeer]
	joined  chan struct{}
	ticket  chan string
	errMsg  chan string
	closed  atomic.Bool // the sender closed the room
	lastErr atomic.Value
}

// RunReceive joins a room and receives its files into sink. It returns nil only when every file arrived and verified.
func RunReceive(ctx context.Context, cfg config.Config, srv config.Server, roomID, secret, device string, sink protocol.Sink, h ReceiveHooks) error {
	c := &receiveClient{cfg: cfg, srv: srv, roomID: roomID, secret: secret, device: device, h: h,
		joined: make(chan struct{}, 1), ticket: make(chan string, 1), errMsg: make(chan string, 4)}
	c.status("connecting", "")
	defer func() {
		if p := c.rtc.Load(); p != nil {
			p.Close()
		}
		c.sigMu.Lock()
		if c.sig != nil {
			c.sig.Close()
		}
		c.sigMu.Unlock()
	}()
	if err := c.join(ctx); err != nil {
		return err
	}
	state := &protocol.ReceiveState{}
	for attempt := 0; ; attempt++ {
		resuming := attempt > 0
		if resuming && c.dead.Load() {
			if err := c.join(ctx); err != nil { // the signaling socket dropped meanwhile: rejoin (new peer id)
				return err
			}
		}
		t, err := c.establish(ctx, resuming)
		if err != nil {
			return err
		}
		c.status("authenticating", t.Kind())
		res := protocol.RunReceiver(ctx, t, protocol.ReceiverOptions{
			RoomID: roomID, Secret: secret, Device: device, Sink: sink, State: state,
			OnOffer: func(files []protocol.NamedFile) bool {
				c.status("offered", t.Kind())
				ok := h.OnOffer(files)
				if ok {
					c.status("receiving", t.Kind())
				}
				return ok
			},
			OnProgress: h.OnProgress,
			OnFileDone: h.OnFileDone,
		})
		if res.Status == protocol.StatusDone {
			protocol.WaitPeerClose(t, 2*time.Second) // let our final file_done reach the sender before we hang up
		}
		_ = t.Close()
		switch res.Status {
		case protocol.StatusDone:
			return nil
		case protocol.StatusFailed:
			if res.Err != nil {
				return fmt.Errorf("%s: %w", reasonText[res.Reason], res.Err)
			}
			if res.Reason == protocol.ReasonAuthFailed {
				return ErrWrongSecret
			}
			return errors.New(reasonText[res.Reason])
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt >= maxReconnects {
			return errors.New("the connection kept dropping")
		}
	}
}

// ErrWrongSecret means the words did not match the sender's.
var ErrWrongSecret = errors.New("wrong secret words: the code does not match the sender's")

func (c *receiveClient) status(s, transport string) {
	if c.h.OnStatus != nil {
		c.h.OnStatus(s, transport)
	}
}

func (c *receiveClient) dial(ctx context.Context) error {
	sig, err := signaling.Dial(ctx, c.cfg.WSURL("/ws"))
	if err != nil {
		return fmt.Errorf("cannot connect to the server: %w", err)
	}
	c.sigMu.Lock()
	if c.sig != nil {
		c.sig.Close()
	}
	c.sig = sig
	c.sigMu.Unlock()
	c.dead.Store(false)
	go c.pump(sig)
	return nil
}

func (c *receiveClient) send(v any) {
	c.sigMu.Lock()
	sig := c.sig
	c.sigMu.Unlock()
	if sig != nil {
		_ = sig.Send(v)
	}
}

func (c *receiveClient) pump(sig *signaling.Client) {
	for m := range sig.In {
		switch m.T {
		case "joined":
			select {
			case c.joined <- struct{}{}:
			default:
			}
		case "signal":
			var s protocol.Signal
			if p := c.rtc.Load(); p != nil && json.Unmarshal(m.Payload, &s) == nil {
				p.OnSignal(s)
			}
		case "relay_ticket":
			select {
			case c.ticket <- m.Ticket:
			default:
			}
		case "room_closed":
			c.closed.Store(true)
		case "error":
			msg := serverErrors[m.Code]
			if msg == "" {
				msg = m.Message
			}
			select {
			case c.errMsg <- msg:
			default:
			}
		}
	}
	c.sigMu.Lock()
	if c.sig == sig {
		c.dead.Store(true)
	}
	c.sigMu.Unlock()
}

func (c *receiveClient) join(ctx context.Context) error {
	if err := c.dial(ctx); err != nil {
		return err
	}
	c.send(map[string]any{"t": "join_room", "v": 2, "room_id": c.roomID, "client": map[string]any{"device": c.device}})
	select {
	case <-c.joined:
		return nil
	case msg := <-c.errMsg:
		return errors.New(msg)
	case <-time.After(10 * time.Second):
		return errors.New("the server did not answer")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// establish returns a live transport: direct WebRTC first, the relay if that does not come up in time.
func (c *receiveClient) establish(ctx context.Context, resuming bool) (protocol.Transport, error) {
	if resuming {
		c.status("reconnecting", "")
	} else {
		c.status("negotiating", "")
	}
	if !c.cfg.ForceRelay {
		peer, err := protocol.NewRTCPeer(protocol.RTCPeerOptions{
			ICEServers: c.srv.ICE(),
			SendSignal: func(s protocol.Signal) { c.send(map[string]any{"t": "signal", "to": "sender", "payload": s}) },
		})
		if err == nil {
			c.rtc.Store(peer)
			if resuming {
				c.send(map[string]any{"t": "signal", "to": "sender", "payload": protocol.Signal{Kind: "restart"}}) // sender builds a fresh offer
			}
			if t, err := peer.Ready(ctx, rtcTimeout); err == nil {
				return t, nil
			}
			peer.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if !c.srv.Relay {
		return nil, errors.New("a direct connection could not be made and the relay is disabled on this server")
	}
	c.send(map[string]any{"t": "relay_request", "to": "sender"})
	select {
	case tk := <-c.ticket:
		return protocol.DialRelay(ctx, c.cfg.WSURL("/relay/"+tk))
	case msg := <-c.errMsg:
		return nil, errors.New(msg)
	case <-time.After(10 * time.Second):
		return nil, errors.New("the relay did not respond")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
