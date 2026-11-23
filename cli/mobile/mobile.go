// Package mobile is the thin layer the native apps call: gomobile for Android (and later Apple platforms), plain
// Go imports for the desktop app. It only exposes basic types and small callback interfaces; file bytes never
// cross the boundary because files are passed as open file descriptors that Go reads and writes itself.
package mobile

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/BioHazard786/Warpdrop/cli/internal/lan"
	"github.com/BioHazard786/Warpdrop/cli/internal/nearby"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
	"github.com/BioHazard786/Warpdrop/cli/internal/transfer"
	"github.com/wlynxg/anet"
)

// progressEvery limits how often progress callbacks cross the FFI boundary.
const progressEvery = 100 * time.Millisecond

// SetAndroidSDK lets pion list network interfaces on Android 11+ (netlink is blocked there). Call it once at startup.
func SetAndroidSDK(sdk int32) { anet.SetAndroidVersion(uint(sdk)) }

// DefaultServer is the hosted server's URL.
func DefaultServer() string { return config.DefaultServer }

// ParseShare reports whether input is a share code or link; roomID and secret come back via the two getters of the result.
func ParseShare(input string) *Share {
	s, ok := protocol.ParseShare(input)
	return &Share{Valid: ok, RoomID: s.RoomID, Secret: s.Secret}
}

// Share is a parsed code or link. Secret is empty when the input was only a room number.
type Share struct {
	Valid          bool
	RoomID, Secret string
}

// ---- send ----

type SendListener interface {
	OnRoom(roomID, secret, link string)
	OnPeer(id, device, transport, status, reason string, sent, total int64, filesDone int32)
	OnPeerGone(id string)
	OnStatus(msg string)
	// OnEnd is the last call. err is "" when the job was cancelled or finished cleanly.
	OnEnd(err string)
}

// SendJob collects files, then Start runs the transfer. The job owns every fd it is given and closes them at the end.
type SendJob struct {
	server, device string
	relay          bool
	srcs           []protocol.Source
	fds            []*os.File
	cancel         context.CancelFunc
	mu             sync.Mutex
	nearby         *NearbyJob // set by StartNearby
	to             string
	sas            map[string]string // peer id -> nearby code, once known
}

func NewSendJob(server, device string, forceRelay bool) *SendJob {
	return &SendJob{server: server, device: device, relay: forceRelay}
}

// AddFile hands over an open, seekable fd. size must be the file's exact size.
func (j *SendJob) AddFile(fd int32, name, mime string, size int64) {
	f := os.NewFile(uintptr(fd), name)
	j.fds = append(j.fds, f)
	j.srcs = append(j.srcs, protocol.Source{
		Entry: protocol.FileEntry{ID: len(j.srcs), Name: name, Size: size, Type: mime},
		R:     f,
	})
}

// AddPath adds a file by path, for apps that have real paths (desktop). Directories are refused: zip them first.
func (j *SendJob) AddPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	name := filepath.Base(path)
	mt := mime.TypeByExtension(filepath.Ext(name))
	if mt == "" {
		mt = "application/octet-stream"
	}
	j.fds = append(j.fds, f)
	j.srcs = append(j.srcs, protocol.Source{Entry: protocol.FileEntry{ID: len(j.srcs), Name: name, Size: st.Size(), Type: mt}, R: f})
	return nil
}

// Start runs the transfer in the background; l.OnEnd always fires exactly once.
func (j *SendJob) Start(l SendListener) { j.start(l) }

// StartNearby sends to a device from n's list instead of sharing a code. A device on the local network is reached
// directly over TCP (peer id "lan", transport "lan", no OnRoom); one only in the server's list is invited (OnRoom
// gets an empty secret and link). SAS(peerID) returns the code both screens must show once OnPeer reports "waiting".
func (j *SendJob) StartNearby(n *NearbyJob, deviceID string, l SendListener) {
	j.nearby, j.to = n, deviceID
	j.start(l)
}

// SAS is the nearby code for a peer ("" until the handshake finished, or for a share-code transfer).
func (j *SendJob) SAS(peerID string) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.sas[peerID]
}

func (j *SendJob) start(l SendListener) {
	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	j.cancel = cancel
	j.mu.Unlock()
	go func() {
		defer func() {
			for _, f := range j.fds {
				_ = f.Close()
			}
		}()
		l.OnEnd(errString(j.run(ctx, l)))
	}()
}

func (j *SendJob) run(ctx context.Context, l SendListener) error {
	cfg, err := config.Resolve(j.server, j.relay)
	if err != nil {
		return err
	}
	last := map[string]time.Time{}
	var mu sync.Mutex
	hooks := transfer.SendHooks{
		OnRoom: func(room, secret string) {
			link := ""
			if secret != "" { // nearby has nothing to share
				link = cfg.ShareLink(room, secret)
			}
			l.OnRoom(room, secret, link)
		},
		OnPeer: func(v transfer.PeerView) {
			if v.SAS != "" {
				j.mu.Lock()
				if j.sas == nil {
					j.sas = map[string]string{}
				}
				j.sas[v.ID] = v.SAS
				j.mu.Unlock()
			}
			mu.Lock()
			if v.Status == "sending" && time.Since(last[v.ID]) < progressEvery {
				mu.Unlock()
				return
			}
			last[v.ID] = time.Now()
			mu.Unlock()
			l.OnPeer(v.ID, v.Device, v.Transport, v.Status, v.Reason, v.Sent, v.Total, int32(v.FilesDone))
		},
		OnPeerGone: l.OnPeerGone,
		OnStatus:   l.OnStatus,
	}
	// a device on the local network is sent to directly, which also works with no internet
	var lobby *nearby.Lobby
	if j.nearby != nil {
		d, ok := j.nearby.lookup(j.to)
		if !ok {
			return errors.New("that device is no longer on the list")
		}
		lobby = j.nearby.lobby()
		if d.LAN != nil {
			c, err := lan.Dial(ctx, *d.LAN)
			if err == nil {
				return transfer.RunSendLAN(ctx, c, d.Name, j.srcs, j.device, hooks)
			}
			// nothing answers there: most likely a stale announcement from a device that is gone (killed
			// without saying goodbye). Drop it so the list stops offering it; a live device is found again.
			j.nearby.RemoveLANPeer(d.LAN.ID)
			if !d.InLobby() || lobby == nil {
				return err
			}
			l.OnStatus(err.Error() + ": using the server instead")
		}
	}
	srv, err := cfg.Fetch(ctx)
	if err != nil {
		return err
	}
	if j.nearby == nil {
		return transfer.RunSend(ctx, cfg, srv, j.srcs, j.device, hooks)
	}
	if lobby == nil {
		return errors.New("not connected to the server's device list")
	}
	return transfer.RunSendNearby(ctx, cfg, srv, j.srcs, j.device, hooks, func(roomID, token string) error {
		return lobby.Invite(j.to, roomID, token)
	})
}

// Cancel stops the job; active receivers are told "cancelled".
func (j *SendJob) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
}

// ---- receive ----

// Offer is the file list the sender announced, and the sender's device name.
type Offer struct {
	from  string
	sas   string
	files []protocol.NamedFile
}

func (o *Offer) Sender() string { return o.from }

// SAS is the 6-digit code to show beside the offer for a nearby transfer; "" for a share-code transfer.
func (o *Offer) SAS() string         { return o.sas }
func (o *Offer) Count() int32        { return int32(len(o.files)) }
func (o *Offer) Name(i int32) string { return o.files[i].SafeName }
func (o *Offer) Size(i int32) int64  { return o.files[i].Size }
func (o *Offer) Mime(i int32) string { return o.files[i].Type }
func (o *Offer) TotalSize() (n int64) {
	for _, f := range o.files {
		n += f.Size
	}
	return n
}

type ReceiveListener interface {
	// OnStatus: connecting | negotiating | authenticating | offered | receiving | reconnecting. transport is "", "rtc" or "relay".
	OnStatus(status, transport string)
	// OnOffer blocks the transfer until you answer; return true to accept.
	OnOffer(offer *Offer) bool
	OnProgress(idx int32, bytes int64)
	OnFileDone(idx int32)
	// OnEnd is the last call. err is "" when every file arrived and verified (or the job was cancelled).
	OnEnd(err string)
}

// Sink is where received files go. The app opens a destination and returns a writable fd that Go then owns.
type Sink interface {
	// Open reserves a destination for file idx and returns a writable fd (Go closes it).
	Open(name string, size int64, idx int32) (int32, error)
	// Finish is called after the fd is closed: ok means the checksum matched and the file may be published;
	// otherwise the destination must be deleted. Returning an error on ok=true fails the transfer.
	Finish(idx int32, ok bool) error
}

// DirSink saves received files straight into a folder (desktop), never overwriting: a clash gets "name (1).ext".
// Pass it wherever a Sink is wanted; the app never calls its methods. Close it when the app is done receiving.
type DirSink struct {
	d *transfer.DiskSink
	Sink
}

// NewDirSink opens (creating if needed) the folder.
func NewDirSink(dir string) (*DirSink, error) {
	d, err := transfer.NewDiskSink(dir)
	if err != nil {
		return nil, err
	}
	return &DirSink{d: d}, nil
}

func (s *DirSink) Dir() string  { return s.d.Dir }
func (s *DirSink) Close() error { return s.d.Close() }

// savedListener is optional (Go callers only, so gomobile never sees it): a ReceiveListener that also has OnSaved
// learns where each file lands in a DirSink, which may differ from its name ("name (1).ext") to avoid overwriting.
type savedListener interface {
	OnSaved(idx int32, path string)
}

// namingSink is a DiskSink that reports each file's final path as it is reserved.
type namingSink struct {
	*transfer.DiskSink
	l savedListener
}

func (n namingSink) Open(f protocol.NamedFile, idx int) (protocol.FileWriter, error) {
	w, err := n.DiskSink.Open(f, idx)
	if named, ok := w.(interface{ FinalName() string }); ok && err == nil {
		n.l.OnSaved(int32(idx), filepath.Join(n.Dir, named.FinalName()))
	}
	return w, err
}

// asSink picks the writer for the job: a DirSink writes itself, anything else hands Go a file descriptor.
func asSink(s Sink, l ReceiveListener) protocol.Sink {
	if d, ok := s.(*DirSink); ok {
		if sl, ok := l.(savedListener); ok {
			return namingSink{d.d, sl}
		}
		return d.d
	}
	return fdSink{s}
}

type ReceiveJob struct {
	server, device string
	relay          bool
	cancel         context.CancelFunc
	mu             sync.Mutex
}

func NewReceiveJob(server, device string, forceRelay bool) *ReceiveJob {
	return &ReceiveJob{server: server, device: device, relay: forceRelay}
}

// Start joins the share (code or link) in the background; l.OnEnd always fires exactly once.
func (j *ReceiveJob) Start(code string, sink Sink, l ReceiveListener) {
	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	j.cancel = cancel
	j.mu.Unlock()
	go func() { l.OnEnd(errString(j.run(ctx, code, sink, l))) }()
}

// StartNearby answers an invite (roomID and ticket come from NearbyListener.OnInvite); l.OnOffer's Offer.SAS() is
// the code to compare with the sender's screen. l.OnEnd always fires exactly once.
func (j *ReceiveJob) StartNearby(roomID, ticket string, sink Sink, l ReceiveListener) {
	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	j.cancel = cancel
	j.mu.Unlock()
	go func() {
		l.OnEnd(errString(j.receive(ctx, roomID, "", ticket, sink, l)))
	}()
}

func (j *ReceiveJob) run(ctx context.Context, code string, sink Sink, l ReceiveListener) error {
	share, ok := protocol.ParseShare(code)
	if !ok || share.Secret == "" {
		return errors.New("that is not a full WarpDrop code or link (it needs the secret words)")
	}
	return j.receive(ctx, share.RoomID, share.Secret, "", sink, l)
}

// receive runs a share-code transfer (secret) or a nearby one (ticket).
func (j *ReceiveJob) receive(ctx context.Context, roomID, secret, ticket string, sink Sink, l ReceiveListener) error {
	cfg, err := config.Resolve(j.server, j.relay)
	if err != nil {
		return err
	}
	srv, err := cfg.Fetch(ctx)
	if err != nil {
		return err
	}
	hooks := receiveHooks(l)
	if ticket != "" {
		return transfer.RunReceiveNearby(ctx, cfg, srv, roomID, ticket, j.device, asSink(sink, l), hooks)
	}
	return transfer.RunReceive(ctx, cfg, srv, roomID, secret, j.device, asSink(sink, l), hooks)
}

// receiveHooks adapts a ReceiveListener, thinning progress calls and carrying the nearby code into the offer.
func receiveHooks(l ReceiveListener) transfer.ReceiveHooks {
	var mu sync.Mutex
	var lastProgress time.Time
	var sas string
	return transfer.ReceiveHooks{
		OnStatus: l.OnStatus,
		OnSAS:    func(s string) { sas = s },
		OnOffer:  func(from string, files []protocol.NamedFile) bool { return l.OnOffer(&Offer{from, sas, files}) },
		OnProgress: func(idx int, n int64) {
			mu.Lock()
			if time.Since(lastProgress) < progressEvery {
				mu.Unlock()
				return
			}
			lastProgress = time.Now()
			mu.Unlock()
			l.OnProgress(int32(idx), n)
		},
		OnFileDone: func(idx int) { l.OnFileDone(int32(idx)) },
	}
}

func (j *ReceiveJob) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
}

// fdSink adapts the app's Sink to protocol.Sink. The data is written to a temporary destination the app owns
// and only published (Finish ok=true) after Close, i.e. after the checksum matched.
type fdSink struct{ s Sink }

func (k fdSink) Open(f protocol.NamedFile, idx int) (protocol.FileWriter, error) {
	fd, err := k.s.Open(f.SafeName, f.Size, int32(idx))
	if err != nil {
		return nil, err
	}
	return &fdWriter{f: os.NewFile(uintptr(fd), f.SafeName), s: k.s, idx: int32(idx)}, nil
}

type fdWriter struct {
	f    *os.File
	s    Sink
	idx  int32
	done bool
}

func (w *fdWriter) Write(p []byte) error { _, err := w.f.Write(p); return err }

func (w *fdWriter) Close() error {
	if w.done {
		return nil
	}
	w.done = true
	if err := w.f.Sync(); err != nil {
		// not every fd is syncable (some providers); only a real I/O error should fail the file
		var pe *os.PathError
		if errors.As(err, &pe) && pe.Err.Error() != "invalid argument" {
			_ = w.f.Close()
			_ = w.s.Finish(w.idx, false)
			return err
		}
	}
	if err := w.f.Close(); err != nil {
		_ = w.s.Finish(w.idx, false)
		return err
	}
	return w.s.Finish(w.idx, true)
}

func (w *fdWriter) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	_ = w.f.Close()
	return w.s.Finish(w.idx, false)
}

func errString(err error) string {
	if err == nil || errors.Is(err, context.Canceled) {
		return ""
	}
	return fmt.Sprint(err)
}

// ---- nearby: devices on the same network ----

// NearbyListener hears about changes to the device list and about invites. Calls never overlap.
type NearbyListener interface {
	// OnDevices means the list changed: read it with DeviceCount, DeviceID and DeviceName.
	OnDevices()
	// OnServer reports the server's list: err is "" once connected, otherwise why it is not (devices on the local
	// network are still found and still work). Called again if the connection drops. There is no automatic retry.
	OnServer(err string)
	// OnInvite: a device wants to send to us through the server. Answer with ReceiveJob.StartNearby(roomID, ticket,
	// ...), or NearbyJob.Decline.
	OnInvite(deviceID, deviceName, roomID, ticket string)
	// OnEnd is the last call, after Stop. err is "" unless the job could not start.
	OnEnd(err string)
}

// NearbyJob keeps this device in the same-network list. hidden = it still sees the others and can send, but is
// not listed to them (so it cannot receive invites). Keep one running while the app is open.
//
// Devices come from two places that are merged into one list: the server (the same public network, works for
// browsers too) and the local network itself (mDNS: direct TCP, faster, works offline). Go does the mDNS by default.
// An app whose platform wants to do it itself (Android's NsdManager) calls UseExternalDiscovery before Start and
// reports what it finds through AddLANPeer and RemoveLANPeer, and announces itself using the port from ListenLAN.
type NearbyJob struct {
	server, device string
	hidden         bool
	external       bool
	id             string // our mDNS id, so we do not list ourselves
	mu             sync.Mutex
	l              *nearby.Lobby
	lobbyDevs      []nearby.Device
	peers          map[string]lan.Peer
	devices        []nearby.Device
	cancel         context.CancelFunc
	listener       NearbyListener
	cb             sync.Mutex // serialises calls into the listener
	lanCancel      context.CancelFunc
	lanConns       map[string]context.CancelFunc // live direct receives, for CancelLAN
	lanSeq         int
}

// LANHandler gives each direct connection its own listener and sink, so several devices can send at once.
// For each connection Listener(id) is called first, then Sink(id), then the usual ReceiveListener calls ending
// in OnEnd. id is what CancelLAN takes.
type LANHandler interface {
	Listener(id string) ReceiveListener
	Sink(id string) Sink
}

func NewNearbyJob(server, device string, hidden bool) *NearbyJob {
	return &NearbyJob{server: server, device: device, hidden: hidden, id: lan.NewID(), peers: map[string]lan.Peer{}}
}

// UseExternalDiscovery turns off Go's own mDNS. Call it before Start and ListenLAN.
func (j *NearbyJob) UseExternalDiscovery() { j.external = true }

// AddLANPeer adds (or updates) a device found by the app's own discovery. id must be stable per device.
func (j *NearbyJob) AddLANPeer(id, name, host string, port int32) {
	p := lan.Peer{ID: id, Name: protocol.DisplayName(name), Addrs: []string{net.JoinHostPort(host, strconv.Itoa(int(port)))}}
	if p.Name == "" {
		p.Name = "device"
	}
	j.mu.Lock()
	j.peers[id] = p
	j.mu.Unlock()
	j.refresh()
}

func (j *NearbyJob) RemoveLANPeer(id string) {
	j.mu.Lock()
	delete(j.peers, id)
	j.mu.Unlock()
	j.refresh()
}

// refresh rebuilds the merged list and tells the app.
func (j *NearbyJob) refresh() {
	j.mu.Lock()
	peers := make([]lan.Peer, 0, len(j.peers))
	for _, p := range j.peers {
		peers = append(peers, p)
	}
	j.devices = nearby.Merge(j.lobbyDevs, peers)
	l := j.listener
	j.mu.Unlock()
	if l != nil {
		j.cb.Lock()
		l.OnDevices()
		j.cb.Unlock()
	}
}

// Start joins the lists in the background; l.OnEnd fires exactly once, when Stop is called.
func (j *NearbyJob) Start(l NearbyListener) {
	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	j.cancel = cancel
	j.listener = l
	j.mu.Unlock()
	go func() { l.OnEnd(errString(j.run(ctx, l))) }()
}

func (j *NearbyJob) run(ctx context.Context, l NearbyListener) error {
	cfg, err := config.Resolve(j.server, false)
	if err != nil {
		return err
	}
	if !j.external {
		go func() {
			// shortcut: if this network cannot do mDNS the list is just the server's; surface it if apps need to say so
			_ = lan.Browse(ctx, j.id, func(ps []lan.Peer) {
				j.mu.Lock()
				j.peers = make(map[string]lan.Peer, len(ps))
				for _, p := range ps {
					j.peers[p.ID] = p
				}
				j.mu.Unlock()
				j.refresh()
			})
		}()
	}
	j.joinServer(ctx, cfg, l)
	<-ctx.Done()
	return nil
}

// joinServer keeps this device in the server's list until ctx ends: when the connection drops (laptop sleep, a
// Wi-Fi change, a restarted server) it rejoins with growing pauses instead of leaving the list empty for good.
// Failure is reported, not fatal.
func (j *NearbyJob) joinServer(ctx context.Context, cfg config.Config, l NearbyListener) {
	report := func(msg string) {
		j.cb.Lock()
		l.OnServer(msg)
		j.cb.Unlock()
	}
	for pause := time.Second; ctx.Err() == nil; pause = min(pause*2, 30*time.Second) {
		lobby, err := nearby.Join(ctx, cfg, j.device, j.hidden)
		if err != nil {
			if ctx.Err() == nil {
				report(err.Error())
			}
		} else {
			pause = time.Second / 2 // doubled to 1s by the loop: a drop after a good session retries quickly
			j.mu.Lock()
			j.l = lobby
			j.lobbyDevs = lobby.Devices()
			j.mu.Unlock()
			report("")
			j.refresh()
			j.follow(ctx, lobby, l)
			if ctx.Err() == nil {
				report("lost the connection to the server, reconnecting")
			}
		}
		select {
		case <-time.After(pause):
		case <-ctx.Done():
		}
	}
}

// follow relays the lobby's events until its connection ends or ctx is done, then clears the server's part of the list.
func (j *NearbyJob) follow(ctx context.Context, lobby *nearby.Lobby, l NearbyListener) {
	defer lobby.Close()
	for {
		select {
		case e, ok := <-lobby.Events:
			if !ok {
				j.mu.Lock()
				j.l, j.lobbyDevs = nil, nil
				j.mu.Unlock()
				j.refresh()
				return
			}
			if e.Kind == "invite" {
				j.cb.Lock()
				l.OnInvite(e.Device.ID, e.Device.Name, e.RoomID, e.Ticket)
				j.cb.Unlock()
				continue
			}
			j.mu.Lock()
			j.lobbyDevs = lobby.Devices()
			j.mu.Unlock()
			j.refresh()
		case <-ctx.Done():
			return
		}
	}
}

// Decline turns down an invite from OnInvite so the sender stops waiting; busy says we are mid-transfer rather
// than "no". Without a server connection it does nothing (the sender gives up on its own after a minute).
func (j *NearbyJob) Decline(roomID, ticket string, busy bool) {
	reason := "declined"
	if busy {
		reason = "busy"
	}
	if l := j.lobby(); l != nil {
		_ = l.Decline(roomID, ticket, reason)
	}
}

// lobby is the live connection to the server's list; nil when not connected.
func (j *NearbyJob) lobby() *nearby.Lobby {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.l
}

func (j *NearbyJob) lookup(id string) (nearby.Device, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, d := range j.devices {
		if d.ID == id {
			return d, true
		}
	}
	return nearby.Device{}, false
}

func (j *NearbyJob) DeviceCount() int32 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return int32(len(j.devices))
}

// The list can change between DeviceCount and these calls (another thread adds or drops a device), and an index
// out of range would take the whole app down: so they answer "" / false instead.
func (j *NearbyJob) DeviceID(i int32) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if i < 0 || int(i) >= len(j.devices) {
		return ""
	}
	return j.devices[i].ID
}

func (j *NearbyJob) DeviceName(i int32) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if i < 0 || int(i) >= len(j.devices) {
		return ""
	}
	return j.devices[i].Name
}

// DeviceDirect says whether device i can be reached directly over the local network.
func (j *NearbyJob) DeviceDirect(i int32) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return i >= 0 && int(i) < len(j.devices) && j.devices[i].LAN != nil
}

// ListenLAN lets devices on the local network send to us directly. It returns the TCP port: with external
// discovery the app must announce it ("_warpdrop._tcp", TXT v=2 id=<any> name=<device>), otherwise Go does.
// A few connections are handled at once (more are turned away), and a device whose offer was turned down is
// ignored for a while. Each gets its listener and sink from h (see LANHandler). Stop ends it.
func (j *NearbyJob) ListenLAN(h LANHandler) (int32, error) {
	if j.hidden {
		return 0, errors.New("a hidden device cannot receive")
	}
	var ln net.Listener
	var err error
	if j.external {
		ln, err = net.Listen("tcp", ":0")
	} else {
		ln, err = lan.Listen(j.id, j.device, "mobile")
	}
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	if j.lanCancel != nil {
		j.lanCancel() // replace an earlier listener
	}
	j.lanCancel = cancel
	j.mu.Unlock()
	go transfer.ServeLAN(ctx, ln, func(c net.Conn) (bool, error) {
		cctx, cancel := context.WithCancel(ctx)
		j.mu.Lock()
		j.lanSeq++
		id := strconv.Itoa(j.lanSeq)
		if j.lanConns == nil {
			j.lanConns = map[string]context.CancelFunc{}
		}
		j.lanConns[id] = cancel
		j.mu.Unlock()
		defer func() {
			cancel()
			j.mu.Lock()
			delete(j.lanConns, id)
			j.mu.Unlock()
		}()
		l := h.Listener(id)
		offered, err := transfer.ReceiveLAN(cctx, c, j.device, asSink(h.Sink(id), l), receiveHooks(l))
		l.OnEnd(errString(err))
		return offered, err
	})
	return int32(ln.Addr().(*net.TCPAddr).Port), nil
}

// CancelLAN stops the direct receive id (from LANHandler); the sender is told "cancelled". Unknown ids are ignored.
func (j *NearbyJob) CancelLAN(id string) {
	j.mu.Lock()
	cancel := j.lanConns[id]
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Stop leaves both lists and stops listening.
func (j *NearbyJob) Stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
	if j.lanCancel != nil {
		j.lanCancel()
	}
}
