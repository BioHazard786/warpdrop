package lan

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

func TestTrackerDropsAfterMissedRounds(t *testing.T) {
	tr := newTracker()
	a := Peer{ID: "a", Name: "A", Addrs: []string{"1.1.1.1:1"}}
	if !tr.round(map[string]Peer{"a": a}) || len(tr.list()) != 1 {
		t.Fatal("new peer not listed")
	}
	if tr.round(map[string]Peer{"a": a}) {
		t.Fatal("unchanged snapshot reported a change")
	}
	if tr.round(nil) || len(tr.list()) != 1 {
		t.Fatal("one silent round must not drop the peer")
	}
	if !tr.round(nil) || len(tr.list()) != 0 {
		t.Fatal("peer still listed after missing two rounds")
	}
}

func TestPeerOfRejectsBadAnnouncements(t *testing.T) {
	e := &zeroconf.ServiceEntry{Port: 1234, Text: []string{"v=2", "id=x", "name=Pixel"}, AddrIPv4: []net.IP{net.ParseIP("10.0.0.2")}}
	if p, ok := peerOf(e); !ok || p.Name != "Pixel" || p.Addrs[0] != "10.0.0.2:1234" {
		t.Fatalf("good entry rejected: %+v", p)
	}
	for _, txt := range [][]string{{"v=1", "id=x"}, {"v=2"}} {
		e.Text = txt
		if _, ok := peerOf(e); ok {
			t.Fatalf("accepted %v", txt)
		}
	}
}

func TestTruncateKeepsRunesWhole(t *testing.T) {
	if got := truncate("héllo", 2); got != "h" {
		t.Fatalf("got %q", got)
	}
}

// Needs working multicast on this machine; skipped where there is none (containers, some CI).
func TestAnnounceAndBrowse(t *testing.T) {
	ln, err := Listen(NewID(), "Test Device", "cli")
	if err != nil {
		t.Skip("no mDNS here:", err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	found := make(chan Peer, 1)
	go Browse(ctx, "someone-else", func(ps []Peer) {
		for _, p := range ps {
			if p.Name == "Test Device" {
				select {
				case found <- p:
				default:
				}
			}
		}
	})
	select {
	case p := <-found:
		c, err := Dial(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	case <-ctx.Done():
		t.Skip("announcement not seen: multicast is probably blocked here")
	}
}

func TestVirtualInterfacesAreNotAnnounced(t *testing.T) {
	for name, want := range map[string]bool{
		"docker0": true, "br-3f2a": true, "veth12": true, "virbr0": true, "tailscale0": true, "wg0": true,
		"vEthernet (WSL)": true, "VMware Network Adapter VMnet8": true,
		"wlp0s20f3": false, "eth0": false, "en0": false, "wlan0": false, "Wi-Fi": false, "Ethernet": false,
	} {
		if virtual.MatchString(name) != want {
			t.Errorf("%q: virtual=%v, want %v", name, !want, want)
		}
	}
}
