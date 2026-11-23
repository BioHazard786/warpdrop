package mobile

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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
		build.Dir = "../../backend"
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

type sendEvents struct {
	room chan [2]string
	end  chan string
}

func (s *sendEvents) OnRoom(id, secret, _ string)                      { s.room <- [2]string{id, secret} }
func (s *sendEvents) OnPeer(_, _, _, _, _ string, _, _ int64, _ int32) {}
func (s *sendEvents) OnPeerGone(string)                                {}
func (s *sendEvents) OnStatus(string)                                  {}
func (s *sendEvents) OnEnd(err string)                                 { s.end <- err }

// tmpSink writes into temp files and "publishes" them by recording the final bytes; failed ones are dropped.
type tmpSink struct {
	mu        sync.Mutex
	dir       string
	files     map[int32]*os.File
	published map[string][]byte
	names     map[int32]string
}

func newSink(t *testing.T) *tmpSink {
	return &tmpSink{dir: t.TempDir(), files: map[int32]*os.File{}, published: map[string][]byte{}, names: map[int32]string{}}
}

func (s *tmpSink) Open(name string, _ int64, idx int32) (int32, error) {
	f, err := os.Create(filepath.Join(s.dir, fmt.Sprintf("%d.tmp", idx)))
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.files[idx], s.names[idx] = f, name
	s.mu.Unlock()
	fd, err := syscall.Dup(int(f.Fd())) // Go closes the dup, we keep f to read back
	return int32(fd), err
}

func (s *tmpSink) Finish(idx int32, ok bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		b, err := os.ReadFile(s.files[idx].Name())
		s.published[s.names[idx]] = b
		return err
	}
	return os.Remove(s.files[idx].Name())
}

type recvEvents struct {
	accept     bool
	onProgress func()
	offer      chan int32
	end        chan string
}

func (r *recvEvents) OnStatus(string, string)   {}
func (r *recvEvents) OnOffer(offer *Offer) bool { r.offer <- offer.Count(); return r.accept }
func (r *recvEvents) OnProgress(int32, int64) {
	if r.onProgress != nil {
		r.onProgress()
	}
}
func (r *recvEvents) OnFileDone(int32) {}
func (r *recvEvents) OnEnd(err string) { r.end <- err }

func writeTemp(t *testing.T, data []byte) int32 {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "src"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Dup(int(f.Fd()))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	return int32(fd)
}

func startSend(t *testing.T, files map[string][]byte) (*SendJob, *sendEvents, [2]string) {
	t.Helper()
	if serverURL == "" {
		t.Skip("backend binary unavailable (is the Go toolchain on PATH?)")
	}
	job := NewSendJob(serverURL, "test-sender", false)
	for name, data := range files {
		job.AddFile(writeTemp(t, data), name, "application/octet-stream", int64(len(data)))
	}
	ev := &sendEvents{room: make(chan [2]string, 1), end: make(chan string, 1)}
	job.Start(ev)
	select {
	case r := <-ev.room:
		t.Cleanup(job.Cancel)
		return job, ev, r
	case err := <-ev.end:
		t.Fatalf("room not created: %s", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timeout creating room")
	}
	return nil, nil, [2]string{}
}

func wait(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(30 * time.Second):
		t.Fatal("timeout")
	}
	return ""
}

func TestSendReceiveRoundTrip(t *testing.T) {
	a := bytes.Repeat([]byte("warp"), 300_000)
	b := []byte("tiny")
	_, _, room := startSend(t, map[string][]byte{"a.bin": a, "b.txt": b})

	sink := newSink(t)
	ev := &recvEvents{accept: true, offer: make(chan int32, 1), end: make(chan string, 1)}
	NewReceiveJob(serverURL, "test-receiver", false).Start(room[0]+"-"+room[1], sink, ev)

	if n := <-ev.offer; n != 2 {
		t.Fatalf("offer has %d files, want 2", n)
	}
	if err := wait(t, ev.end); err != "" {
		t.Fatal(err)
	}
	if !bytes.Equal(sink.published["a.bin"], a) || !bytes.Equal(sink.published["b.txt"], b) {
		t.Fatalf("published files differ: %d entries", len(sink.published))
	}
}

func TestDeclinedOfferPublishesNothing(t *testing.T) {
	_, _, room := startSend(t, map[string][]byte{"x.bin": []byte("secret stuff")})
	sink := newSink(t)
	ev := &recvEvents{accept: false, offer: make(chan int32, 1), end: make(chan string, 1)}
	NewReceiveJob(serverURL, "test-receiver", false).Start(room[0]+"-"+room[1], sink, ev)
	wait(t, ev.end)
	if len(sink.published) != 0 {
		t.Fatalf("declined transfer published %d files", len(sink.published))
	}
}

func TestCancelMidTransferDiscardsPartial(t *testing.T) {
	_, _, room := startSend(t, map[string][]byte{"big.bin": bytes.Repeat([]byte("0123456789abcdef"), 1<<20)}) // 16 MB
	sink := newSink(t)
	job := NewReceiveJob(serverURL, "test-receiver", false)
	ev := &recvEvents{accept: true, offer: make(chan int32, 1), end: make(chan string, 1)}
	ev.onProgress = job.Cancel
	job.Start(room[0]+"-"+room[1], sink, ev)
	wait(t, ev.end)
	if len(sink.published) != 0 {
		t.Fatal("cancelled transfer published a file")
	}
	if left, _ := filepath.Glob(filepath.Join(sink.dir, "*.tmp")); len(left) != 0 {
		t.Fatalf("partial files left behind: %v", left)
	}
}

func TestBareRoomNumberIsRejected(t *testing.T) {
	job := NewReceiveJob("http://127.0.0.1:1", "d", false)
	ev := &recvEvents{offer: make(chan int32, 1), end: make(chan string, 1)}
	job.Start("4821", nil, ev)
	if wait(t, ev.end) == "" {
		t.Fatal("expected an error for a code without secret words")
	}
}

// ---- nearby ----

type nearbyEvents struct {
	changed chan struct{}
	invite  chan [4]string // device id, name, room, ticket
	end     chan string
}

func newNearbyEvents() *nearbyEvents {
	return &nearbyEvents{changed: make(chan struct{}, 8), invite: make(chan [4]string, 1), end: make(chan string, 1)}
}
func (n *nearbyEvents) OnDevices() {
	select {
	case n.changed <- struct{}{}:
	default:
	}
}
func (n *nearbyEvents) OnInvite(id, name, room, ticket string) {
	n.invite <- [4]string{id, name, room, ticket}
}
func (n *nearbyEvents) OnServer(string)  {}
func (n *nearbyEvents) OnEnd(err string) { n.end <- err }

// peerEvents records the sender-side view of the peer so the test can ask for its nearby code.
type peerEvents struct {
	sendEvents
	waiting chan string // peer id, once the receiver is asked to accept
}

func (p *peerEvents) OnPeer(id, _, _, status, _ string, _, _ int64, _ int32) {
	if status == "waiting" {
		select {
		case p.waiting <- id:
		default:
		}
	}
}

type offerEvents struct {
	recvEvents
	sas chan string
}

func (o *offerEvents) OnOffer(offer *Offer) bool { o.sas <- offer.SAS(); return o.accept }

// The API the Android and desktop apps use: list devices, invite one, send, receive, and both screens show one code.
func TestNearbyRoundTripShowsTheSameCodeOnBothSides(t *testing.T) {
	if serverURL == "" {
		t.Skip("backend binary unavailable")
	}
	rcvList, sndList := newNearbyEvents(), newNearbyEvents()
	rcvNearby := NewNearbyJob(serverURL, "Phone", false)
	rcvNearby.Start(rcvList)
	t.Cleanup(rcvNearby.Stop)
	sndNearby := NewNearbyJob(serverURL, "Laptop", true) // hidden: it can see and send, but is not listed
	sndNearby.Start(sndList)
	t.Cleanup(sndNearby.Stop)

	for sndNearby.DeviceCount() == 0 {
		select {
		case <-sndList.changed:
		case err := <-sndList.end:
			t.Fatalf("sender lobby ended: %q", err)
		case <-time.After(10 * time.Second):
			t.Fatal("the phone never showed up in the list")
		}
	}
	if sndNearby.DeviceCount() != 1 || sndNearby.DeviceName(0) != "Phone" {
		t.Fatalf("want exactly the phone, got %d devices", sndNearby.DeviceCount())
	}
	if rcvNearby.DeviceCount() != 0 {
		t.Fatalf("a hidden sender must not be listed, but the phone sees %d device(s)", rcvNearby.DeviceCount())
	}

	data := bytes.Repeat([]byte("nearby!"), 100_000)
	send := NewSendJob(serverURL, "Laptop", false)
	send.AddFile(writeTemp(t, data), "n.bin", "application/octet-stream", int64(len(data)))
	sev := &peerEvents{sendEvents: sendEvents{room: make(chan [2]string, 1), end: make(chan string, 1)}, waiting: make(chan string, 1)}
	send.StartNearby(sndNearby, sndNearby.DeviceID(0), sev)
	t.Cleanup(send.Cancel)
	if r := <-sev.room; r[1] != "" {
		t.Fatalf("a nearby room must not have secret words, got %q", r[1])
	}

	var inv [4]string
	select {
	case inv = <-rcvList.invite:
	case <-time.After(10 * time.Second):
		t.Fatal("no invite")
	}
	if inv[1] != "Laptop" {
		t.Fatalf("invite from %q", inv[1])
	}
	sink := newSink(t)
	rev := &offerEvents{recvEvents: recvEvents{accept: true, offer: make(chan int32, 1), end: make(chan string, 1)}, sas: make(chan string, 1)}
	NewReceiveJob(serverURL, "Phone", false).StartNearby(inv[2], inv[3], sink, rev)

	rcvSAS := <-rev.sas
	peer := <-sev.waiting
	if len(rcvSAS) != 6 || rcvSAS != send.SAS(peer) {
		t.Fatalf("codes differ: receiver %q sender %q", rcvSAS, send.SAS(peer))
	}
	if err := wait(t, rev.end); err != "" {
		t.Fatalf("receive: %s", err)
	}
	if !bytes.Equal(sink.published["n.bin"], data) {
		t.Fatal("received bytes differ")
	}
}

// Direct transfer between two devices on a network, as an app that does its own discovery uses it: no server is
// reachable at all, the receiver listens, the sender is told about it, and both screens show one code.
func TestNearbyDirectWithoutServer(t *testing.T) {
	const dead = "http://127.0.0.1:1"
	rcv := NewNearbyJob(dead, "Phone", false)
	rcv.UseExternalDiscovery()
	rev := &offerEvents{recvEvents: recvEvents{accept: true, offer: make(chan int32, 1), end: make(chan string, 4)}, sas: make(chan string, 1)}
	sink := newSink(t)
	port, err := rcv.ListenLAN(oneLAN{rev, sink, nil})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rcv.Stop)

	snd, list := NewNearbyJob(dead, "Laptop", true), newNearbyEvents()
	snd.UseExternalDiscovery()
	snd.Start(list)
	t.Cleanup(snd.Stop)
	snd.AddLANPeer("abc", "Phone", "127.0.0.1", port)
	if snd.DeviceCount() != 1 || snd.DeviceName(0) != "Phone" || !snd.DeviceDirect(0) || snd.DeviceID(0) != "lan:abc" {
		t.Fatalf("want one direct phone, got %d", snd.DeviceCount())
	}

	data := bytes.Repeat([]byte("direct!"), 100_000)
	send := NewSendJob(dead, "Laptop", false)
	send.AddFile(writeTemp(t, data), "d.bin", "application/octet-stream", int64(len(data)))
	sev := &peerEvents{sendEvents: sendEvents{room: make(chan [2]string, 1), end: make(chan string, 1)}, waiting: make(chan string, 1)}
	send.StartNearby(snd, snd.DeviceID(0), sev)
	t.Cleanup(send.Cancel)

	rcvSAS := <-rev.sas
	peer := <-sev.waiting
	if peer != "lan" || len(rcvSAS) != 6 || rcvSAS != send.SAS(peer) {
		t.Fatalf("peer %q, codes receiver %q sender %q", peer, rcvSAS, send.SAS(peer))
	}
	if err := wait(t, rev.end); err != "" {
		t.Fatalf("receive: %s", err)
	}
	if err := wait(t, sev.end); err != "" {
		t.Fatalf("send: %s", err)
	}
	if !bytes.Equal(sink.published["d.bin"], data) {
		t.Fatal("received bytes differ")
	}

	snd.RemoveLANPeer("abc")
	if snd.DeviceCount() != 0 {
		t.Fatal("removed device still listed")
	}
}

// Desktop style: files by path in, a folder out, no file descriptors crossing over.
func TestPathsInFolderOut(t *testing.T) {
	const dead = "http://127.0.0.1:1"
	dir := t.TempDir()
	sink, err := NewDirSink(filepath.Join(dir, "in"))
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	rcv := NewNearbyJob(dead, "Phone", false)
	rcv.UseExternalDiscovery()
	rev := &offerEvents{recvEvents: recvEvents{accept: true, offer: make(chan int32, 1), end: make(chan string, 4)}, sas: make(chan string, 1)}
	port, err := rcv.ListenLAN(oneLAN{rev, sink, nil})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rcv.Stop)
	snd := NewNearbyJob(dead, "Laptop", true)
	snd.UseExternalDiscovery()
	snd.Start(newNearbyEvents())
	t.Cleanup(snd.Stop)
	snd.AddLANPeer("abc", "Phone", "127.0.0.1", port)

	data := bytes.Repeat([]byte("path!"), 50_000)
	src := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	send := NewSendJob(dead, "Laptop", false)
	if err := send.AddPath(dir); err == nil {
		t.Fatal("a directory must be refused")
	}
	if err := send.AddPath(src); err != nil {
		t.Fatal(err)
	}
	sev := &peerEvents{sendEvents: sendEvents{room: make(chan [2]string, 1), end: make(chan string, 1)}, waiting: make(chan string, 1)}
	send.StartNearby(snd, snd.DeviceID(0), sev)
	t.Cleanup(send.Cancel)
	<-rev.sas
	if err := wait(t, rev.end); err != "" {
		t.Fatalf("receive: %s", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "in", "p.txt"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file not saved correctly: %v", err)
	}
}

// oneLAN hands every direct connection the same listener and sink, and reports each connection's id.
type oneLAN struct {
	l   ReceiveListener
	s   Sink
	ids chan string
}

func (o oneLAN) Listener(id string) ReceiveListener {
	if o.ids != nil {
		o.ids <- id
	}
	return o.l
}
func (o oneLAN) Sink(string) Sink { return o.s }

// The receiver stops a direct transfer it already accepted, and the sender hears "cancelled" quickly.
func TestCancelLANReceive(t *testing.T) {
	const dead = "http://127.0.0.1:1"
	rcv := NewNearbyJob(dead, "Phone", false)
	rcv.UseExternalDiscovery()
	rev := &offerEvents{recvEvents: recvEvents{accept: true, offer: make(chan int32, 1), end: make(chan string, 4)}, sas: make(chan string, 1)}
	ids := make(chan string, 1)
	port, err := rcv.ListenLAN(oneLAN{rev, newSink(t), ids})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rcv.Stop)
	snd := NewNearbyJob(dead, "Laptop", true)
	snd.UseExternalDiscovery()
	snd.Start(newNearbyEvents())
	t.Cleanup(snd.Stop)
	snd.AddLANPeer("abc", "Phone", "127.0.0.1", port)

	data := bytes.Repeat([]byte("x"), 64<<20) // big enough to still be running when we cancel
	send := NewSendJob(dead, "Laptop", false)
	send.AddFile(writeTemp(t, data), "big.bin", "application/octet-stream", int64(len(data)))
	sev := &peerEvents{sendEvents: sendEvents{room: make(chan [2]string, 1), end: make(chan string, 1)}, waiting: make(chan string, 1)}
	send.StartNearby(snd, snd.DeviceID(0), sev)
	t.Cleanup(send.Cancel)

	id := <-ids
	<-rev.sas // accepted
	start := time.Now()
	rcv.CancelLAN(id)
	if err := wait(t, sev.end); err != "cancelled" {
		t.Fatalf("sender should hear cancelled, got %q", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("sender took %v to notice", d)
	}
}

// A device that is busy turns the invite down, and the sender stops waiting with "device is busy".
func TestNearbyDeclineEndsTheSend(t *testing.T) {
	if serverURL == "" {
		t.Skip("backend binary unavailable")
	}
	rcvList, sndList := newNearbyEvents(), newNearbyEvents()
	rcvNearby := NewNearbyJob(serverURL, "Phone", false)
	rcvNearby.Start(rcvList)
	t.Cleanup(rcvNearby.Stop)
	sndNearby := NewNearbyJob(serverURL, "Laptop", true)
	sndNearby.Start(sndList)
	t.Cleanup(sndNearby.Stop)
	for sndNearby.DeviceCount() == 0 {
		select {
		case <-sndList.changed:
		case <-time.After(10 * time.Second):
			t.Fatal("the phone never showed up in the list")
		}
	}
	send := NewSendJob(serverURL, "Laptop", false)
	send.AddFile(writeTemp(t, []byte("hi")), "n.bin", "application/octet-stream", 2)
	sev := &peerEvents{sendEvents: sendEvents{room: make(chan [2]string, 1), end: make(chan string, 1)}, waiting: make(chan string, 1)}
	send.StartNearby(sndNearby, sndNearby.DeviceID(0), sev)
	t.Cleanup(send.Cancel)

	inv := <-rcvList.invite
	rcvNearby.Decline(inv[2], inv[3], true)
	if err := wait(t, sev.end); err != "device is busy" {
		t.Fatalf("sender: %q", err)
	}
}

// A dropped server connection (laptop sleep, Wi-Fi change) must not leave the device out of the list for good.
func TestNearbyRejoinsAfterTheConnectionDrops(t *testing.T) {
	if serverURL == "" {
		t.Skip("backend binary unavailable")
	}
	target := strings.TrimPrefix(serverURL, "http://")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", target)
			if err != nil {
				c.Close()
				continue
			}
			mu.Lock()
			conns = append(conns, c, up)
			mu.Unlock()
			go func() { io.Copy(up, c); up.Close() }()
			go func() { io.Copy(c, up); c.Close() }()
		}
	}()

	watcher := NewNearbyJob(serverURL, "Watcher", false)
	watcher.Start(newNearbyEvents())
	t.Cleanup(watcher.Stop)
	flaky := NewNearbyJob("http://"+ln.Addr().String(), "Flaky", false)
	flaky.Start(newNearbyEvents())
	t.Cleanup(flaky.Stop)

	waitFor := func(want int32, what string) {
		t.Helper()
		for end := time.Now().Add(15 * time.Second); watcher.DeviceCount() != want; time.Sleep(50 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("%s: the watcher sees %d device(s), want %d", what, watcher.DeviceCount(), want)
			}
		}
	}
	waitFor(1, "first join")
	mu.Lock()
	for _, c := range conns {
		c.Close()
	}
	conns = nil
	mu.Unlock()
	waitFor(0, "after the drop")
	waitFor(1, "after the automatic rejoin")
}
