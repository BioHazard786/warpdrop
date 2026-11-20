package protocol

// Pure transfer-state machines: no IO. Each step returns the next state plus effects for the driver to run.
// They mirror web/src/transfer/machine.ts so both implementations replay the same fixtures.

// ----------------------------------------------------------------- receiver

type RS int

const (
	RIdle RS = iota
	ROffered
	RAwaitStart
	RStreaming
	RVerifying
	RDone
	RFailed
)

type RState struct {
	S      RS
	Files  []FileEntry
	Idx    int
	Offset int64 // requested start offset (RAwaitStart) or start of this stream (RStreaming)
	Bytes  int64 // bytes of the current file received so far (RStreaming)
	Reason Reason
	ByPeer bool
}

type REventKind int

const (
	EvFrame REventKind = iota
	EvChunk
	EvAccept
	EvDecline
	EvCancel
	EvVerified
	EvClosed
	EvBad
)

type REvent struct {
	K      REventKind
	Frame  any   // EvFrame: a parsed frame
	N      int64 // EvChunk: byte count
	Idx    int   // EvAccept resume position
	Offset int64
	OK     bool // EvVerified
}

type REffectKind int

const (
	FxSend REffectKind = iota
	FxOffer
	FxBeginFile
	FxVerify
	FxFinishFile
	FxAbort
	FxComplete
)

type REffect struct {
	K      REffectKind
	Frame  any         // FxSend
	Files  []FileEntry // FxOffer
	Idx    int
	Offset int64
	Hash   string // FxVerify: the hash the sender claims
	Reason Reason // FxAbort
	ByPeer bool
}

func rfail(reason Reason, tell bool) (RState, []REffect) {
	var fx []REffect
	if tell {
		fx = append(fx, REffect{K: FxSend, Frame: NewCancel(reason)})
	}
	return RState{S: RFailed, Reason: reason}, append(fx, REffect{K: FxAbort, Reason: reason})
}

func rpeerEnd(reason Reason) (RState, []REffect) {
	return RState{S: RFailed, Reason: reason, ByPeer: true}, []REffect{{K: FxAbort, Reason: reason, ByPeer: true}}
}

// ReceiverStep advances the receiver. Terminal states are sticky.
func ReceiverStep(st RState, ev REvent) (RState, []REffect) {
	if st.S == RDone || st.S == RFailed {
		return st, nil
	}
	switch ev.K {
	case EvClosed:
		return rpeerEnd(ReasonClosed)
	case EvCancel:
		return rfail(ReasonUser, true)
	case EvBad:
		return rfail(ReasonProtocolError, true)
	case EvFrame:
		if c, ok := ev.Frame.(Cancel); ok {
			return rpeerEnd(c.Reason)
		}
	}

	switch st.S {
	case RIdle:
		if m, ok := ev.Frame.(Manifest); ok && ev.K == EvFrame {
			return RState{S: ROffered, Files: m.Files}, []REffect{{K: FxOffer, Files: m.Files}}
		}
	case ROffered:
		switch {
		case ev.K == EvDecline:
			return rfail(ReasonDeclined, true)
		case ev.K == EvAccept:
			if ev.Idx < 0 || ev.Idx >= len(st.Files) || ev.Offset < 0 || ev.Offset > st.Files[ev.Idx].Size {
				break
			}
			return RState{S: RAwaitStart, Files: st.Files, Idx: ev.Idx, Offset: ev.Offset},
				[]REffect{{K: FxSend, Frame: NewRequest(st.Files[ev.Idx].ID, ev.Offset)}}
		case ev.K == EvFrame:
			if _, ok := ev.Frame.(Manifest); ok {
				return st, nil // duplicate while the user decides
			}
		}
	case RAwaitStart:
		if f, ok := ev.Frame.(FileStart); ok && ev.K == EvFrame && f.ID == st.Files[st.Idx].ID && f.Offset == st.Offset {
			return RState{S: RStreaming, Files: st.Files, Idx: st.Idx, Offset: st.Offset, Bytes: st.Offset},
				[]REffect{{K: FxBeginFile, Idx: st.Idx, Offset: st.Offset}}
		}
	case RStreaming:
		file := st.Files[st.Idx]
		if ev.K == EvChunk {
			st.Bytes += ev.N
			if st.Bytes > file.Size {
				break
			}
			return st, nil
		}
		if f, ok := ev.Frame.(FileEnd); ok && ev.K == EvFrame && f.ID == file.ID && st.Bytes == file.Size {
			return RState{S: RVerifying, Files: st.Files, Idx: st.Idx}, []REffect{{K: FxVerify, Idx: st.Idx, Hash: f.Hash}}
		}
	case RVerifying:
		if ev.K != EvVerified {
			break
		}
		if !ev.OK {
			return rfail(ReasonHashMismatch, true)
		}
		fx := []REffect{{K: FxFinishFile, Idx: st.Idx}, {K: FxSend, Frame: NewFileDone(st.Files[st.Idx].ID)}}
		if next := st.Idx + 1; next < len(st.Files) {
			return RState{S: RAwaitStart, Files: st.Files, Idx: next},
				append(fx, REffect{K: FxSend, Frame: NewRequest(st.Files[next].ID, 0)})
		}
		return RState{S: RDone}, append(fx, REffect{K: FxComplete})
	}
	return rfail(ReasonProtocolError, true)
}

// ----------------------------------------------------------------- sender (one per receiver)

type SS int

const (
	SAwaitRequest SS = iota
	SSending
	SAwaitDone
	SDone
	SFailed
)

type SState struct {
	S      SS
	Files  []FileEntry
	Idx    int
	Reason Reason
	ByPeer bool
}

type SEventKind int

const (
	SEvFrame SEventKind = iota
	SEvStreamed
	SEvCancel
	SEvClosed
	SEvBad
)

type SEvent struct {
	K     SEventKind
	Frame any
	Idx   int
}

type SEffectKind int

const (
	SFxSend SEffectKind = iota
	SFxStreamFile
	SFxAck
	SFxFileAcked
	SFxAbort
	SFxComplete
)

type SEffect struct {
	K      SEffectKind
	Frame  any
	Idx    int
	Offset int64
	ID     int
	Bytes  int64
	Reason Reason
	ByPeer bool
}

func SenderInit(files []FileEntry) SState { return SState{S: SAwaitRequest, Files: files} }

func sfail(reason Reason, tell bool) (SState, []SEffect) {
	var fx []SEffect
	if tell {
		fx = append(fx, SEffect{K: SFxSend, Frame: NewCancel(reason)})
	}
	return SState{S: SFailed, Reason: reason}, append(fx, SEffect{K: SFxAbort, Reason: reason})
}

func speerEnd(reason Reason) (SState, []SEffect) {
	return SState{S: SFailed, Reason: reason, ByPeer: true}, []SEffect{{K: SFxAbort, Reason: reason, ByPeer: true}}
}

func (s SState) fileIdx(id int) int {
	for i, f := range s.Files {
		if f.ID == id {
			return i
		}
	}
	return -1
}

// SenderStep advances one receiver's sender state. A request may name any file (a resumed session starts mid-manifest).
func SenderStep(st SState, ev SEvent) (SState, []SEffect) {
	if st.S == SDone || st.S == SFailed {
		return st, nil
	}
	switch ev.K {
	case SEvClosed:
		return speerEnd(ReasonClosed)
	case SEvCancel:
		return sfail(ReasonUser, true)
	case SEvBad:
		return sfail(ReasonProtocolError, true)
	}
	if c, ok := ev.Frame.(Cancel); ok && ev.K == SEvFrame {
		return speerEnd(c.Reason)
	}
	if a, ok := ev.Frame.(Ack); ok && ev.K == SEvFrame {
		// only meaningful for the file in flight; stale or absurd acks are ignored
		if st.S == SSending && a.ID == st.Files[st.Idx].ID && a.Bytes <= st.Files[st.Idx].Size {
			return st, []SEffect{{K: SFxAck, Idx: st.Idx, ID: a.ID, Bytes: a.Bytes}}
		}
		return st, nil
	}

	switch st.S {
	case SAwaitRequest:
		if r, ok := ev.Frame.(Request); ok && ev.K == SEvFrame {
			idx := st.fileIdx(r.ID)
			if idx < 0 || r.Offset > st.Files[idx].Size {
				break
			}
			return SState{S: SSending, Files: st.Files, Idx: idx}, []SEffect{{K: SFxStreamFile, Idx: idx, Offset: r.Offset}}
		}
	case SSending:
		if ev.K == SEvStreamed && ev.Idx == st.Idx {
			return SState{S: SAwaitDone, Files: st.Files, Idx: st.Idx}, nil
		}
	case SAwaitDone:
		cur := st.Files[st.Idx]
		if d, ok := ev.Frame.(FileDone); ok && ev.K == SEvFrame && d.ID == cur.ID {
			fx := []SEffect{{K: SFxFileAcked, Idx: st.Idx}}
			if st.Idx+1 >= len(st.Files) {
				return SState{S: SDone}, append(fx, SEffect{K: SFxComplete})
			}
			return SState{S: SAwaitRequest, Files: st.Files}, fx
		}
		// a receiver that lost the connection may re-request the same file from a new offset
		if r, ok := ev.Frame.(Request); ok && ev.K == SEvFrame && r.ID == cur.ID && r.Offset <= cur.Size {
			return SState{S: SSending, Files: st.Files, Idx: st.Idx}, []SEffect{{K: SFxStreamFile, Idx: st.Idx, Offset: r.Offset}}
		}
	}
	return sfail(ReasonProtocolError, true)
}
