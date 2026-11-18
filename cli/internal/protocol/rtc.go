package protocol

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// RTCTransport is a pion data channel as a Transport. Fingerprints come from the negotiated SDP, so the
// handshake is bound to this DTLS session.
type RTCTransport struct {
	pc   *webrtc.PeerConnection
	dc   *webrtc.DataChannel
	recv chan Wire
	done chan struct{}
	low  chan struct{}
	once sync.Once
	max  int
}

func newRTCTransport(pc *webrtc.PeerConnection, dc *webrtc.DataChannel) *RTCTransport {
	t := &RTCTransport{pc: pc, dc: dc, recv: make(chan Wire, 64), done: make(chan struct{}), low: make(chan struct{}, 1)}
	t.max = 64 << 10 // the smallest interoperable SCTP message, until the peer says otherwise
	if sctp := pc.SCTP(); sctp != nil {
		if m := int(sctp.GetCapabilities().MaxMessageSize); m >= 16<<10 {
			t.max = m
		}
	}
	dc.SetBufferedAmountLowThreshold(LowWater)
	dc.OnBufferedAmountLow(func() {
		select {
		case t.low <- struct{}{}:
		default:
		}
	})
	// Blocking here is deliberate: it stops pion's read loop, which closes the SCTP window and slows the sender.
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		w := Wire{Bin: m.Data}
		if m.IsString {
			w = Wire{Text: string(m.Data), IsText: true}
		}
		select {
		case t.recv <- w:
		case <-t.done:
		}
	})
	dc.OnClose(func() { t.Close() })
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			t.Close()
		}
	})
	return t
}

func (t *RTCTransport) Kind() string     { return "rtc" }
func (t *RTCTransport) MaxMessage() int  { return t.max }
func (t *RTCTransport) Buffered() uint64 { return t.dc.BufferedAmount() }
func (t *RTCTransport) LocalFingerprint() []byte {
	if d := t.pc.CurrentLocalDescription(); d != nil {
		return DTLSFingerprint(d.SDP)
	}
	return nil
}
func (t *RTCTransport) RemoteFingerprint() []byte {
	if d := t.pc.CurrentRemoteDescription(); d != nil {
		return DTLSFingerprint(d.SDP)
	}
	return nil
}
func (t *RTCTransport) EnableCrypto([]byte, Role) error { return nil } // DTLS already encrypts the channel
func (t *RTCTransport) SendText(s string) error {
	if t.isClosed() {
		return ErrClosed
	}
	return t.dc.SendText(s)
}
func (t *RTCTransport) SendBinary(b []byte) error {
	if t.isClosed() {
		return ErrClosed
	}
	return t.dc.Send(b)
}

func (t *RTCTransport) isClosed() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *RTCTransport) Drain(ctx context.Context) error {
	tick := time.NewTicker(50 * time.Millisecond) // belt and braces if a low-water event is missed
	defer tick.Stop()
	for t.dc.BufferedAmount() > LowWater {
		select {
		case <-t.low:
		case <-tick.C:
		case <-t.done:
			return ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (t *RTCTransport) Recv(ctx context.Context) (Wire, error) {
	select { // deliver what already arrived before reporting the close
	case w := <-t.recv:
		return w, nil
	default:
	}
	select {
	case w := <-t.recv:
		return w, nil
	case <-t.done:
		select {
		case w := <-t.recv:
			return w, nil
		default:
			return Wire{}, ErrClosed
		}
	case <-ctx.Done():
		return Wire{}, ctx.Err()
	}
}

func (t *RTCTransport) Close() error {
	t.once.Do(func() {
		close(t.done)
		_ = t.dc.Close()
		_ = t.pc.Close()
	})
	return nil
}

// ------------------------------------------------------------------ peer connection attempt

// Signal is the payload exchanged through the signaling server to set up a peer connection.
type Signal struct {
	Kind      string                   `json:"kind"` // offer | answer | candidate | restart
	SDP       string                   `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit `json:"candidate"` // null marks end of candidates
}

type RTCPeerOptions struct {
	ICEServers []webrtc.ICEServer
	Offerer    bool // the offerer creates the data channel
	SendSignal func(Signal)
}

// RTCPeer is one connection attempt. Never reuse it: create a fresh one per attempt.
type RTCPeer struct {
	pc     *webrtc.PeerConnection
	o      RTCPeerOptions
	ready  chan *RTCTransport
	failed chan struct{}
	once   sync.Once

	mu        sync.Mutex
	queue     []webrtc.ICECandidateInit // ICE that arrives before the remote description is set
	remoteSet bool
}

func NewRTCPeer(o RTCPeerOptions) (*RTCPeer, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: o.ICEServers})
	if err != nil {
		return nil, err
	}
	p := &RTCPeer{pc: pc, o: o, ready: make(chan *RTCTransport, 1), failed: make(chan struct{})}
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		s := Signal{Kind: "candidate"}
		if c != nil {
			init := c.ToJSON()
			s.Candidate = &init
		}
		o.SendSignal(s)
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed {
			p.once.Do(func() { close(p.failed) })
		}
	})
	if o.Offerer {
		ordered := true
		dc, err := pc.CreateDataChannel("warpdrop", &webrtc.DataChannelInit{Ordered: &ordered}) // reliable and ordered: no lifetime limits
		if err != nil {
			_ = pc.Close()
			return nil, err
		}
		p.watch(dc)
	} else {
		pc.OnDataChannel(p.watch)
	}
	return p, nil
}

func (p *RTCPeer) watch(dc *webrtc.DataChannel) {
	dc.OnOpen(func() { p.ready <- newRTCTransport(p.pc, dc) })
}

// Start makes the offer (offerer only).
func (p *RTCPeer) Start() error {
	if !p.o.Offerer {
		return nil
	}
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return err
	}
	p.o.SendSignal(Signal{Kind: "offer", SDP: offer.SDP})
	return nil
}

// OnSignal applies a peer's signal. Stale or malformed signals are ignored; the connect timeout handles a dead negotiation.
func (p *RTCPeer) OnSignal(s Signal) {
	switch {
	case s.Kind == "offer" && !p.o.Offerer:
		if p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: s.SDP}) != nil {
			return
		}
		p.flush()
		answer, err := p.pc.CreateAnswer(nil)
		if err != nil || p.pc.SetLocalDescription(answer) != nil {
			return
		}
		p.o.SendSignal(Signal{Kind: "answer", SDP: answer.SDP})
	case s.Kind == "answer" && p.o.Offerer:
		if p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: s.SDP}) == nil {
			p.flush()
		}
	case s.Kind == "candidate" && s.Candidate != nil:
		p.mu.Lock()
		if !p.remoteSet {
			p.queue = append(p.queue, *s.Candidate)
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		_ = p.pc.AddICECandidate(*s.Candidate)
	}
}

func (p *RTCPeer) flush() {
	p.mu.Lock()
	p.remoteSet = true
	q := p.queue
	p.queue = nil
	p.mu.Unlock()
	for _, c := range q {
		_ = p.pc.AddICECandidate(c)
	}
}

var ErrRTCFailed = errors.New("webrtc connection failed")

// Ready waits for an open data channel, or fails on timeout, ICE failure or ctx cancel.
func (p *RTCPeer) Ready(ctx context.Context, timeout time.Duration) (*RTCTransport, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case t := <-p.ready:
		return t, nil
	case <-p.failed:
	case <-timer.C:
	case <-ctx.Done():
	}
	_ = p.pc.Close()
	return nil, ErrRTCFailed
}

func (p *RTCPeer) Close() { _ = p.pc.Close() }
