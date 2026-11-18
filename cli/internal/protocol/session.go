package protocol

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
)

type Status int

const (
	StatusDone   Status = iota
	StatusFailed        // the transfer ended and will not resume
	StatusClosed        // transport lost; the caller may resume on a new one
)

type Result struct {
	Status Status
	Reason Reason
	ByPeer bool
	Err    error // local cause, when there is one (disk full, ...)
}

func (r Result) String() string {
	switch r.Status {
	case StatusDone:
		return "done"
	case StatusClosed:
		return "closed"
	}
	return fmt.Sprintf("failed(%s,byPeer=%v)", r.Reason, r.ByPeer)
}

func failed(reason Reason, byPeer bool) Result {
	return Result{Status: StatusFailed, Reason: reason, ByPeer: byPeer}
}

// ------------------------------------------------------------------ handshake

const clientName = "cli/2"

// handshake runs the hello/auth exchange. It returns nil on success or the Result to end with.
func handshake(ctx context.Context, role Role, t Transport, roomID, secret, device string) *Result {
	ctx, cancel := context.WithTimeout(ctx, HandshakeTimeout)
	defer cancel()
	next := func() (any, *Result) {
		w, err := t.Recv(ctx)
		if err != nil {
			if errors.Is(err, ErrClosed) {
				return nil, &Result{Status: StatusClosed}
			}
			r := failed(ReasonProtocolError, false) // timeout or cancel
			return nil, &r
		}
		if !w.IsText {
			r := failed(ReasonProtocolError, false)
			return nil, &r
		}
		f, err := ParseFrame(w.Text)
		if err != nil {
			r := failed(ReasonProtocolError, false)
			return nil, &r
		}
		if c, ok := f.(Cancel); ok {
			r := failed(c.Reason, true)
			return nil, &r
		}
		return f, nil
	}
	eph, err := NewEphemeral()
	if err != nil {
		return &Result{Status: StatusFailed, Reason: ReasonProtocolError, Err: err}
	}
	_ = t.SendText(Encode(NewHello(clientName, device, fmt.Sprintf("%x", eph.Public()))))

	f, res := next()
	if res != nil {
		return res
	}
	hello, ok := f.(Hello)
	if !ok || hello.V != Version {
		r := failed(ReasonProtocolError, false)
		return &r
	}
	peerPub, _ := hexDecode(hello.Pub)
	keys, err := DeriveKeys(secret, roomID, eph, peerPub)
	if err != nil {
		r := failed(ReasonProtocolError, false)
		return &r
	}
	senderPub, receiverPub := eph.Public(), peerPub
	senderFp, receiverFp := t.LocalFingerprint(), t.RemoteFingerprint()
	if role == RoleReceiver {
		senderPub, receiverPub = peerPub, eph.Public()
		senderFp, receiverFp = receiverFp, senderFp
	}
	tr := Transcript(senderPub, receiverPub, senderFp, receiverFp)

	if role == RoleReceiver {
		_ = t.SendText(Encode(NewAuth(MAC(keys.Auth, "recv", tr))))
	}
	f, res = next()
	if res != nil {
		return res
	}
	auth, ok := f.(Auth)
	if !ok {
		r := failed(ReasonProtocolError, false)
		return &r
	}
	label := "send"
	if role == RoleSender {
		label = "recv"
	}
	if !VerifyMAC(keys.Auth, label, tr, auth.Mac) {
		_ = t.SendText(Encode(NewCancel(ReasonAuthFailed)))
		r := failed(ReasonAuthFailed, false)
		return &r
	}
	if role == RoleSender {
		_ = t.SendText(Encode(NewAuth(MAC(keys.Auth, "send", tr))))
	}
	if err := t.EnableCrypto(keys.Session, role); err != nil {
		return &Result{Status: StatusFailed, Reason: ReasonProtocolError, Err: err}
	}
	return nil
}

// failHandshake tells the peer why (protocol errors only) and closes, so it never waits out the timeout.
func failHandshake(t Transport, r Result) Result {
	if r.Status == StatusFailed && !r.ByPeer && r.Reason == ReasonProtocolError {
		_ = t.SendText(Encode(NewCancel(ReasonProtocolError)))
	}
	if r.Status != StatusClosed {
		_ = t.Close()
	}
	return r
}

// ------------------------------------------------------------------ sender

type Source struct {
	Entry FileEntry
	R     io.ReaderAt // must stay readable for the whole session
}

type SenderOptions struct {
	RoomID, Secret, Device string
	Files                  []Source
	OnProgress             func(idx int, bytes int64)
	OnFileDone             func(idx int)
}

type sender struct {
	t        Transport
	o        SenderOptions
	acked    atomic.Int64
	ackCh    chan struct{}
	events   chan SEvent
	wg       sync.WaitGroup
	streamMu sync.Mutex
	stop     context.CancelFunc // cancels the in-flight stream
}

// RunSender serves one receiver. Cancelling ctx tells the receiver "user" and ends the session.
func RunSender(ctx context.Context, t Transport, o SenderOptions) Result {
	if res := handshake(ctx, RoleSender, t, o.RoomID, o.Secret, o.Device); res != nil {
		return failHandshake(t, *res)
	}
	entries := make([]FileEntry, len(o.Files))
	for i, f := range o.Files {
		entries[i] = f.Entry
	}
	manifest := Encode(NewManifest(entries))
	if len(manifest) > t.MaxMessage() { // the peer's SCTP/relay limit: it could never receive this frame
		_ = t.SendText(Encode(NewCancel(ReasonProtocolError)))
		_ = t.Close()
		r := failed(ReasonProtocolError, false)
		r.Err = fmt.Errorf("file list (%d bytes) exceeds this connection's message limit (%d): send fewer files or zip them", len(manifest), t.MaxMessage())
		return r
	}
	_ = t.SendText(manifest)

	s := &sender{t: t, o: o, ackCh: make(chan struct{}, 1), events: make(chan SEvent, 16)}
	rctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		s.wg.Wait()
	}()
	s.wg.Add(1)
	go s.readLoop(rctx)

	st := SenderInit(entries)
	for {
		var ev SEvent
		select {
		case ev = <-s.events:
		case <-ctx.Done():
			ev = SEvent{K: SEvCancel}
		}
		var fx []SEffect
		st, fx = SenderStep(st, ev)
		for _, e := range fx {
			if r := s.apply(rctx, e); r != nil {
				return *r
			}
		}
	}
}

func (s *sender) post(ctx context.Context, ev SEvent) {
	select {
	case s.events <- ev:
	case <-ctx.Done():
	}
}

func (s *sender) readLoop(ctx context.Context) {
	defer s.wg.Done()
	for {
		w, err := s.t.Recv(ctx)
		if err != nil {
			if errors.Is(err, ErrClosed) {
				s.post(ctx, SEvent{K: SEvClosed})
			}
			return
		}
		if !w.IsText {
			s.post(ctx, SEvent{K: SEvBad})
			continue
		}
		f, err := ParseFrame(w.Text)
		if err != nil {
			s.post(ctx, SEvent{K: SEvBad})
			continue
		}
		s.post(ctx, SEvent{K: SEvFrame, Frame: f})
	}
}

func (s *sender) apply(ctx context.Context, e SEffect) *Result {
	switch e.K {
	case SFxSend:
		_ = s.t.SendText(Encode(e.Frame))
	case SFxStreamFile:
		s.streamMu.Lock()
		if s.stop != nil {
			s.stop()
		}
		var sctx context.Context
		sctx, s.stop = context.WithCancel(ctx)
		s.streamMu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.streamFile(sctx, e.Idx, e.Offset)
		}()
	case SFxAck:
		s.acked.Store(max(s.acked.Load(), e.Bytes))
		select {
		case s.ackCh <- struct{}{}:
		default:
		}
	case SFxFileAcked:
		if s.o.OnFileDone != nil {
			s.o.OnFileDone(e.Idx)
		}
	case SFxComplete:
		_ = s.t.Close() // the receiver already has everything and has confirmed it; it waits for this close before leaving
		return &Result{Status: StatusDone}
	case SFxAbort:
		if e.Reason == ReasonClosed {
			return &Result{Status: StatusClosed}
		}
		_ = s.t.Close()
		r := failed(e.Reason, e.ByPeer)
		return &r
	}
	return nil
}

func (s *sender) streamFile(ctx context.Context, idx int, offset int64) {
	src := s.o.Files[idx]
	size := src.Entry.Size
	h := xxhash.New()
	s.acked.Store(offset)
	fail := func() {
		if ctx.Err() == nil { // unreadable file: tell the receiver instead of leaving it hanging
			s.post(ctx, SEvent{K: SEvCancel})
		}
	}
	if offset > 0 { // the hash covers the whole file, so re-read what the receiver already holds
		if _, err := io.Copy(h, io.NewSectionReader(src.R, 0, offset)); err != nil {
			fail()
			return
		}
	}
	if s.t.SendText(Encode(NewFileStart(src.Entry.ID, offset))) != nil {
		return
	}
	chunk := chunkSizeFor(s.t)
	sr := io.NewSectionReader(src.R, offset, size-offset)
	sent := offset
	for sent < size {
		if ctx.Err() != nil {
			return
		}
		buf := make([]byte, min(int64(chunk), size-sent)) // fresh buffer: the transport may retain it
		if _, err := io.ReadFull(sr, buf); err != nil {
			fail()
			return
		}
		// flow control: receiver-confirmed window first, then the socket's own buffer
		for sent-s.acked.Load() >= MaxUnacked {
			before := s.acked.Load()
			timer := time.NewTimer(StallTimeout)
			select {
			case <-s.ackCh:
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
			timer.Stop()
			if s.acked.Load() == before && ctx.Err() == nil {
				_ = s.t.Close() // receiver went silent: end as "closed" so it can resume
				return
			}
		}
		if s.t.Buffered() > HighWater {
			if s.t.Drain(ctx) != nil {
				return
			}
		}
		if s.t.SendBinary(buf) != nil {
			return
		}
		h.Write(buf)
		sent += int64(len(buf))
		if s.o.OnProgress != nil {
			s.o.OnProgress(idx, sent)
		}
	}
	if s.t.SendText(Encode(NewFileEnd(src.Entry.ID, hexU64(h.Sum64())))) != nil {
		return
	}
	s.post(ctx, SEvent{K: SEvStreamed, Idx: idx})
}

// ------------------------------------------------------------------ receiver

type FileWriter interface {
	Write(p []byte) error
	Close() error // finalize: called only after the hash matched
	Abort() error // discard everything written so far
}

type NamedFile struct {
	FileEntry
	SafeName string
}

type Sink interface {
	Open(f NamedFile, idx int) (FileWriter, error)
}

type curFile struct {
	idx   int
	bytes int64
	w     FileWriter
	h     *xxhash.Digest
}

// ReceiveState survives transport changes so a transfer can resume.
type ReceiveState struct {
	Manifest string
	Files    []NamedFile
	Accepted bool
	NextIdx  int
	cur      *curFile
	sinceAck int64
}

// Discard throws away a partial file that was kept for a resume. Call it when giving up for good.
func (s *ReceiveState) Discard() {
	if s.cur != nil {
		_ = s.cur.w.Abort()
		s.cur = nil
	}
}

type ReceiverOptions struct {
	RoomID, Secret, Device string
	Sink                   Sink
	State                  *ReceiveState
	// OnOffer asks the user. It is called once per transfer, never again on resume.
	OnOffer    func(files []NamedFile) bool
	OnProgress func(idx int, bytes int64)
	OnFileDone func(idx int)
}

func busy(s RS) bool { return s == RStreaming || s == RAwaitStart || s == RVerifying }

// RunReceiver receives one transfer. Cancelling ctx tells the sender "user" and ends the session.
func RunReceiver(ctx context.Context, t Transport, o ReceiverOptions) Result {
	if res := handshake(ctx, RoleReceiver, t, o.RoomID, o.Secret, o.Device); res != nil {
		return failHandshake(t, *res)
	}
	r := &receiver{t: t, o: o, ss: o.State}
	st := RState{}
	var q []REvent
	for {
		if len(q) == 0 {
			ev, chunk := r.next(ctx, st)
			q = append(q, ev)
			r.chunk = chunk
		}
		ev := q[0]
		q = q[1:]
		before := st
		var fx []REffect
		st, fx = ReceiverStep(st, ev)
		if r.chunk != nil && before.S == RStreaming && st.S == RStreaming {
			if err := r.writeChunk(r.chunk); err != nil {
				return r.fail(err)
			}
		}
		r.chunk = nil
		for _, e := range fx {
			more, res := r.apply(e)
			if res != nil {
				return *res
			}
			q = append(q, more...)
		}
	}
}

type receiver struct {
	t     Transport
	o     ReceiverOptions
	ss    *ReceiveState
	chunk []byte
}

// next blocks for the next event. Not while the user is deciding: the stall watchdog only runs mid-transfer.
func (r *receiver) next(ctx context.Context, st RState) (REvent, []byte) {
	if ctx.Err() != nil { // Recv prefers queued frames, so a busy sender must not be able to starve cancellation
		return REvent{K: EvCancel}, nil
	}
	rctx, cancel := ctx, context.CancelFunc(func() {})
	if busy(st.S) {
		rctx, cancel = context.WithTimeout(ctx, StallTimeout)
	}
	defer cancel()
	w, err := r.t.Recv(rctx)
	switch {
	case err == nil && w.IsText:
		if f, perr := ParseFrame(w.Text); perr == nil {
			return REvent{K: EvFrame, Frame: f}, nil
		}
		return REvent{K: EvBad}, nil
	case err == nil:
		return REvent{K: EvChunk, N: int64(len(w.Bin))}, w.Bin
	case ctx.Err() != nil:
		return REvent{K: EvCancel}, nil // the caller cancelled
	case errors.Is(err, context.DeadlineExceeded):
		_ = r.t.Close() // stalled: end as "closed" so it can resume
		return REvent{K: EvClosed}, nil
	}
	return REvent{K: EvClosed}, nil
}

func (r *receiver) writeChunk(p []byte) error {
	cur := r.ss.cur
	if err := cur.w.Write(p); err != nil {
		return err
	}
	cur.h.Write(p)
	cur.bytes += int64(len(p))
	r.ss.sinceAck += int64(len(p))
	if r.o.OnProgress != nil {
		r.o.OnProgress(cur.idx, cur.bytes)
	}
	if r.ss.sinceAck >= AckEvery {
		r.ss.sinceAck = 0
		_ = r.t.SendText(Encode(NewAck(r.ss.Files[cur.idx].ID, cur.bytes)))
	}
	return nil
}

// fail ends the session loudly for a local fault (disk full, bad manifest change, ...).
func (r *receiver) fail(err error) Result {
	_ = r.t.SendText(Encode(NewCancel(ReasonProtocolError)))
	r.abortCurrent()
	_ = r.t.Close()
	res := failed(ReasonProtocolError, false)
	res.Err = err
	return res
}

func (r *receiver) abortCurrent() {
	if r.ss.cur != nil {
		_ = r.ss.cur.w.Abort()
		r.ss.cur = nil
	}
}

func (r *receiver) apply(e REffect) ([]REvent, *Result) {
	ss := r.ss
	switch e.K {
	case FxSend:
		_ = r.t.SendText(Encode(e.Frame))
	case FxOffer:
		key := fmt.Sprint(e.Files)
		if ss.Manifest != "" && ss.Manifest != key {
			res := r.fail(errors.New("manifest changed mid-transfer"))
			return nil, &res
		}
		first := ss.Manifest == ""
		ss.Manifest = key
		ss.Files = make([]NamedFile, len(e.Files))
		for i, f := range e.Files {
			ss.Files[i] = NamedFile{f, SanitizeName(f.Name)}
		}
		if !ss.Accepted && !(first && r.o.OnOffer(ss.Files)) {
			return []REvent{{K: EvDecline}}, nil
		}
		ss.Accepted = true
		ev := REvent{K: EvAccept, Idx: ss.NextIdx}
		if ss.cur != nil {
			ev.Idx, ev.Offset = ss.cur.idx, ss.cur.bytes
		}
		return []REvent{ev}, nil
	case FxBeginFile:
		if ss.cur != nil && ss.cur.idx == e.Idx && ss.cur.bytes == e.Offset {
			break // resuming into the writer we already hold
		}
		if e.Offset != 0 {
			res := r.fail(errors.New("resume offset without a partial file"))
			return nil, &res
		}
		w, err := r.o.Sink.Open(ss.Files[e.Idx], e.Idx)
		if err != nil {
			res := r.fail(err)
			return nil, &res
		}
		ss.cur = &curFile{idx: e.Idx, w: w, h: xxhash.New()}
		ss.sinceAck = 0
	case FxVerify:
		return []REvent{{K: EvVerified, OK: hexU64(ss.cur.h.Sum64()) == e.Hash}}, nil
	case FxFinishFile:
		if err := ss.cur.w.Close(); err != nil {
			res := r.fail(err)
			return nil, &res
		}
		ss.cur = nil
		ss.NextIdx = e.Idx + 1
		if r.o.OnFileDone != nil {
			r.o.OnFileDone(e.Idx)
		}
	case FxComplete:
		return nil, &Result{Status: StatusDone}
	case FxAbort:
		if e.Reason == ReasonClosed {
			return nil, &Result{Status: StatusClosed} // keep the writer for a resume
		}
		r.abortCurrent()
		_ = r.t.Close()
		res := failed(e.Reason, e.ByPeer)
		return nil, &res
	}
	return nil, nil
}

func hexDecode(s string) ([]byte, error) { return hex.DecodeString(s) }

// WaitPeerClose blocks until the peer closes the transport (or timeout). A receiver calls it after finishing so the
// last frame it sent (the final file_done) is flushed before it tears the connection down.
func WaitPeerClose(t Transport, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		if _, err := t.Recv(ctx); err != nil {
			return
		}
	}
}
