// Package core is the desktop app's logic: it drives the shared Go core (cli/mobile) and turns its callbacks into
// plain state and events. It knows nothing about windows, so it is tested without a GUI; main.go binds it to Wails.
package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/mobile"
)

type Device struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Direct bool   `json:"direct"` // also reachable straight over the local network
}

type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Transfer is everything the UI shows about one send or receive.
type Transfer struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`   // send | receive
	Status    string `json:"status"` // listening | connecting | authenticating | waiting | offered | sending | receiving | done | failed | declined | cancelled
	Peer      string `json:"peer"`
	Nearby    bool   `json:"nearby"`    // sent to a device picked from the list: no code, and Peer is known from the start
	To        string `json:"to"`        // nearby sends: the device id it went to
	Transport string `json:"transport"` // "", rtc, relay or lan
	Sent      int64  `json:"sent"`
	Total     int64  `json:"total"`
	SAS       string `json:"sas"`  // nearby only: must match the other screen
	Code      string `json:"code"` // sending with a share code
	Link      string `json:"link"`
	Files     []File `json:"files"`
	Error     string `json:"error"`
	Done      bool   `json:"done"`
	Started   int64  `json:"started"` // unix ms
	// Paths are where received files were saved, by file index: a clash gets "name (1).ext", so not always Files' names.
	Paths []string `json:"paths"`
}

type State struct {
	Version   string     `json:"version"`
	Settings  Settings   `json:"settings"` // what the Settings page edits: the device name and folder in use, the saved server
	Device    string     `json:"device"`
	Dir       string     `json:"dir"`
	Visible   bool       `json:"visible"`
	Server    string     `json:"server"` // "" when connected to the server's device list
	Devices   []Device   `json:"devices"`
	Transfers []Transfer `json:"transfers"`
}

// Config holds the defaults; anything saved on the Settings page wins over them.
type Config struct {
	Server string // "" = the hosted one
	Dir    string // where received files go, default ~/Downloads
	Device string // the name other devices see, default "<host> on <OS>"
	// StateDir keeps settings and finished transfers across restarts; "" keeps nothing (tests).
	StateDir string
}

type rec struct {
	t      Transfer
	cancel func()
	decide chan bool // receive: the user's answer to the offer
	prefix []int64
}

type Engine struct {
	emit     func(event string, data any)
	base     Config
	stateDir string
	saveMu   sync.Mutex // one writer at a time for the state files

	mu        sync.Mutex
	server    string
	device    string
	sink      *mobile.DirSink
	oldSinks  []*mobile.DirSink // folders switched away from: receives may still be writing there, closed on Shutdown
	saved     Settings
	visible   bool
	nearby    *mobile.NearbyJob
	serverErr string
	devices   []Device
	transfers map[string]*rec
	order     []string
	seq       int
}

// New opens the download folder and loads saved settings and history. Nothing runs until Start.
func New(c Config, emit func(event string, data any)) (*Engine, error) {
	e := &Engine{emit: emit, base: c, stateDir: c.StateDir, transfers: map[string]*rec{}}
	e.readJSON(settingsFile, &e.saved)
	eff, err := e.effective(e.saved)
	if err != nil {
		return nil, err
	}
	sink, err := mobile.NewDirSink(eff.Dir)
	if err != nil && e.saved.Dir != "" { // the saved folder is gone (an unplugged drive): fall back to the default
		e.saved.Dir = ""
		if eff, err = e.effective(e.saved); err == nil {
			sink, err = mobile.NewDirSink(eff.Dir)
		}
	}
	if err != nil {
		return nil, err
	}
	e.server, e.device, e.sink = eff.Server, eff.Device, sink
	e.loadHistory()
	return e, nil
}

// effective fills what s leaves empty from the defaults.
func (e *Engine) effective(s Settings) (Settings, error) {
	if s.Server == "" {
		s.Server = e.base.Server
	}
	if s.Dir == "" {
		s.Dir = e.base.Dir
	}
	if s.Dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return s, err
		}
		s.Dir = filepath.Join(home, "Downloads")
	}
	if s.Device == "" {
		s.Device = e.base.Device
	}
	if s.Device == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "desktop"
		}
		s.Device = host + " on " + map[string]string{"linux": "Linux", "windows": "Windows", "darwin": "macOS"}[runtime.GOOS]
	}
	return s, nil
}

// conn is what a new job needs, read under the lock because Settings can change them.
func (e *Engine) conn() (server, device string, sink *mobile.DirSink) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.server, e.device, e.sink
}

// Start makes this device visible on the network and starts looking for others.
func (e *Engine) Start() error { return e.SetVisible(true) }

// Shutdown stops discovery and cancels every transfer.
func (e *Engine) Shutdown() {
	e.mu.Lock()
	n := e.nearby
	var cancels []func()
	for _, r := range e.transfers {
		if !r.t.Done && r.cancel != nil {
			cancels = append(cancels, r.cancel)
		}
	}
	e.mu.Unlock()
	if n != nil {
		n.Stop()
	}
	for _, c := range cancels {
		c()
	}
	e.mu.Lock()
	sinks := append(e.oldSinks, e.sink)
	e.mu.Unlock()
	for _, s := range sinks {
		_ = s.Close()
	}
}

func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	set := e.saved
	set.Device, set.Dir = e.device, e.sink.Dir()
	s := State{Version: Version, Settings: set, Device: e.device, Dir: e.sink.Dir(), Visible: e.visible, Server: e.serverErr, Devices: append([]Device{}, e.devices...), Transfers: []Transfer{}}
	for _, id := range e.order {
		s.Transfers = append(s.Transfers, e.transfers[id].t)
	}
	return s
}

// ---- nearby

// SetVisible chooses whether other devices see this one. Either way this device still sees them and can send.
func (e *Engine) SetVisible(v bool) error {
	e.mu.Lock()
	old := e.nearby
	n := mobile.NewNearbyJob(e.server, e.device, !v)
	e.nearby, e.visible, e.devices = n, v, nil
	e.mu.Unlock()
	if old != nil {
		old.Stop()
	}
	e.emit("visible", v)
	e.emit("devices", []Device{})
	n.Start(&nearbyEvents{e: e, n: n})
	if !v {
		return nil
	}
	if _, err := n.ListenLAN(lanHandler{e, n}); err != nil {
		return fmt.Errorf("others on this network cannot send to you directly: %w", err)
	}
	return nil
}

type nearbyEvents struct {
	e *Engine
	n *mobile.NearbyJob
}

func (l *nearbyEvents) OnDevices() {
	ds := make([]Device, l.n.DeviceCount())
	for i := range ds {
		ds[i] = Device{ID: l.n.DeviceID(int32(i)), Name: l.n.DeviceName(int32(i)), Direct: l.n.DeviceDirect(int32(i))}
	}
	l.e.mu.Lock()
	if l.e.nearby != l.n { // a stale job (visibility was toggled)
		l.e.mu.Unlock()
		return
	}
	l.e.devices = ds
	l.e.mu.Unlock()
	l.e.emit("devices", ds)
}

func (l *nearbyEvents) OnServer(err string) {
	l.e.mu.Lock()
	cur := l.e.nearby == l.n
	if cur {
		l.e.serverErr = err
	}
	l.e.mu.Unlock()
	if cur {
		l.e.emit("server", err)
	}
}

func (l *nearbyEvents) OnInvite(_, _, roomID, ticket string) {
	r := &receiver{e: l.e}
	r.begin()
	server, device, sink := l.e.conn()
	job := mobile.NewReceiveJob(server, device, false)
	l.e.setCancel(r.cur, job.Cancel)
	job.StartNearby(roomID, ticket, sink, r)
}

func (l *nearbyEvents) OnEnd(string) {}

// lanHandler makes each direct connection its own transfer, stoppable through CancelLAN.
type lanHandler struct {
	e *Engine
	n *mobile.NearbyJob
}

func (h lanHandler) Listener(id string) mobile.ReceiveListener {
	r := &receiver{e: h.e}
	r.begin()
	h.e.setCancel(r.cur, func() { h.n.CancelLAN(id) })
	return r
}

func (h lanHandler) Sink(string) mobile.Sink {
	_, _, sink := h.e.conn()
	return sink
}

// ---- transfers

func (e *Engine) add(kind string, files []File, cancel func()) string {
	e.mu.Lock()
	e.seq++
	id := fmt.Sprintf("t%d", e.seq)
	r := &rec{cancel: cancel, decide: make(chan bool, 1)}
	r.t = Transfer{ID: id, Kind: kind, Status: "connecting", Files: files, Total: sum(files), Started: time.Now().UnixMilli()}
	e.transfers[id] = r
	e.order = append(e.order, id)
	t := r.t
	e.mu.Unlock()
	e.emit("transfer", t)
	return id
}

func (e *Engine) setCancel(id string, c func()) {
	e.mu.Lock()
	e.transfers[id].cancel = c
	e.mu.Unlock()
}

// update changes one transfer and tells the UI. A finished transfer is never changed again.
func (e *Engine) update(id string, f func(*rec)) {
	e.mu.Lock()
	r := e.transfers[id]
	if r == nil || r.t.Done {
		e.mu.Unlock()
		return
	}
	f(r)
	t := r.t
	e.mu.Unlock()
	e.emit("transfer", t)
	if t.Done { // happens once per transfer: a finished one is never updated again
		e.saveHistory()
	}
}

func sum(fs []File) (n int64) {
	for _, f := range fs {
		n += f.Size
	}
	return n
}

func prefixes(fs []File) []int64 {
	var p []int64
	var n int64
	for _, f := range fs {
		p = append(p, n)
		n += f.Size
	}
	return p
}

// files lists paths for the UI, refusing anything that is not a regular file before a job is created.
func files(paths []string) ([]File, error) {
	if len(paths) == 0 {
		return nil, errors.New("no files chosen")
	}
	var out []File
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a file: zip folders first", filepath.Base(p))
		}
		out = append(out, File{Name: filepath.Base(p), Size: st.Size()})
	}
	return out, nil
}

// Describe lists name and size for paths the user picked, so the UI can show them before sending.
func (e *Engine) Describe(paths []string) ([]File, error) { return files(paths) }

// SendToDevice sends files to a device from the nearby list and returns the transfer id.
func (e *Engine) SendToDevice(deviceID string, paths []string) (string, error) {
	return e.send(paths, deviceID)
}

// SendWithCode shares files with a code and link anyone can use, and returns the transfer id.
func (e *Engine) SendWithCode(paths []string) (string, error) { return e.send(paths, "") }

func (e *Engine) send(paths []string, deviceID string) (string, error) {
	fs, err := files(paths)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	n, server, device := e.nearby, e.server, e.device
	e.mu.Unlock()
	job := mobile.NewSendJob(server, device, false)
	for _, p := range paths {
		if err := job.AddPath(p); err != nil {
			job.Cancel()
			return "", err
		}
	}
	id := e.add("send", fs, job.Cancel)
	if deviceID != "" {
		e.mu.Lock()
		for _, d := range e.devices {
			if d.ID == deviceID {
				e.transfers[id].t.Peer = d.Name
			}
		}
		e.transfers[id].t.Nearby, e.transfers[id].t.To = true, deviceID
		t := e.transfers[id].t
		e.mu.Unlock()
		e.emit("transfer", t)
	}
	l := &sender{e: e, id: id, job: job}
	if deviceID == "" {
		job.Start(l)
	} else {
		job.StartNearby(n, deviceID, l)
	}
	return id, nil
}

type sender struct {
	e   *Engine
	id  string
	job *mobile.SendJob
}

func (l *sender) OnRoom(room, secret, link string) {
	l.e.update(l.id, func(r *rec) {
		r.t.Status = "listening"
		if secret != "" {
			r.t.Code, r.t.Link = room+"-"+secret, link
		}
	})
}

func (l *sender) OnPeer(id, device, transport, status, reason string, sent, total int64, _ int32) {
	sas := ""
	if status == "waiting" {
		sas = l.job.SAS(id)
	}
	l.e.update(l.id, func(r *rec) {
		r.t.Peer, r.t.Transport, r.t.Status, r.t.Sent, r.t.Error = device, transport, status, sent, reason
		if sas != "" {
			r.t.SAS = sas
		}
		if status == "done" {
			r.t.Done = true
		}
	})
	if status == "done" {
		go func() { time.Sleep(300 * time.Millisecond); l.job.Cancel() }() // let the last frames land, then close the room
	}
}

func (l *sender) OnPeerGone(string) {}
func (l *sender) OnStatus(string)   {}
func (l *sender) OnEnd(err string)  { l.e.finish(l.id, err) }

// finish ends a transfer: an error, or a plain end that was not already a completed, declined or cancelled one.
func (e *Engine) finish(id, err string) {
	e.update(id, func(r *rec) {
		r.t.Done = true
		switch {
		case r.t.Status == "declined" || r.t.Status == "cancelled" || r.t.Status == "done": // already decided; the job's error just echoes it
		case err != "":
			r.t.Status, r.t.Error = "failed", err
		case r.t.Kind == "receive" && r.t.Status == "receiving":
			r.t.Status = "done"
		default:
			r.t.Status = "cancelled"
		}
	})
}

// ReceiveCode receives from a share code or link and returns the transfer id.
func (e *Engine) ReceiveCode(code string) (string, error) {
	r := &receiver{e: e}
	r.begin()
	server, device, sink := e.conn()
	job := mobile.NewReceiveJob(server, device, false)
	e.setCancel(r.cur, job.Cancel)
	job.Start(code, sink, r)
	return r.cur, nil
}

// receiver is the listener for one incoming transfer.
type receiver struct {
	e   *Engine
	cur string
}

func (r *receiver) begin() {
	r.cur = r.e.add("receive", nil, nil)
}

func (r *receiver) OnStatus(status, transport string) {
	if r.cur == "" {
		r.begin()
	}
	r.e.update(r.cur, func(x *rec) {
		x.t.Transport = transport
		if status == "offered" || status == "receiving" || status == "authenticating" || status == "connecting" {
			x.t.Status = status
		}
	})
}

func (r *receiver) OnOffer(o *mobile.Offer) bool {
	if r.cur == "" {
		r.begin()
	}
	fs := make([]File, o.Count())
	for i := range fs {
		fs[i] = File{Name: o.Name(int32(i)), Size: o.Size(int32(i))}
	}
	id := r.cur
	var ch chan bool
	r.e.update(id, func(x *rec) {
		x.t.Peer, x.t.SAS, x.t.Files, x.t.Total, x.t.Status = o.Sender(), o.SAS(), fs, sum(fs), "offered"
		x.prefix, ch = prefixes(fs), x.decide
	})
	if ch == nil {
		return false
	}
	ok := <-ch
	if !ok {
		r.e.update(id, func(x *rec) {
			if x.t.Status != "cancelled" {
				x.t.Status = "declined"
			}
		})
	}
	return ok
}

func (r *receiver) OnProgress(idx int32, n int64) {
	r.e.update(r.cur, func(x *rec) {
		if int(idx) < len(x.prefix) {
			x.t.Sent = x.prefix[idx] + n
		}
	})
}

func (r *receiver) OnFileDone(int32) {}

// OnSaved records where file idx is being written, for "Show in folder" once it's done.
func (r *receiver) OnSaved(idx int32, path string) {
	r.e.update(r.cur, func(x *rec) {
		for len(x.t.Paths) <= int(idx) {
			x.t.Paths = append(x.t.Paths, "")
		}
		x.t.Paths[idx] = path
	})
}

func (r *receiver) OnEnd(err string) {
	if r.cur == "" {
		return
	}
	r.e.finish(r.cur, err)
}

// Respond answers an offer: true accepts and the files are saved to the download folder.
func (e *Engine) Respond(id string, accept bool) {
	e.mu.Lock()
	r := e.transfers[id]
	e.mu.Unlock()
	if r != nil {
		select {
		case r.decide <- accept:
		default:
		}
	}
}

// Cancel stops a transfer, whichever side it is on.
func (e *Engine) Cancel(id string) {
	e.mu.Lock()
	r := e.transfers[id]
	ignore := r == nil || (r.cancel == nil && r.t.Status != "offered")
	e.mu.Unlock()
	if ignore {
		return
	}
	e.update(id, func(x *rec) {
		if x.t.Status != "done" {
			x.t.Status = "cancelled"
		}
	})
	select { // a pending offer ends with "no"
	case r.decide <- false:
	default:
	}
	if r.cancel != nil {
		r.cancel()
	}
}

// Dismiss removes a finished transfer from the list.
func (e *Engine) Dismiss(id string) {
	e.mu.Lock()
	if r := e.transfers[id]; r != nil && r.t.Done {
		delete(e.transfers, id)
		for i, o := range e.order {
			if o == id {
				e.order = append(e.order[:i], e.order[i+1:]...)
				break
			}
		}
	}
	e.mu.Unlock()
	e.emit("dismissed", id)
	e.saveHistory()
}
