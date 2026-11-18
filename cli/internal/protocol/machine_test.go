package protocol

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/cespare/xxhash/v2"
)

type step struct {
	From  string         `json:"from"`
	Frame map[string]any `json:"frame"`
	Bin   string         `json:"bin"`
}

type transcript struct {
	Receiver map[string]int64 `json:"receiverHasBytes"`
	Steps    []step           `json:"steps"`
	Expect   struct {
		Error *string `json:"error"`
	} `json:"expect"`
}

// frameOf re-encodes a fixture frame to text and parses it with the production parser.
func frameOf(t *testing.T, m map[string]any) any {
	t.Helper()
	b, _ := json.Marshal(m)
	f, err := ParseFrame(string(b))
	if err != nil {
		t.Fatalf("fixture frame %s rejected: %v", b, err)
	}
	return f
}

// replay drives the receiver machine like a driver would and returns the final state, sent frames and finished files.
func replay(t *testing.T, name string) (RState, []any, []int) {
	t.Helper()
	var all map[string]transcript
	fixture(t, "transcripts", &all)
	tr := all[name]
	st := RState{}
	var sent []any
	var done []int
	var current []byte
	for _, s := range tr.Steps {
		if s.From != "sender" {
			continue
		}
		var ev REvent
		if s.Frame != nil {
			f := frameOf(t, s.Frame)
			ev = REvent{K: EvFrame, Frame: f}
			if fs, ok := f.(FileStart); ok {
				current = nil
				if have := tr.Receiver[fmtID(fs.ID)]; have > 0 {
					current = []byte("hello world")[:have] // resume fixture: first bytes already held
				}
			}
		} else {
			b, _ := hex.DecodeString(s.Bin)
			ev = REvent{K: EvChunk, N: int64(len(b))}
			current = append(current, b...)
		}
		q := []REvent{ev}
		for len(q) > 0 {
			var fx []REffect
			st, fx = ReceiverStep(st, q[0])
			q = q[1:]
			for _, e := range fx {
				switch e.K {
				case FxSend:
					sent = append(sent, e.Frame)
				case FxOffer:
					if tr.Expect.Error != nil && *tr.Expect.Error == "declined" {
						q = append(q, REvent{K: EvDecline})
					} else {
						resume := REvent{K: EvAccept}
						if h, ok := tr.Receiver["0"]; ok {
							resume.Offset = h
						}
						q = append(q, resume)
					}
				case FxVerify:
					// the hash the receiver computes over what it actually got for this file
					sum := xxhash.New()
					sum.Write(current)
					q = append(q, REvent{K: EvVerified, OK: hexU64(sum.Sum64()) == e.Hash})
				case FxFinishFile:
					done = append(done, e.Idx)
				}
			}
		}
	}
	return st, sent, done
}

func fmtID(id int) string { return string(rune('0' + id)) }

func TestReceiverReplaysSharedTranscripts(t *testing.T) {
	cases := []struct {
		name   string
		state  RS
		reason Reason
		done   []int
		sent   []string
	}{
		{"happy-path", RDone, "", []int{0, 1}, []string{"request", "file_done", "request", "file_done"}},
		{"duplicate-names", RDone, "", []int{0, 1}, []string{"request", "file_done", "request", "file_done"}},
		{"cancel-declined", RFailed, ReasonDeclined, nil, []string{"cancel"}},
		{"hash-mismatch", RFailed, ReasonHashMismatch, nil, []string{"request", "cancel"}},
		{"overrun", RFailed, ReasonProtocolError, nil, []string{"request", "cancel"}},
		{"resume", RDone, "", []int{0}, []string{"request", "file_done"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, sent, done := replay(t, c.name)
			if st.S != c.state || st.Reason != c.reason {
				t.Fatalf("state=%v reason=%q, want %v %q", st.S, st.Reason, c.state, c.reason)
			}
			if len(done) != len(c.done) {
				t.Fatalf("finished %v, want %v", done, c.done)
			}
			var types []string
			for _, f := range sent {
				types = append(types, frameType(f))
			}
			if len(types) != len(c.sent) {
				t.Fatalf("sent %v, want %v", types, c.sent)
			}
			for i := range types {
				if types[i] != c.sent[i] {
					t.Fatalf("sent %v, want %v", types, c.sent)
				}
			}
		})
	}
}

func TestResumeRequestsHeldOffset(t *testing.T) {
	_, sent, _ := replay(t, "resume")
	if r, ok := sent[0].(Request); !ok || r.Offset != 5 {
		t.Fatalf("first frame %v, want request at offset 5", sent[0])
	}
}

func frameType(f any) string {
	switch f.(type) {
	case Request:
		return "request"
	case FileDone:
		return "file_done"
	case Cancel:
		return "cancel"
	}
	return "?"
}

func TestReceiverRejectsOutOfOrderInput(t *testing.T) {
	m := Manifest{"manifest", []FileEntry{{0, "a", 1, ""}}}
	offered, _ := ReceiverStep(RState{}, REvent{K: EvFrame, Frame: m})
	await, _ := ReceiverStep(offered, REvent{K: EvAccept})
	streaming, _ := ReceiverStep(await, REvent{K: EvFrame, Frame: NewFileStart(0, 0)})
	for name, c := range map[string]struct {
		st RState
		ev REvent
	}{
		"chunk when idle":             {RState{}, REvent{K: EvChunk, N: 1}},
		"chunk before file_start":     {await, REvent{K: EvChunk, N: 1}},
		"file_end with short bytes":   {streaming, REvent{K: EvFrame, Frame: NewFileEnd(0, "0000000000000000")}},
		"file_start wrong id":         {await, REvent{K: EvFrame, Frame: NewFileStart(9, 0)}},
		"file_start wrong offset":     {await, REvent{K: EvFrame, Frame: NewFileStart(0, 1)}},
		"accept with absurd offset":   {offered, REvent{K: EvAccept, Offset: 99}},
		"verified when not verifying": {streaming, REvent{K: EvVerified, OK: true}},
	} {
		if st, _ := ReceiverStep(c.st, c.ev); st.S != RFailed || st.Reason != ReasonProtocolError {
			t.Errorf("%s: got %v/%q", name, st.S, st.Reason)
		}
	}
	// peer cancel and close end it, and terminal states are sticky
	if st, _ := ReceiverStep(await, REvent{K: EvFrame, Frame: NewCancel(ReasonUser)}); st.S != RFailed || !st.ByPeer || st.Reason != ReasonUser {
		t.Error("peer cancel")
	}
	if st, fx := ReceiverStep(RState{S: RFailed}, REvent{K: EvChunk, N: 1}); st.S != RFailed || fx != nil {
		t.Error("terminal state must be sticky")
	}
}

func TestSenderMachine(t *testing.T) {
	files := []FileEntry{{0, "a", 4, ""}, {1, "b", 0, ""}}
	st, fx := SenderStep(SenderInit(files), SEvent{K: SEvFrame, Frame: NewRequest(0, 0)})
	if len(fx) != 1 || fx[0].K != SFxStreamFile {
		t.Fatal("must stream")
	}
	if _, fx = SenderStep(st, SEvent{K: SEvFrame, Frame: NewAck(0, 4)}); len(fx) != 1 || fx[0].K != SFxAck {
		t.Fatal("ack for the current file must be forwarded")
	}
	for _, a := range []Ack{NewAck(1, 0), NewAck(0, 999)} {
		if _, fx = SenderStep(st, SEvent{K: SEvFrame, Frame: a}); fx != nil {
			t.Errorf("stale ack %v forwarded", a)
		}
	}
	st, _ = SenderStep(st, SEvent{K: SEvStreamed, Idx: 0})
	// resume re-request of the same file
	if _, fx = SenderStep(st, SEvent{K: SEvFrame, Frame: NewRequest(0, 2)}); len(fx) != 1 || fx[0].Offset != 2 {
		t.Fatal("resume request must restart streaming at the offset")
	}
	st, _ = SenderStep(st, SEvent{K: SEvFrame, Frame: NewFileDone(0)})
	if st.S != SAwaitRequest {
		t.Fatal("next file expected")
	}
	st, _ = SenderStep(st, SEvent{K: SEvFrame, Frame: NewRequest(1, 0)})
	st, _ = SenderStep(st, SEvent{K: SEvStreamed, Idx: 1})
	if st, fx = SenderStep(st, SEvent{K: SEvFrame, Frame: NewFileDone(1)}); st.S != SDone || fx[len(fx)-1].K != SFxComplete {
		t.Fatal("must complete")
	}
	for _, r := range []Request{NewRequest(9, 0), NewRequest(0, 5)} {
		if s, _ := SenderStep(SenderInit(files), SEvent{K: SEvFrame, Frame: r}); s.S != SFailed {
			t.Errorf("accepted bad request %v", r)
		}
	}
}
