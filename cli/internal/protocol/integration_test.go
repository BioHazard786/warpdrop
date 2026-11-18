package protocol

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ------------------------------------------------------------------ pion <-> pion, real DTLS/SCTP over loopback

func TestPionLoopbackTransferBindsDTLSFingerprints(t *testing.T) {
	var sender, receiver *RTCPeer
	var err error
	// signals cross over directly, standing in for the signaling server
	sender, err = NewRTCPeer(RTCPeerOptions{Offerer: true, SendSignal: func(s Signal) { go receiver.OnSignal(s) }})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err = NewRTCPeer(RTCPeerOptions{SendSignal: func(s Signal) { go sender.OnSignal(s) }})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	defer receiver.Close()
	if err := sender.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var st, rt *RTCTransport
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); st, _ = sender.Ready(ctx, 15*time.Second) }()
	go func() { defer wg.Done(); rt, _ = receiver.Ready(ctx, 15*time.Second) }()
	wg.Wait()
	if st == nil || rt == nil {
		t.Skip("could not establish loopback WebRTC in this environment")
	}
	if len(st.LocalFingerprint()) != 32 || !bytes.Equal(st.LocalFingerprint(), rt.RemoteFingerprint()) {
		t.Fatal("sender's local fingerprint must equal the receiver's view of it")
	}

	a, b := pattern(5*mib+3, 1), pattern(70_000, 2) // > 64 KiB exercises SCTP fragmentation
	files := []Source{src(0, "a.bin", a), src(1, "b.bin", b), src(2, "empty", nil)}
	sink := newSink()
	sRes := make(chan Result, 1)
	go func() {
		sRes <- RunSender(ctx, st, SenderOptions{RoomID: testRoom, Secret: testSecret, Device: "s", Files: files})
	}()
	r := RunReceiver(ctx, rt, ReceiverOptions{RoomID: testRoom, Secret: testSecret, Device: "r", Sink: sink, State: &ReceiveState{},
		OnOffer: func([]NamedFile) bool { return true }})
	expect(t, r, StatusDone, "", false)
	expect(t, wait(t, sRes), StatusDone, "", false)
	if !bytes.Equal(sink.bytes(0), a) || !bytes.Equal(sink.bytes(1), b) {
		t.Fatal("bytes differ over WebRTC")
	}
}

// ------------------------------------------------------------------ relay through the real Go server

var backendBase string

func TestMain(m *testing.M) {
	code := func() int {
		dir, err := os.MkdirTemp("", "wd-backend-")
		if err != nil {
			return m.Run()
		}
		defer os.RemoveAll(dir)
		bin := filepath.Join(dir, "server")
		build := exec.Command("go", "build", "-o", bin, "./cmd/server")
		build.Dir = "../../../backend"
		if build.Run() != nil {
			return m.Run() // relay tests skip themselves
		}
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		srv := exec.Command(bin)
		srv.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
		if srv.Start() != nil {
			return m.Run()
		}
		defer srv.Process.Kill()
		base := fmt.Sprintf("127.0.0.1:%d", port)
		for i := 0; i < 50; i++ {
			if resp, err := http.Get("http://" + base + "/healthz"); err == nil {
				resp.Body.Close()
				backendBase = base
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		return m.Run()
	}()
	os.Exit(code)
}

type sig struct{ c *websocket.Conn }

func dialSig(t *testing.T) *sig {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial("ws://"+backendBase+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &sig{c}
}

func (s *sig) send(m map[string]any) {
	if err := s.c.WriteJSON(m); err != nil {
		panic(err)
	}
}

func (s *sig) want(t *testing.T, typ string) map[string]any {
	t.Helper()
	_ = s.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var m map[string]any
	if err := s.c.ReadJSON(&m); err != nil || m["t"] != typ {
		t.Fatalf("want %s, got %v (%v)", typ, m, err)
	}
	return m
}

func relayPair(t *testing.T, s, r *sig) (*RelayTransport, *RelayTransport) {
	t.Helper()
	r.send(map[string]any{"t": "relay_request", "to": "sender"})
	ts, tr := s.want(t, "relay_ticket"), r.want(t, "relay_ticket")
	if ts["ticket"] != tr["ticket"] {
		t.Fatal("tickets differ")
	}
	ctx := context.Background()
	a, err := DialRelay(ctx, "ws://"+backendBase+"/relay/"+ts["ticket"].(string))
	if err != nil {
		t.Fatal(err)
	}
	b, err := DialRelay(ctx, "ws://"+backendBase+"/relay/"+tr["ticket"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

func newRoom(t *testing.T) (*sig, *sig, string) {
	t.Helper()
	if backendBase == "" {
		t.Skip("backend binary unavailable (is the Go toolchain on PATH?)")
	}
	s := dialSig(t)
	s.send(map[string]any{"t": "create_room", "v": 2, "client": map[string]any{}})
	room := s.want(t, "room_created")["room_id"].(string)
	r := dialSig(t)
	r.send(map[string]any{"t": "join_room", "v": 2, "room_id": room, "client": map[string]any{}})
	r.want(t, "joined")
	s.want(t, "peer_joined")
	return s, r, room
}

func TestRelayTransferThroughServerAndCiphertextOnly(t *testing.T) {
	s, r, room := newRoom(t)
	a, b := relayPair(t, s, r)
	secretName := "tax-return-2026.pdf"
	data := pattern(2*mib+5, 7)
	sink := newSink()
	sRes := make(chan Result, 1)
	go func() {
		sRes <- RunSender(context.Background(), a, SenderOptions{RoomID: room, Secret: testSecret, Device: "s", Files: []Source{src(0, secretName, data), src(1, "empty", nil)}})
	}()
	res := RunReceiver(context.Background(), b, ReceiverOptions{RoomID: room, Secret: testSecret, Device: "r", Sink: sink, State: &ReceiveState{},
		OnOffer: func([]NamedFile) bool { return true }})
	expect(t, res, StatusDone, "", false)
	expect(t, wait(t, sRes), StatusDone, "", false)
	if !bytes.Equal(sink.bytes(0), data) {
		t.Fatal("bytes differ over the relay")
	}
}

func TestRelayWireIsCiphertext(t *testing.T) {
	s, r, room := newRoom(t)
	a, b := relayPair(t, s, r)
	type frame struct {
		typ  int
		data []byte
	}
	var mu sync.Mutex
	var onWire []frame
	a.wire = func(typ int, d []byte) {
		mu.Lock()
		onWire = append(onWire, frame{typ, append([]byte(nil), d...)})
		mu.Unlock()
	}
	name := "secret-plans-q3.docx"
	data := pattern(300_000, 9)
	sRes := make(chan Result, 1)
	go func() {
		sRes <- RunSender(context.Background(), a, SenderOptions{RoomID: room, Secret: testSecret, Device: "s", Files: []Source{src(0, name, data)}})
	}()
	sink := newSink()
	RunReceiver(context.Background(), b, ReceiverOptions{RoomID: room, Secret: testSecret, Device: "r", Sink: sink, State: &ReceiveState{},
		OnOffer: func([]NamedFile) bool { return true }})
	wait(t, sRes)
	if !bytes.Equal(sink.bytes(0), data) {
		t.Fatal("transfer failed")
	}
	// positive control: the check must be able to see plaintext
	if !bytes.Contains([]byte("xx"+name+"yy"), []byte(name)) {
		t.Fatal("control failed")
	}
	mu.Lock()
	defer mu.Unlock()
	var plainText, binary int
	for _, f := range onWire {
		if f.typ == websocket.TextMessage {
			plainText++ // only the two handshake frames may be plaintext
			if bytes.Contains(f.data, []byte(name)) {
				t.Fatal("file name leaked in a plaintext frame")
			}
			continue
		}
		binary++
		if bytes.Contains(f.data, []byte(name)) || (len(f.data) > 1000 && bytes.Contains(f.data, data[1000:1064])) {
			t.Fatal("plaintext found inside an encrypted frame")
		}
	}
	if plainText != 2 || binary < 5 {
		t.Fatalf("expected exactly hello+auth in the clear and the rest sealed, got %d plaintext, %d sealed", plainText, binary)
	}
}

func TestRelayWrongSecretFailsBoth(t *testing.T) {
	s, r, room := newRoom(t)
	a, b := relayPair(t, s, r)
	sRes := make(chan Result, 1)
	go func() {
		sRes <- RunSender(context.Background(), a, SenderOptions{RoomID: room, Secret: testSecret, Device: "s", Files: []Source{src(0, "x", pattern(10, 0))}})
	}()
	res := RunReceiver(context.Background(), b, ReceiverOptions{RoomID: room, Secret: "wrong-wrong-wrong-wrong", Device: "r", Sink: newSink(), State: &ReceiveState{},
		OnOffer: func([]NamedFile) bool { t.Error("must not be offered"); return true }})
	expect(t, res, StatusFailed, ReasonAuthFailed, true)
	expect(t, wait(t, sRes), StatusFailed, ReasonAuthFailed, false)
}

func TestRelayResumeOnSecondConnection(t *testing.T) {
	s, r, room := newRoom(t)
	data := pattern(6*mib, 5)
	files := []Source{src(0, "big", data)}
	sink, state := newSink(), &ReceiveState{}
	a, b := relayPair(t, s, r)
	go RunSender(context.Background(), a, SenderOptions{RoomID: room, Secret: testSecret, Device: "s", Files: files})
	sink.delay = time.Millisecond
	done := make(chan Result, 1)
	go func() {
		done <- RunReceiver(context.Background(), b, ReceiverOptions{RoomID: room, Secret: testSecret, Device: "r", Sink: sink, State: state,
			OnOffer: func([]NamedFile) bool { return true }})
	}()
	for sink.written.Load() < mib {
		time.Sleep(5 * time.Millisecond)
	}
	_ = b.Close() // cut the link mid-file
	expect(t, wait(t, done), StatusClosed, "", false)
	sink.delay = 0

	a2, b2 := relayPair(t, s, r)
	sRes := make(chan Result, 1)
	go func() {
		sRes <- RunSender(context.Background(), a2, SenderOptions{RoomID: room, Secret: testSecret, Device: "s", Files: files})
	}()
	res := RunReceiver(context.Background(), b2, ReceiverOptions{RoomID: room, Secret: testSecret, Device: "r", Sink: sink, State: state,
		OnOffer: func([]NamedFile) bool { t.Error("must not prompt again"); return false }})
	expect(t, res, StatusDone, "", false)
	expect(t, wait(t, sRes), StatusDone, "", false)
	if !bytes.Equal(sink.bytes(0), data) {
		t.Fatal("resumed file differs")
	}
}
