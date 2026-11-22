package core

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// recorder keeps the latest version of every transfer an engine reported.
type recorder struct {
	mu sync.Mutex
	ts map[string]Transfer
}

func newEngine(t *testing.T, name string) (*Engine, *recorder, string) {
	t.Helper()
	rc := &recorder{ts: map[string]Transfer{}}
	dir := t.TempDir()
	e, err := New(Config{Server: "http://127.0.0.1:1", Dir: dir, Device: name}, func(ev string, data any) {
		if tr, ok := data.(Transfer); ok && ev == "transfer" {
			rc.mu.Lock()
			rc.ts[tr.ID] = tr
			rc.mu.Unlock()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	return e, rc, dir
}

func (r *recorder) waitFor(t *testing.T, what string, ok func(Transfer) bool) Transfer {
	t.Helper()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		r.mu.Lock()
		for _, tr := range r.ts {
			if ok(tr) {
				r.mu.Unlock()
				return tr
			}
		}
		r.mu.Unlock()
	}
	t.Fatalf("timed out waiting for %s; have %+v", what, r.ts)
	return Transfer{}
}

// Two devices on one network, no server: the sender finds the receiver, both show the same code, the file lands.
func TestSendToNearbyDeviceAndAccept(t *testing.T) {
	rcv, rrec, rdir := newEngine(t, "Receiver on Linux")
	snd, srec, _ := newEngine(t, "Sender on Linux")
	if err := rcv.Start(); err != nil {
		t.Fatal(err)
	}
	if err := snd.SetVisible(false); err != nil {
		t.Fatal(err)
	}

	var dev Device
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end) && dev.ID == ""; time.Sleep(100 * time.Millisecond) {
		for _, d := range snd.State().Devices {
			if d.Name == "Receiver on Linux" {
				dev = d
			}
		}
	}
	if dev.ID == "" || !dev.Direct {
		t.Skip("mDNS is not available on this machine")
	}

	data := bytes.Repeat([]byte("desktop!"), 200_000)
	src := filepath.Join(t.TempDir(), "d.bin")
	os.WriteFile(src, data, 0o644)
	if _, err := snd.SendToDevice(dev.ID, []string{t.TempDir()}); err == nil {
		t.Fatal("a folder must be refused")
	}
	if _, err := snd.SendToDevice(dev.ID, []string{src}); err != nil {
		t.Fatal(err)
	}

	offer := rrec.waitFor(t, "an offer", func(x Transfer) bool { return x.Status == "offered" })
	if offer.Peer != "Sender on Linux" || len(offer.Files) != 1 || offer.SAS == "" || offer.Transport != "lan" {
		t.Fatalf("offer: %+v", offer)
	}
	sw := srec.waitFor(t, "sender waiting", func(x Transfer) bool { return x.Status == "waiting" })
	if sw.SAS != offer.SAS {
		t.Fatalf("codes differ: %q vs %q", sw.SAS, offer.SAS)
	}
	rcv.Respond(offer.ID, true)

	srec.waitFor(t, "sender done", func(x Transfer) bool { return x.Status == "done" })
	rrec.waitFor(t, "receiver done", func(x Transfer) bool { return x.Status == "done" && x.Done })
	got, err := os.ReadFile(filepath.Join(rdir, "d.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file not saved: %v", err)
	}
}

func TestDeclineIsReportedToBoth(t *testing.T) {
	rcv, rrec, rdir := newEngine(t, "Receiver on Linux")
	snd, srec, _ := newEngine(t, "Sender on Linux")
	rcv.Start()
	snd.SetVisible(false)
	var dev Device
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end) && dev.ID == ""; time.Sleep(100 * time.Millisecond) {
		for _, d := range snd.State().Devices {
			if d.Name == "Receiver on Linux" {
				dev = d
			}
		}
	}
	if dev.ID == "" {
		t.Skip("mDNS is not available on this machine")
	}
	src := filepath.Join(t.TempDir(), "n.bin")
	os.WriteFile(src, []byte("no thanks"), 0o644)
	snd.SendToDevice(dev.ID, []string{src})
	offer := rrec.waitFor(t, "an offer", func(x Transfer) bool { return x.Status == "offered" })
	rcv.Respond(offer.ID, false)
	rrec.waitFor(t, "declined", func(x Transfer) bool { return x.Status == "declined" })
	srec.waitFor(t, "sender failed", func(x Transfer) bool { return x.Status == "failed" && x.Error != "" })
	if ents, _ := os.ReadDir(rdir); len(ents) != 0 {
		t.Fatalf("declined files left behind: %v", ents)
	}
}
