package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
)

func serve(ctx context.Context, ln net.Listener, sink protocol.Sink, h ReceiveHooks, ended chan<- error) {
	go ServeLAN(ctx, ln, func(c net.Conn) (bool, error) {
		offered, err := ReceiveLAN(ctx, c, "Phone", sink, h)
		ended <- err
		return offered, err
	})
}

func lanSend(ctx context.Context, t *testing.T, addr string, data []byte, h SendHooks) error {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return RunSendLAN(ctx, c, "Phone", []protocol.Source{source(0, "a.bin", data)}, "Laptop", h)
}

// Two devices transfer a file over plain TCP with no server, and both screens show the same code.
func TestLANSendReceive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sink, err := NewDiskSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()

	var rcvSAS string
	ended := make(chan error, 4)
	serve(ctx, ln, sink, ReceiveHooks{
		OnSAS:   func(s string) { rcvSAS = s },
		OnOffer: func(from string, fs []protocol.NamedFile) bool { return from == "Laptop" && len(fs) == 1 },
	}, ended)

	data := pattern(3<<20, 5)
	var sndSAS string
	var last PeerView
	err = lanSend(ctx, t, ln.Addr().String(), data, SendHooks{OnPeer: func(v PeerView) {
		if v.SAS != "" {
			sndSAS = v.SAS
		}
		last = v
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	if last.Status != "done" || last.Transport != "lan" || sndSAS == "" || sndSAS != rcvSAS {
		t.Fatalf("view %+v, codes %q vs %q", last, sndSAS, rcvSAS)
	}
	if !bytes.Equal(readAll(t, dir, "a.bin"), data) {
		t.Fatal("file differs")
	}
}

// A declined offer is reported to the sender, and the same address is then ignored for a while.
func TestLANDeclineThenCooldown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	sink, _ := NewDiskSink(t.TempDir())
	defer sink.Close()
	ended := make(chan error, 4)
	prompts := 0
	serve(ctx, ln, sink, ReceiveHooks{
		OnOffer: func(string, []protocol.NamedFile) bool { prompts++; return false },
	}, ended)

	if err := lanSend(ctx, t, ln.Addr().String(), pattern(1000, 1), SendHooks{}); err == nil || err.Error() != "receiver declined" {
		t.Fatalf("sender should be told it was declined, got %v", err)
	}
	<-ended
	// the second attempt is dropped without a prompt
	if err := lanSend(ctx, t, ln.Addr().String(), pattern(1000, 1), SendHooks{}); err == nil {
		t.Fatal("second attempt should fail while cooling down")
	}
	if prompts != 1 {
		t.Fatalf("receiver was prompted %d times", prompts)
	}
}

// While lanMax devices are being handled, another connection is closed at once and its sender reports ErrBusy.
func TestLANBusyRejectsOthers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	sink, _ := NewDiskSink(t.TempDir())
	defer sink.Close()
	serve(ctx, ln, sink, ReceiveHooks{}, make(chan error, 8))

	for range lanMax { // each sits silent in the handshake
		c, _ := net.Dial("tcp", ln.Addr().String())
		defer c.Close()
	}
	time.Sleep(200 * time.Millisecond)
	if err := lanSend(ctx, t, ln.Addr().String(), pattern(1000, 1), SendHooks{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
}

// Throughput of the direct path on loopback (crypto + framing + disk), for spotting regressions.
func BenchmarkLANTransfer(b *testing.B) {
	data := pattern(64<<20, 3)
	b.SetBytes(int64(len(data)))
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink, _ := NewDiskSink(b.TempDir())
	defer sink.Close()
	ended := make(chan error, 1)
	serve(ctx, ln, sink, ReceiveHooks{OnOffer: func(string, []protocol.NamedFile) bool { return true }}, ended)
	b.ResetTimer()
	for i := range b.N {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			b.Fatal(err)
		}
		name := fmt.Sprintf("b%d.bin", i)
		if err := RunSendLAN(ctx, c, "Phone", []protocol.Source{source(0, name, data)}, "Laptop", SendHooks{}); err != nil {
			b.Fatal(err)
		}
		if err := <-ended; err != nil {
			b.Fatal(err)
		}
	}
}

func isTimeout(err error) bool { ne, ok := err.(net.Error); return ok && ne.Timeout() }
