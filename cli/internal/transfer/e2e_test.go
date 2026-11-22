package transfer

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
)

var serverURL string

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
			return m.Run()
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
				serverURL = "http://" + base
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		return m.Run()
	}()
	os.Exit(code)
}

func pattern(n, seed int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + seed + (i >> 8))
	}
	return b
}

func source(id int, name string, data []byte) protocol.Source {
	return protocol.Source{Entry: protocol.FileEntry{ID: id, Name: name, Size: int64(len(data))}, R: bytes.NewReader(data)}
}

type sent struct {
	roomID, secret string
	done           chan error
	cancel         context.CancelFunc
	mu             sync.Mutex
	peers          map[string]PeerView
}

// startSender creates a room. The server rate-limits room creation (burst 5, then one per 6 s per IP), so when it
// answers "too many requests" we wait for a token instead of failing: tests must not depend on how many rooms ran before.
func startSender(t *testing.T, cfg config.Config, files []protocol.Source) *sent {
	t.Helper()
	if serverURL == "" {
		t.Skip("backend binary unavailable (is the Go toolchain on PATH?)")
	}
	srv, err := cfg.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		s := &sent{done: make(chan error, 1), cancel: cancel, peers: map[string]PeerView{}}
		ready := make(chan struct{})
		go func() {
			s.done <- RunSend(ctx, cfg, srv, files, "test-sender", SendHooks{
				OnRoom: func(room, secret string) { s.roomID, s.secret = room, secret; close(ready) },
				OnPeer: func(v PeerView) { s.mu.Lock(); s.peers[v.ID] = v; s.mu.Unlock() },
			})
		}()
		select {
		case <-ready:
			t.Cleanup(cancel)
			return s
		case err := <-s.done:
			cancel()
			if err == nil || !strings.Contains(err.Error(), "too many requests") {
				t.Fatalf("room not created: %v", err)
			}
			time.Sleep(6500 * time.Millisecond)
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatal("room not created (timeout)")
		}
	}
	t.Fatal("room creation stayed rate limited")
	return nil
}

func (s *sent) view() (PeerView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.peers {
		return v, true
	}
	return PeerView{}, false
}

func receive(t *testing.T, cfg config.Config, s *sent, dir string, hooks ReceiveHooks) error {
	t.Helper()
	srv, err := cfg.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sink, err := NewDiskSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if hooks.OnOffer == nil {
		hooks.OnOffer = func(string, []protocol.NamedFile) bool { return true }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return RunReceive(ctx, cfg, srv, s.roomID, s.secret, "test-receiver", sink, hooks)
}

func readAll(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return b
}

func TestSendReceiveOverWebRTC(t *testing.T) {
	cfg := config.Config{Server: serverURL}
	a, b := pattern(3<<20+9, 1), pattern(100, 2)
	s := startSender(t, cfg, []protocol.Source{source(0, "big.bin", a), source(1, "big.bin", b), source(2, "empty", nil)})
	dir := t.TempDir()
	var kind string
	err := receive(t, cfg, s, dir, ReceiveHooks{OnStatus: func(st, tr string) {
		if st == "receiving" {
			kind = tr
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readAll(t, dir, "big.bin"), a) || !bytes.Equal(readAll(t, dir, "big (1).bin"), b) || len(readAll(t, dir, "empty")) != 0 {
		t.Fatal("received files differ")
	}
	if kind != "rtc" {
		t.Logf("note: transfer used %q (WebRTC did not connect in this environment)", kind)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 3 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestSendReceiveForcedRelay(t *testing.T) {
	cfg := config.Config{Server: serverURL, ForceRelay: true}
	data := pattern(2<<20, 4)
	s := startSender(t, cfg, []protocol.Source{source(0, "r.bin", data)})
	dir := t.TempDir()
	var kind string
	if err := receive(t, cfg, s, dir, ReceiveHooks{OnStatus: func(st, tr string) {
		if st == "receiving" {
			kind = tr
		}
	}}); err != nil {
		t.Fatal(err)
	}
	if kind != "relay" {
		t.Fatalf("forced relay used %q", kind)
	}
	if !bytes.Equal(readAll(t, dir, "r.bin"), data) {
		t.Fatal("bytes differ")
	}
}

func TestWrongSecretIsReportedAndNothingIsWritten(t *testing.T) {
	cfg := config.Config{Server: serverURL, ForceRelay: true}
	s := startSender(t, cfg, []protocol.Source{source(0, "x", pattern(10, 0))})
	s.secret = "wrong-wrong-wrong-wrong"
	dir := t.TempDir()
	err := receive(t, cfg, s, dir, ReceiveHooks{OnOffer: func(string, []protocol.NamedFile) bool { t.Error("must not be offered"); return true }})
	if err != ErrWrongSecret {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("wrote %v", entries)
	}
}

func TestUnknownRoomIsAClearError(t *testing.T) {
	cfg := config.Config{Server: serverURL}
	if serverURL == "" {
		t.Skip("backend binary unavailable")
	}
	// no real room needed (the server allows only 5 room creations per minute per IP, so tests must not waste them)
	err := receive(t, cfg, &sent{roomID: "0000", secret: protocol.GenerateSecret()}, t.TempDir(), ReceiveHooks{})
	if err == nil || err.Error() == "" {
		t.Fatal("must fail with a message")
	}
}

func TestDeclinedByUser(t *testing.T) {
	cfg := config.Config{Server: serverURL, ForceRelay: true}
	s := startSender(t, cfg, []protocol.Source{source(0, "x", pattern(10, 0))})
	dir := t.TempDir()
	err := receive(t, cfg, s, dir, ReceiveHooks{OnOffer: func(string, []protocol.NamedFile) bool { return false }})
	if err == nil {
		t.Fatal("decline must be an error exit")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("wrote %v", entries)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := s.view(); ok && v.Status == "failed" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("sender never saw the decline")
}

func TestReceiverSurvivesCuttingTheLinkMidFile(t *testing.T) {
	cfg := config.Config{Server: serverURL, ForceRelay: true}
	data := pattern(8<<20, 7)
	s := startSender(t, cfg, []protocol.Source{source(0, "resume.bin", data)})
	dir := t.TempDir()
	var cut sync.Once
	var cutDone bool
	// kill the receiver's link once, partway through
	hooks := ReceiveHooks{
		OnProgress: func(_ int, b int64) {
			if b > 1<<20 {
				cut.Do(func() { cutDone = true; killLinks() })
			}
		},
	}
	if err := receive(t, cfg, s, dir, hooks); err != nil {
		t.Fatalf("receive failed after a link cut: %v", err)
	}
	if !cutDone {
		t.Fatal("test never cut the link")
	}
	if !bytes.Equal(readAll(t, dir, "resume.bin"), data) {
		t.Fatal("resumed file differs from the original")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftover .part files: %v", entries)
	}
}

// Regression: the receiver used to hang up right after its last frame, so the sender never learned the transfer
// finished and stayed "sending" forever.
func TestSenderLearnsTheTransferCompleted(t *testing.T) {
	for _, relay := range []bool{false, true} {
		cfg := config.Config{Server: serverURL, ForceRelay: relay}
		s := startSender(t, cfg, []protocol.Source{source(0, "x", pattern(300_000, 1)), source(1, "y", pattern(10, 2))})
		if err := receive(t, cfg, s, t.TempDir(), ReceiveHooks{}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if v, ok := s.view(); ok && v.Status == "done" {
				break
			}
			if time.Now().After(deadline) {
				v, _ := s.view()
				t.Fatalf("relay=%v: sender never saw completion, still %q", relay, v.Status)
			}
			time.Sleep(20 * time.Millisecond)
		}
		s.cancel()
	}
}

func tempEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Regression: interrupting the sender used to leave the receiver with a stray .part file and no useful message.
// Both transports: the cancel frame must survive the sender hanging up right after sending it.
func TestSenderInterruptedMidTransferTellsReceiverAndLeavesNoPartFile(t *testing.T) {
	for _, relay := range []bool{true, false} {
		name := map[bool]string{true: "relay", false: "webrtc"}[relay]
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{Server: serverURL, ForceRelay: relay}
			s := startSender(t, cfg, []protocol.Source{source(0, "big.bin", pattern(64<<20, 3))})
			dir := t.TempDir()
			var once sync.Once
			err := receive(t, cfg, s, dir, ReceiveHooks{OnProgress: func(_ int, b int64) {
				if b > 1<<20 {
					once.Do(s.cancel) // the sender user presses Ctrl-C
				}
			}})
			if err == nil || !strings.Contains(err.Error(), "cancelled") {
				t.Fatalf("receiver should report the sender cancelled, got %v", err)
			}
			if left := tempEntries(t, dir); len(left) != 0 {
				t.Fatalf("leftover files after the sender cancelled: %v", left)
			}
		})
	}
}

// Regression: when the sender vanishes without a word, giving up must also discard the partial file.
func TestSenderVanishingLeavesNoPartFile(t *testing.T) {
	cfg := config.Config{Server: serverURL, ForceRelay: true}
	s := startSender(t, cfg, []protocol.Source{source(0, "big.bin", pattern(64<<20, 4))})
	dir := t.TempDir()
	var once sync.Once
	err := receive(t, cfg, s, dir, ReceiveHooks{OnProgress: func(_ int, b int64) {
		if b > 1<<20 {
			once.Do(func() { killLinks(); s.cancel() }) // transport dies and the room is gone: nothing to resume into
		}
	}})
	if err == nil {
		t.Fatal("must fail when the sender is gone for good")
	}
	if left := tempEntries(t, dir); len(left) != 0 {
		t.Fatalf("leftover files after giving up: %v", left)
	}
}
