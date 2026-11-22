package protocol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testSecret = "amber-otter-kite-moth"
	testRoom   = "4821"
	mib        = 1 << 20
)

func pattern(n, seed int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + seed + (i >> 8))
	}
	return b
}

func src(id int, name string, data []byte) Source {
	return Source{FileEntry{id, name, int64(len(data)), ""}, bytes.NewReader(data)}
}

// memSink collects files in memory.
type memSink struct {
	mu      sync.Mutex
	data    map[int]*bytes.Buffer
	names   map[int]string
	closed  []int
	aborted []int
	openErr error
	delay   time.Duration
	written atomic.Int64
}

func newSink() *memSink { return &memSink{data: map[int]*bytes.Buffer{}, names: map[int]string{}} }

func (s *memSink) Open(f NamedFile, idx int) (FileWriter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openErr != nil {
		return nil, s.openErr
	}
	s.names[idx] = f.SafeName
	if s.data[idx] == nil {
		s.data[idx] = &bytes.Buffer{}
	}
	return &memWriter{s, idx}, nil
}

func (s *memSink) bytes(idx int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.data[idx]; b != nil {
		return append([]byte(nil), b.Bytes()...)
	}
	return nil
}

type memWriter struct {
	s   *memSink
	idx int
}

func (w *memWriter) Write(p []byte) error {
	if w.s.delay > 0 {
		time.Sleep(w.s.delay)
	}
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	w.s.data[w.idx].Write(p)
	w.s.written.Add(int64(len(p)))
	return nil
}
func (w *memWriter) Close() error {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	w.s.closed = append(w.s.closed, w.idx)
	return nil
}
func (w *memWriter) Abort() error {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	w.s.aborted = append(w.s.aborted, w.idx)
	return nil
}

type rig struct {
	sRes, rRes       chan Result
	st, rt           *MemTransport
	sink             *memSink
	state            *ReceiveState
	cancelS, cancelR context.CancelFunc
}

type rigOpts struct {
	secret string
	accept *bool
	sink   *memSink
	state  *ReceiveState
	st, rt *MemTransport
	fp     bool
}

func newRig(t *testing.T, files []Source, o rigOpts) *rig {
	t.Helper()
	if o.st == nil {
		o.st, o.rt = NewMemPair(o.fp)
	}
	if o.sink == nil {
		o.sink = newSink()
	}
	if o.state == nil {
		o.state = &ReceiveState{}
	}
	secret := o.secret
	if secret == "" {
		secret = testSecret
	}
	accept := true
	if o.accept != nil {
		accept = *o.accept
	}
	r := &rig{sRes: make(chan Result, 1), rRes: make(chan Result, 1), st: o.st, rt: o.rt, sink: o.sink, state: o.state}
	ctxS, cS := context.WithCancel(context.Background())
	ctxR, cR := context.WithCancel(context.Background())
	r.cancelS, r.cancelR = cS, cR
	t.Cleanup(func() { cS(); cR() })
	go func() {
		r.sRes <- RunSender(ctxS, o.st, SenderOptions{RoomID: testRoom, Secret: testSecret, Device: "s", Files: files})
	}()
	go func() {
		r.rRes <- RunReceiver(ctxR, o.rt, ReceiverOptions{RoomID: testRoom, Secret: secret, Device: "r", Sink: o.sink, State: o.state,
			OnOffer: func([]NamedFile) bool { return accept }})
	}()
	return r
}

func wait(t *testing.T, ch chan Result) Result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(20 * time.Second):
		t.Fatal("session hung")
		return Result{}
	}
}

func expect(t *testing.T, got Result, status Status, reason Reason, byPeer bool) {
	t.Helper()
	if got.Status != status || (status == StatusFailed && (got.Reason != reason || got.ByPeer != byPeer)) {
		t.Fatalf("got %v, want status=%v reason=%q byPeer=%v", got, status, reason, byPeer)
	}
}

func TestTransfersZeroByteDuplicateAndMultiChunk(t *testing.T) {
	a, b := pattern(3*mib+17, 1), pattern(1000, 2)
	x := newRig(t, []Source{src(0, "same.txt", a), src(1, "same.txt", b), src(2, "empty", nil)}, rigOpts{})
	expect(t, wait(t, x.sRes), StatusDone, "", false)
	expect(t, wait(t, x.rRes), StatusDone, "", false)
	if !bytes.Equal(x.sink.bytes(0), a) || !bytes.Equal(x.sink.bytes(1), b) || len(x.sink.bytes(2)) != 0 {
		t.Fatal("received bytes differ")
	}
	if x.state.Sender != "s" {
		t.Fatalf("sender name %q, want the sender's hello device", x.state.Sender)
	}
	if len(x.sink.closed) != 3 {
		t.Fatalf("closed %v", x.sink.closed)
	}
}

func TestFileLargerThanWindowNeedsAcks(t *testing.T) {
	big := pattern(40*mib, 1)
	x := newRig(t, []Source{src(0, "big", big)}, rigOpts{})
	expect(t, wait(t, x.sRes), StatusDone, "", false)
	expect(t, wait(t, x.rRes), StatusDone, "", false)
	if !bytes.Equal(x.sink.bytes(0), big) {
		t.Fatal("bytes differ")
	}
}

func TestHostileNameIsSanitizedBeforeSink(t *testing.T) {
	x := newRig(t, []Source{src(0, "../../etc/passwd", pattern(5, 0))}, rigOpts{})
	wait(t, x.sRes)
	if x.sink.names[0] != "passwd" {
		t.Fatalf("sink saw %q", x.sink.names[0])
	}
}

func TestWrongSecretFailsBothAndSendsNoManifest(t *testing.T) {
	x := newRig(t, []Source{src(0, "x", pattern(10, 0))}, rigOpts{secret: "wrong-wrong-wrong-wrong"})
	expect(t, wait(t, x.sRes), StatusFailed, ReasonAuthFailed, false)
	expect(t, wait(t, x.rRes), StatusFailed, ReasonAuthFailed, true)
	if len(x.sink.data) != 0 {
		t.Fatal("sink must stay untouched")
	}
}

func TestDTLSFingerprintMismatchFailsAuth(t *testing.T) {
	st, rt := NewMemPair(true)
	rt.pfp = repeat(0xcc) // the receiver sees a different sender fingerprint than the sender really has
	x := newRig(t, []Source{src(0, "x", pattern(10, 0))}, rigOpts{st: st, rt: rt})
	expect(t, wait(t, x.sRes), StatusFailed, ReasonAuthFailed, false)
	if r := wait(t, x.rRes); r.Status != StatusFailed {
		t.Fatalf("receiver: %v", r)
	}
}

func TestDeclineCancelsSender(t *testing.T) {
	no := false
	x := newRig(t, []Source{src(0, "x", pattern(10, 0))}, rigOpts{accept: &no})
	expect(t, wait(t, x.rRes), StatusFailed, ReasonDeclined, false)
	expect(t, wait(t, x.sRes), StatusFailed, ReasonDeclined, true)
}

func TestCorruptChunkIsCaughtByHash(t *testing.T) {
	st, rt := NewMemPair(false)
	flipped := false
	st.Tamper = func(w Wire) (Wire, bool) {
		if !w.IsText && !flipped {
			flipped = true
			w.Bin = append([]byte(nil), w.Bin...)
			w.Bin[0] ^= 0xff
		}
		return w, true
	}
	x := newRig(t, []Source{src(0, "x", pattern(500_000, 0))}, rigOpts{st: st, rt: rt})
	expect(t, wait(t, x.rRes), StatusFailed, ReasonHashMismatch, false)
	expect(t, wait(t, x.sRes), StatusFailed, ReasonHashMismatch, true)
	if len(x.sink.aborted) != 1 || len(x.sink.closed) != 0 {
		t.Fatalf("writer must be aborted not finalized: aborted=%v closed=%v", x.sink.aborted, x.sink.closed)
	}
}

func TestOverrunIsProtocolError(t *testing.T) {
	st, rt := NewMemPair(false)
	n := 0
	st.Tamper = func(w Wire) (Wire, bool) {
		if !w.IsText && n == 0 {
			n++
			defer st.send(Wire{Bin: make([]byte, 50)}) // a rogue second chunk beyond the declared 100 bytes
		}
		return w, true
	}
	x := newRig(t, []Source{src(0, "x", pattern(100, 0))}, rigOpts{st: st, rt: rt})
	expect(t, wait(t, x.rRes), StatusFailed, ReasonProtocolError, false)
}

func TestSinkOpenErrorFailsLoudlyInsteadOfHanging(t *testing.T) {
	s := newSink()
	s.openErr = errors.New("disk full")
	x := newRig(t, []Source{src(0, "x", pattern(10, 0))}, rigOpts{sink: s})
	r := wait(t, x.rRes)
	expect(t, r, StatusFailed, ReasonProtocolError, false)
	if r.Err == nil || r.Err.Error() != "disk full" {
		t.Fatalf("local cause must surface: %v", r.Err)
	}
	expect(t, wait(t, x.sRes), StatusFailed, ReasonProtocolError, true)
}

func TestSenderCancelStopsStreaming(t *testing.T) {
	s := newSink()
	s.delay = 3 * time.Millisecond
	x := newRig(t, []Source{src(0, "big", pattern(20*mib, 0))}, rigOpts{sink: s})
	for s.written.Load() < mib {
		time.Sleep(5 * time.Millisecond)
	}
	x.cancelS()
	expect(t, wait(t, x.sRes), StatusFailed, ReasonUser, false)
	expect(t, wait(t, x.rRes), StatusFailed, ReasonUser, true)
	sent := x.st.SentBytes.Load()
	time.Sleep(100 * time.Millisecond)
	if x.st.SentBytes.Load() != sent {
		t.Fatal("sender kept streaming after cancel")
	}
	if len(s.aborted) != 1 {
		t.Fatalf("aborted %v", s.aborted)
	}
}

func TestReceiverCancelStopsSender(t *testing.T) {
	s := newSink()
	s.delay = 3 * time.Millisecond
	x := newRig(t, []Source{src(0, "big", pattern(20*mib, 0))}, rigOpts{sink: s})
	for s.written.Load() < mib {
		time.Sleep(5 * time.Millisecond)
	}
	x.cancelR()
	expect(t, wait(t, x.rRes), StatusFailed, ReasonUser, false)
	expect(t, wait(t, x.sRes), StatusFailed, ReasonUser, true)
}

func TestDroppedTransportEndsBothAsClosedAndKeepsWriter(t *testing.T) {
	s := newSink()
	s.delay = 3 * time.Millisecond
	x := newRig(t, []Source{src(0, "big", pattern(20*mib, 0))}, rigOpts{sink: s})
	for s.written.Load() < mib {
		time.Sleep(5 * time.Millisecond)
	}
	_ = x.st.Close()
	expect(t, wait(t, x.sRes), StatusClosed, "", false)
	expect(t, wait(t, x.rRes), StatusClosed, "", false)
	if len(s.aborted) != 0 {
		t.Fatal("writer must be kept for resume")
	}
}

func TestResumeContinuesFromLastWrittenByte(t *testing.T) {
	data := pattern(6*mib+123, 3)
	files := []Source{src(0, "a.bin", data), src(1, "b.bin", pattern(2000, 9))}
	sink, state := newSink(), &ReceiveState{}
	st, rt := NewMemPair(false)
	st.Tamper = func(w Wire) (Wire, bool) {
		if st.SentBytes.Load() >= 2*mib { // the link dies mid-file
			_ = st.Close()
			return w, false
		}
		return w, true
	}
	first := newRig(t, files, rigOpts{sink: sink, state: state, st: st, rt: rt})
	expect(t, wait(t, first.rRes), StatusClosed, "", false)
	wait(t, first.sRes)
	have := state.cur.bytes
	if have < 2*mib || have >= int64(len(data)) {
		t.Fatalf("held %d bytes", have)
	}

	offers := 0
	st2, rt2 := NewMemPair(false)
	sRes := make(chan Result, 1)
	go func() {
		sRes <- RunSender(context.Background(), st2, SenderOptions{RoomID: testRoom, Secret: testSecret, Device: "s", Files: files})
	}()
	r2 := RunReceiver(context.Background(), rt2, ReceiverOptions{RoomID: testRoom, Secret: testSecret, Device: "r", Sink: sink, State: state,
		OnOffer: func([]NamedFile) bool { offers++; return true }})
	expect(t, r2, StatusDone, "", false)
	expect(t, wait(t, sRes), StatusDone, "", false)
	if offers != 0 {
		t.Fatal("resume must not prompt again")
	}
	if !bytes.Equal(sink.bytes(0), data) || !bytes.Equal(sink.bytes(1), pattern(2000, 9)) {
		t.Fatal("resumed files differ")
	}
	if st2.SentBytes.Load() > int64(len(data))-have+4000 {
		t.Fatalf("resent too much: %d", st2.SentBytes.Load())
	}
}

func TestBinaryFrameDuringHandshakeDoesNotHangPeer(t *testing.T) {
	st, rt := NewMemPair(false)
	x := newRig(t, []Source{src(0, "x", pattern(10, 0))}, rigOpts{st: st, rt: rt})
	_ = st.SendBinary([]byte{1})
	a, b := wait(t, x.sRes), wait(t, x.rRes)
	if a.Status != StatusFailed && b.Status != StatusFailed {
		t.Fatalf("got %v / %v", a, b)
	}
}

func TestLargeManifest(t *testing.T) {
	many := make([]Source, 2000)
	for i := range many {
		many[i] = src(i, fmt.Sprintf("f%d.bin", i), pattern(1, i))
	}
	x := newRig(t, many, rigOpts{})
	expect(t, wait(t, x.sRes), StatusDone, "", false)
	expect(t, wait(t, x.rRes), StatusDone, "", false)
	if len(x.sink.names) != 2000 {
		t.Fatalf("received %d files", len(x.sink.names))
	}

	tooMany := make([]Source, 6000)
	for i := range tooMany {
		tooMany[i] = src(i, fmt.Sprintf("f%d.bin", i), pattern(1, i))
	}
	y := newRig(t, tooMany, rigOpts{})
	r := wait(t, y.sRes)
	expect(t, r, StatusFailed, ReasonProtocolError, false)
	if r.Err == nil {
		t.Fatal("the sender must say why")
	}
	if r := wait(t, y.rRes); r.Status != StatusFailed {
		t.Fatalf("receiver: %v", r)
	}
}

// slowAfterFileEnd pauses the sender right after file_end has been delivered, which is exactly the window in which a
// fast receiver's file_done used to overtake the sender's own "streamed" event.
type slowAfterFileEnd struct{ Transport }

func (w slowAfterFileEnd) SendText(s string) error {
	err := w.Transport.SendText(s)
	if strings.Contains(s, `"t":"file_end"`) {
		time.Sleep(40 * time.Millisecond)
	}
	return err
}

// Regression (found as a ~1-in-8 flake in TestLargeManifest): the receiver's file_done must never be able to arrive
// at the sender's state machine before the sender knows it finished streaming.
func TestFastReceiverDoneNeverOvertakesStreamed(t *testing.T) {
	st, rt := NewMemPair(false)
	files := []Source{src(0, "a", pattern(10, 1)), src(1, "b", pattern(10, 2)), src(2, "c", pattern(10, 3))}
	sink, state := newSink(), &ReceiveState{}
	sRes := make(chan Result, 1)
	go func() {
		sRes <- RunSender(context.Background(), slowAfterFileEnd{st}, SenderOptions{RoomID: testRoom, Secret: testSecret, Device: "s", Files: files})
	}()
	r := RunReceiver(context.Background(), rt, ReceiverOptions{RoomID: testRoom, Secret: testSecret, Device: "r", Sink: sink, State: state,
		OnOffer: func([]NamedFile) bool { return true }})
	expect(t, r, StatusDone, "", false)
	expect(t, wait(t, sRes), StatusDone, "", false)
}

func TestSenderNameIsCleanedForDisplay(t *testing.T) {
	if got := senderName(""); got != "sender" {
		t.Fatalf("empty name: %q", got)
	}
	if got := []rune(senderName(strings.Repeat("é", 100))); len(got) != 64 {
		t.Fatalf("long name kept %d runes, want 64", len(got))
	}
	if got := senderName("evil\x1b[2Jname"); strings.ContainsRune(got, 0x1b) {
		t.Fatalf("escape survived: %q", got)
	}
}
