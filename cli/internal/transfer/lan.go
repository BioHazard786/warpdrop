package transfer

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
)

// A LAN transfer runs the nearby handshake (no secret, both screens show a code) straight over TCP between two
// devices on the same network. The connecting side is the sender and the listening side is the receiver.
// shortcut: no resume after a dropped connection, start the transfer again; resuming needs a reconnect protocol.

const (
	lanRoom     = "lan"
	lanCooldown = 30 * time.Second // after an offer is turned down, ignore that address for this long
	lanMax      = 3                // devices handled at once; more are turned away
)

// ErrBusy: the receiving device turned the connection away before the handshake (busy, or cooling down after a
// declined offer).
var ErrBusy = errors.New("device is busy")

// RunSendLAN offers files to the device at the other end of c. The code to compare is in the PeerView with
// Status "waiting". It returns nil when the receiver got everything, and also when ctx was cancelled.
func RunSendLAN(ctx context.Context, c net.Conn, peerName string, files []protocol.Source, device string, h SendHooks) error {
	if len(files) == 0 {
		c.Close()
		return errors.New("no files to send")
	}
	var mu sync.Mutex
	view := PeerView{ID: "lan", Device: peerName, Transport: "lan", Status: "authenticating"}
	var prefix []int64
	var entries []protocol.FileEntry
	for _, f := range files {
		prefix = append(prefix, view.Total)
		view.Total += f.Entry.Size
		entries = append(entries, f.Entry)
	}
	update := func(f func(*PeerView)) {
		mu.Lock()
		defer mu.Unlock()
		f(&view)
		if h.OnPeer != nil {
			h.OnPeer(view)
		}
	}
	update(func(*PeerView) {})
	var handshook atomic.Bool
	t := protocol.NewLANTransport(c)
	defer t.Close()
	res := protocol.RunSender(ctx, t, protocol.SenderOptions{
		RoomID: lanRoom, Device: device, Files: files,
		OnProgress: func(idx int, b int64) { update(func(v *PeerView) { v.Sent, v.Status = prefix[idx]+b, "sending" }) },
		OnFileDone: func(int) { update(func(v *PeerView) { v.FilesDone++ }) },
		OnOffered:  func() { update(func(v *PeerView) { v.Status = "waiting" }) },
		OnSAS: func(sas string) {
			handshook.Store(true)
			update(func(v *PeerView) { v.SAS = sas })
		},
	})
	switch res.Status {
	case protocol.StatusDone:
		update(func(v *PeerView) { v.Status = "done" })
		return nil
	case protocol.StatusClosed:
		if ctx.Err() != nil {
			return nil
		}
		res = protocol.Result{Status: protocol.StatusFailed, Reason: protocol.ReasonClosed}
	}
	if ctx.Err() != nil {
		return nil
	}
	err := failureErr(res)
	if res.Reason == protocol.ReasonClosed && !handshook.Load() {
		err = ErrBusy // closed before a word was said: ServeLAN turned us away
	}
	update(func(v *PeerView) { v.Status, v.Reason = "failed", err.Error() })
	return err
}

// ReceiveLAN handles one connection from a device that wants to send to us: h.OnSAS and h.OnOffer fire as for an
// invite from the nearby list. err is nil only when every file arrived and verified; offered says whether the
// user was asked (ServeLAN uses it to ignore a device that keeps pestering).
func ReceiveLAN(ctx context.Context, c net.Conn, device string, sink protocol.Sink, h ReceiveHooks) (offered bool, err error) {
	state := &protocol.ReceiveState{}
	defer func() {
		if err != nil {
			state.Discard()
		}
	}()
	t := protocol.NewLANTransport(c)
	defer t.Close()
	ts := &timedSink{Sink: sink}
	defer func() {
		if ts.bytes > 0 { // one line per transfer, to tell a slow disk from a slow network
			sec := time.Since(ts.first).Seconds() // from the first byte: the user may take a while to accept
			slog.Info("lan receive", "bytes", ts.bytes, "seconds", sec, "write_seconds", ts.spent.Seconds(),
				"mb_per_s", float64(ts.bytes)/1e6/sec, "err", err)
		}
	}()
	status := func(s string) {
		if h.OnStatus != nil {
			h.OnStatus(s, "lan")
		}
	}
	status("authenticating")
	res := protocol.RunReceiver(ctx, t, protocol.ReceiverOptions{
		RoomID: lanRoom, Device: device, Sink: ts, State: state,
		OnOffer: func(files []protocol.NamedFile) bool {
			offered = true
			status("offered")
			if h.OnSAS != nil && state.SAS != "" {
				h.OnSAS(state.SAS)
			}
			ok := h.OnOffer(state.Sender, files)
			if ok {
				status("receiving")
			}
			return ok
		},
		OnProgress: h.OnProgress,
		OnFileDone: h.OnFileDone,
	})
	switch res.Status {
	case protocol.StatusDone:
		protocol.WaitPeerClose(t, 2*time.Second) // let our final file_done reach the sender before we hang up
		return offered, nil
	case protocol.StatusClosed:
		if ctx.Err() != nil {
			return offered, ctx.Err()
		}
		res = protocol.Result{Status: protocol.StatusFailed, Reason: protocol.ReasonClosed}
	}
	return offered, failureErr(res)
}

// ServeLAN calls handle for each connection on ln until ctx ends, up to lanMax at a time: more are closed at once
// (the sender reports ErrBusy), so a flood of connections cannot pile up prompts. A device whose offer was turned
// down or cancelled (handle returned offered and an error) is ignored for a while.
func ServeLAN(ctx context.Context, ln net.Listener, handle func(net.Conn) (offered bool, err error)) {
	go func() { <-ctx.Done(); ln.Close() }()
	slots := make(chan struct{}, lanMax)
	var mu sync.Mutex
	until := map[string]time.Time{}
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		mu.Lock()
		cooling := time.Now().Before(until[host])
		mu.Unlock()
		if cooling {
			c.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			c.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			if offered, err := handle(c); err != nil && offered {
				mu.Lock()
				until[host] = time.Now().Add(lanCooldown)
				mu.Unlock()
			}
		}()
	}
}

// timedSink counts bytes and the time spent writing them.
type timedSink struct {
	protocol.Sink
	first time.Time
	bytes int64
	spent time.Duration
}

func (s *timedSink) Open(f protocol.NamedFile, idx int) (protocol.FileWriter, error) {
	w, err := s.Sink.Open(f, idx)
	if err != nil {
		return nil, err
	}
	return timedWriter{w, s}, nil
}

type timedWriter struct {
	protocol.FileWriter
	s *timedSink
}

func (w timedWriter) Write(p []byte) error {
	t := time.Now()
	if w.s.first.IsZero() {
		w.s.first = t
	}
	err := w.FileWriter.Write(p)
	w.s.spent += time.Since(t)
	w.s.bytes += int64(len(p))
	return err
}
