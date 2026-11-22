// Package lan finds other WarpDrop devices on the same network over mDNS (DNS-SD, "_warpdrop._tcp") and accepts
// direct TCP connections from them. No server is involved, so it works offline.
package lan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
	"github.com/libp2p/zeroconf/v2"
)

const (
	Service = "_warpdrop._tcp"

	round     = 12 * time.Second // how often the network is asked again
	listen    = 3 * time.Second  // how long each round waits for answers
	missLimit = 2                // rounds a device may stay silent before it is dropped
)

// Peer is another device. Addrs are "host:port" and all of them belong to the same device (it may be on several
// networks), so a caller dials them in turn.
type Peer struct {
	ID    string
	Name  string
	Addrs []string
}

// NewID makes the random id a process advertises under and uses to ignore its own announcement.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Listener accepts incoming connections and announces itself to the network until Close.
type Listener struct {
	net.Listener
	srv *zeroconf.Server
}

// Listen opens a TCP port and announces it as "name". client is "cli", "android", ...
func Listen(id, name, client string) (*Listener, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}
	txt := []string{"v=2", "id=" + id, "name=" + truncate(name, 120), "client=" + client}
	port := ln.Addr().(*net.TCPAddr).Port
	// Our own host name, not the machine's: the OS's mDNS daemon (avahi, Bonjour) already answers for
	// "<hostname>.local" with every address it has, docker bridges included, and a resolver may pick any of them.
	ifaces, ips := lanAddrs()
	var srv *zeroconf.Server
	if len(ips) > 0 {
		srv, err = zeroconf.RegisterProxy(id, Service, "local.", port, "warpdrop-"+id, ips, txt, ifaces)
	} else { // nothing that looks like a LAN: announce everything rather than nothing
		srv, err = zeroconf.Register(id, Service, "local.", port, txt, nil)
	}
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("cannot announce on the network: %w", err)
	}
	return &Listener{Listener: ln, srv: srv}, nil
}

// virtual matches interfaces of containers, VMs and VPNs. Their addresses are useless to other devices on the Wi-Fi
// (a phone dialing a docker bridge's 172.17.0.1 just times out), and mDNS hands out every announced address.
var virtual = regexp.MustCompile(`(?i)^(docker|br-|veth|virbr|vmnet|vboxnet|cni|flannel|podman|lxc|lxd|tun|tap|utun|wg|tailscale|zt)|vEthernet|VMware|VirtualBox|Hyper-V`)

// lanAddrs lists the interfaces to announce on (up, multicast, not loopback, not virtual) and their addresses:
// IPv4, and IPv6 that is not link-local (a link-local address needs a zone the other side cannot know).
func lanAddrs() (ifaces []net.Interface, ips []string) {
	all, _ := net.Interfaces()
	for _, i := range all {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagMulticast == 0 || i.Flags&net.FlagLoopback != 0 || virtual.MatchString(i.Name) {
			continue
		}
		addrs, _ := i.Addrs()
		var mine []string
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && (n.IP.To4() != nil || n.IP.IsGlobalUnicast()) {
				mine = append(mine, n.IP.String())
			}
		}
		if len(mine) > 0 {
			ifaces, ips = append(ifaces, i), append(ips, mine...)
		}
	}
	return ifaces, ips
}

func (l *Listener) Close() error {
	l.srv.Shutdown()
	return l.Listener.Close()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// Browse reports the devices on the network, calling onChange with the whole sorted list whenever it changes.
// It runs until ctx ends. The library never reports a device leaving, so the network is asked again every few
// seconds and a device that stays silent for a couple of rounds is dropped.
func Browse(ctx context.Context, selfID string, onChange func([]Peer)) error {
	t := newTracker()
	for first := true; ; first = false {
		seen, err := browseOnce(ctx, selfID)
		if err != nil && first {
			return err
		}
		if t.round(seen) {
			onChange(t.list())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(round - listen):
		}
	}
}

func browseOnce(ctx context.Context, selfID string) (map[string]Peer, error) {
	ctx, cancel := context.WithTimeout(ctx, listen)
	defer cancel()
	entries := make(chan *zeroconf.ServiceEntry)
	seen := map[string]Peer{}
	var wg sync.WaitGroup
	wg.Go(func() {
		for e := range entries {
			if p, ok := peerOf(e); ok && p.ID != selfID {
				seen[p.ID] = p
			}
		}
	})
	err := zeroconf.Browse(ctx, Service, "local.", entries)
	wg.Wait() // Browse closes entries when it returns
	if ctx.Err() != nil {
		err = nil
	}
	return seen, err
}

func peerOf(e *zeroconf.ServiceEntry) (Peer, bool) {
	kv := map[string]string{}
	for _, s := range e.Text {
		if k, v, ok := strings.Cut(s, "="); ok {
			kv[k] = v
		}
	}
	p := Peer{ID: kv["id"], Name: protocol.DisplayName(kv["name"])} // anyone on the network can announce anything
	if p.ID == "" || kv["v"] != "2" || e.Port == 0 {
		return Peer{}, false
	}
	if p.Name == "" {
		p.Name = "device"
	}
	for _, ip := range e.AddrIPv4 {
		p.Addrs = append(p.Addrs, net.JoinHostPort(ip.String(), fmt.Sprint(e.Port)))
	}
	for _, ip := range e.AddrIPv6 {
		if !ip.IsLinkLocalUnicast() { // needs a zone to dial, which the announcement does not carry
			p.Addrs = append(p.Addrs, net.JoinHostPort(ip.String(), fmt.Sprint(e.Port)))
		}
	}
	return p, len(p.Addrs) > 0
}

// tracker turns repeated snapshots into a stable list.
type tracker struct {
	peers  map[string]Peer
	missed map[string]int
}

func newTracker() *tracker { return &tracker{peers: map[string]Peer{}, missed: map[string]int{}} }

// round folds in one snapshot and reports whether the list changed.
func (t *tracker) round(seen map[string]Peer) (changed bool) {
	for id, p := range seen {
		if old, ok := t.peers[id]; !ok || old.Name != p.Name || strings.Join(old.Addrs, ",") != strings.Join(p.Addrs, ",") {
			changed = true
		}
		t.peers[id], t.missed[id] = p, 0
	}
	for id := range t.peers {
		if _, ok := seen[id]; ok {
			continue
		}
		if t.missed[id]++; t.missed[id] >= missLimit {
			delete(t.peers, id)
			delete(t.missed, id)
			changed = true
		}
	}
	return changed
}

func (t *tracker) list() []Peer {
	out := make([]Peer, 0, len(t.peers))
	for _, p := range t.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || (out[i].Name == out[j].Name && out[i].ID < out[j].ID)
	})
	return out
}

// Dial connects to the first reachable address of p.
func Dial(ctx context.Context, p Peer) (net.Conn, error) {
	var last error
	for _, a := range p.Addrs {
		d := net.Dialer{Timeout: 2 * time.Second}
		c, err := d.DialContext(ctx, "tcp", a)
		if err == nil {
			return c, nil
		}
		if last = err; ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("cannot reach %s: %w", p.Name, last)
}
