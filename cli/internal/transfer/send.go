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

const maxAuthFailures = 3

// lastRoom lets tests sever live transports like a network drop would.
var lastRoom atomic.Pointer[room]

// PeerView is a snapshot of one receiver for the UI.
type PeerView struct {
	ID        string
	Device    string
	Transport string // "", "rtc" or "relay"
	Status    string // connecting | authenticating | sending | done | failed
	Reason    string
	Sent      int64
	Total     int64
	FilesDone int
}

type SendHooks struct {
	OnRoom     func(roomID, secret string)
	OnPeer     func(PeerView)
	OnPeerGone func(id string)
	OnStatus   func(msg string)
}

var reasonText = map[protocol.Reason]string{
	protocol.ReasonDeclined:      "receiver declined",
	protocol.ReasonUser:          "cancelled",
	protocol.ReasonAuthFailed:    "wrong secret words",
	protocol.ReasonHashMismatch:  "file arrived corrupted",
	protocol.ReasonProtocolError: "protocol error",
	protocol.ReasonClosed:        "connection lost",
}

type room struct {
	ctx    context.Context
	cfg    config.Config
	srv    config.Server
	files  []protocol.Source
	prefix []int64 // bytes in files before i; files are served in order
	total  int64
	device string
	secret string
	h      SendHooks

	mu     sync.Mutex // guards sig, roomID, token, links
	sig    *signaling.Client
	roomID string
	token  string
	links  map[string]*link

	authFails atomic.Int32
	fatal     chan error
}

// RunSend creates a room and serves every receiver that joins until ctx is cancelled or the room dies.
// Cancelling ctx tells active receivers "cancelled" and closes the room.
func RunSend(ctx context.Context, cfg config.Config, srv config.Server, files []protocol.Source, device string, h SendHooks) error {
	var entries []protocol.FileEntry
	r := &room{ctx: ctx, cfg: cfg, srv: srv, files: files, device: device, secret: protocol.GenerateSecret(), h: h,
		links: map[string]*link{}, fatal: make(chan error, 1)}
	for _, f := range files {
		r.prefix = append(r.prefix, r.total)
		r.total += f.Entry.Size
		entries = append(entries, f.Entry)
	}
	if len(entries) == 0 {
		return errors.New("no files to send")
	}
	if b, _ := json.Marshal(protocol.NewManifest(entries)); len(b) > protocol.MaxManifestBytes || len(entries) > protocol.MaxFiles {
		return errors.New("too many files for one transfer: put them in a zip first")
	}
	if err := r.dial(false); err != nil {
		return err
	}
	lastRoom.Store(r)
	defer r.shutdown()
	for {
		r.mu.Lock()
		in := r.sig.In
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case err := <-r.fatal:
			return err
		case m, ok := <-in:
			if !ok {
				if err := r.reconnect(); err != nil {
					return err
				}
				continue
			}
			if err := r.handle(m); err != nil {
				return err
			}
		}
	}
}

func (r *room) say(msg string) {
	if r.h.OnStatus != nil {
		r.h.OnStatus(msg)
	}
}

func (r *room) dial(resume bool) error {
	sig, err := signaling.Dial(r.ctx, r.cfg.WSURL("/ws"))
	if err != nil {
		return fmt.Errorf("cannot connect to the server: %w", err)
	}
	r.mu.Lock()
	r.sig = sig
	id, tok := r.roomID, r.token
	r.mu.Unlock()
	if resume && id != "" {
		return sig.Send(map[string]any{"t": "resume_room", "room_id": id, "token": tok})
	}
	return sig.Send(map[string]any{"t": "create_room", "v": 2, "client": map[string]any{"device": r.device}})
}

// reconnect redials with backoff and resumes the room; receivers on a live P2P link are unaffected meanwhile.
func (r *room) reconnect() error {
	r.say("connection to the server lost, reconnecting...")
	for attempt := 0; attempt < 10; attempt++ {
		select {
		case <-r.ctx.Done():
			return nil
		case <-time.After(min(8*time.Second, 500*time.Millisecond<<attempt)):
		}
		if r.dial(true) == nil {
			return nil
		}
	}
	return errors.New("lost the connection to the server")
}

func (r *room) send(v any) {
	r.mu.Lock()
	sig := r.sig
	r.mu.Unlock()
	if sig != nil {
		_ = sig.Send(v)
	}
}

func (r *room) handle(m signaling.Message) error {
	switch m.T {
	case "room_created":
		r.mu.Lock()
		r.roomID, r.token = m.RoomID, m.Token
		r.mu.Unlock()
		if r.h.OnRoom != nil {
			r.h.OnRoom(m.RoomID, r.secret)
		}
	case "room_resumed":
		r.say("reconnected")
	case "peer_joined":
		var c struct{ Device string }
		_ = json.Unmarshal(m.Client, &c)
		if len(c.Device) > 64 {
			c.Device = c.Device[:64]
		}
		if c.Device == "" {
			c.Device = "receiver"
		}
		l := newLink(r, m.PeerID, protocol.DisplayName(c.Device))
		r.mu.Lock()
		old := r.links[m.PeerID]
		r.links[m.PeerID] = l
		r.mu.Unlock()
		if old != nil {
			old.dispose(false)
		}
		l.startRTC()
	case "peer_left":
		if l := r.link(m.PeerID); l != nil {
			l.onSignalingLeft()
		}
	case "signal":
		var s protocol.Signal
		if json.Unmarshal(m.Payload, &s) == nil {
			if l := r.link(m.From); l != nil {
				l.onSignal(s)
			}
		}
	case "relay_ticket":
		if l := r.link(m.PeerID); l != nil {
			go l.openRelay(m.Ticket)
		}
	case "room_closed":
		return errors.New("the room was closed")
	case "error":
		switch m.Code {
		case "not_found":
			return errors.New("the share link expired")
		case "rate_limited":
			return errors.New("too many requests: wait a minute and try again")
		}
		return fmt.Errorf("server error: %s", m.Message)
	}
	return nil
}

func (r *room) link(id string) *link {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.links[id]
}

func (r *room) authFailed() {
	if int(r.authFails.Add(1)) >= maxAuthFailures {
		r.send(map[string]any{"t": "close_room"})
		select {
		case r.fatal <- errors.New("too many failed attempts: the room was closed"):
		default:
		}
	}
}

func (r *room) shutdown() {
	r.mu.Lock()
	links := make([]*link, 0, len(r.links))
	for _, l := range r.links {
		links = append(links, l)
	}
	r.mu.Unlock()
	r.send(map[string]any{"t": "close_room"})
	for _, l := range links {
		l.dispose(false)
	}
	r.mu.Lock()
	if r.sig != nil {
		r.sig.Close()
	}
	r.mu.Unlock()
}

// ------------------------------------------------------------------ one receiver

type link struct {
	r      *room
	id     string
	mu     sync.Mutex
	rtc    *protocol.RTCPeer
	tr     protocol.Transport
	gen    int // bumped on every new transport so stale session results are ignored
	gone   bool
	closed bool
	view   PeerView
}

func newLink(r *room, id, device string) *link {
	l := &link{r: r, id: id, view: PeerView{ID: id, Device: device, Status: "connecting", Total: r.total}}
	l.emit()
	return l
}

func (l *link) emit() {
	if l.r.h.OnPeer != nil && !l.closed {
		l.r.h.OnPeer(l.view)
	}
}

func (l *link) update(f func(*PeerView)) {
	l.mu.Lock()
	f(&l.view)
	v := l.view
	closed := l.closed
	l.mu.Unlock()
	if l.r.h.OnPeer != nil && !closed {
		l.r.h.OnPeer(v)
	}
}

func (l *link) startRTC() {
	peer, err := protocol.NewRTCPeer(protocol.RTCPeerOptions{
		ICEServers: l.r.srv.ICE(),
		Offerer:    true,
		SendSignal: func(s protocol.Signal) { l.r.send(map[string]any{"t": "signal", "to": l.id, "payload": s}) },
	})
	if err != nil {
		return // the receiver will ask for the relay
	}
	l.mu.Lock()
	old := l.rtc
	l.rtc = peer
	l.mu.Unlock()
	if old != nil {
		old.Close()
	}
	go func() {
		if peer.Start() != nil {
			return
		}
		if t, err := peer.Ready(l.r.ctx, 30*time.Second); err == nil {
			l.attach(t, peer)
		}
	}()
}

func (l *link) onSignal(s protocol.Signal) {
	if s.Kind == "restart" {
		l.mu.Lock()
		l.gen++ // whatever session is running is about to be replaced
		l.mu.Unlock()
		l.startRTC()
		return
	}
	l.mu.Lock()
	p := l.rtc
	l.mu.Unlock()
	if p != nil {
		p.OnSignal(s)
	}
}

func (l *link) openRelay(ticket string) {
	t, err := protocol.DialRelay(l.r.ctx, l.r.cfg.WSURL("/relay/"+ticket))
	if err != nil {
		l.update(func(v *PeerView) { v.Status, v.Reason = "failed", "relay connection failed" })
		return
	}
	l.attach(t, nil)
}

func (l *link) attach(t protocol.Transport, from *protocol.RTCPeer) {
	l.mu.Lock()
	if l.closed || (from != nil && l.rtc != from) { // disposed, or an RTC attempt that was replaced
		l.mu.Unlock()
		_ = t.Close()
		return
	}
	old := l.tr
	l.tr = t
	l.gen++
	gen := l.gen
	if t.Kind() == "relay" && l.rtc != nil {
		l.rtc.Close()
		l.rtc = nil
	}
	l.mu.Unlock()
	if old != nil {
		_ = old.Close() // replaced: its session ends "closed" and is ignored below
	}
	kind := "rtc"
	if t.Kind() == "relay" {
		kind = "relay"
	}
	l.update(func(v *PeerView) { v.Transport, v.Status = kind, "authenticating" })

	go func() {
		res := protocol.RunSender(l.r.ctx, t, protocol.SenderOptions{
			RoomID: l.r.roomID, Secret: l.r.secret, Device: l.r.device, Files: l.r.files,
			OnProgress: func(idx int, b int64) {
				l.update(func(v *PeerView) { v.Sent, v.Status = l.r.prefix[idx]+b, "sending" })
			},
			OnFileDone: func(int) { l.update(func(v *PeerView) { v.FilesDone++ }) },
		})
		l.mu.Lock()
		stale, gone := l.gen != gen || l.closed, l.gone
		l.mu.Unlock()
		if stale {
			return
		}
		switch res.Status {
		case protocol.StatusDone:
			l.update(func(v *PeerView) { v.Status = "done" })
		case protocol.StatusClosed:
			if gone {
				l.dispose(true)
			} else { // wait for the receiver to reconnect and resume
				l.update(func(v *PeerView) { v.Status, v.Transport = "connecting", "" })
			}
		default:
			if res.Reason == protocol.ReasonAuthFailed {
				l.r.authFailed()
			}
			why := reasonText[res.Reason]
			l.update(func(v *PeerView) { v.Status, v.Reason = "failed", why })
		}
	}()
}

// onSignalingLeft: the receiver's signaling socket is gone. A live session must outlive that.
func (l *link) onSignalingLeft() {
	l.mu.Lock()
	l.gone = true
	idle := l.tr == nil || l.view.Status == "connecting"
	l.mu.Unlock()
	if idle {
		l.dispose(true)
	}
}

func (l *link) dispose(notify bool) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	rtc, tr := l.rtc, l.tr
	l.rtc, l.tr = nil, nil
	l.mu.Unlock()
	if rtc != nil {
		rtc.Close()
	}
	if tr != nil {
		_ = tr.Close()
	}
	l.r.mu.Lock()
	if l.r.links[l.id] == l {
		delete(l.r.links, l.id)
	}
	l.r.mu.Unlock()
	if notify && l.r.h.OnPeerGone != nil {
		l.r.h.OnPeerGone(l.id)
	}
}

// dropAll closes every live transport without telling anyone (test hook).
func (r *room) dropAll() {
	r.mu.Lock()
	var ts []protocol.Transport
	for _, l := range r.links {
		l.mu.Lock()
		if l.tr != nil {
			ts = append(ts, l.tr)
		}
		l.mu.Unlock()
	}
	r.mu.Unlock()
	for _, t := range ts {
		_ = t.Close()
	}
}
