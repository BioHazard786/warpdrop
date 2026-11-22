package protocol

import (
	"context"
	"net"
	"testing"
	"time"
)

// The same sender and receiver code runs over a real TCP stream, with the frames sealed after the handshake.
func TestLANTransportCarriesAFullTransfer(t *testing.T) {
	a, b := net.Pipe()
	data := pattern(2_000_000, 3)
	sSAS, rSAS, sRes, rRes := nearbyRun(t, NewLANTransport(a), NewLANTransport(b), []Source{src(0, "x.bin", data)})
	expect(t, sRes, StatusDone, "", false)
	expect(t, rRes, StatusDone, "", false)
	if sSAS == "" || sSAS != rSAS {
		t.Fatalf("codes %q %q", sSAS, rSAS)
	}
}

func TestLANTransportRejectsBadFrames(t *testing.T) {
	for name, hdr := range map[string][]byte{
		"too large":    {1, 0xff, 0xff, 0xff, 0xff},
		"unknown type": {9, 0, 0, 0, 1, 'x'},
	} {
		a, b := net.Pipe()
		tr := NewLANTransport(b)
		go func() { a.Write(hdr) }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if _, err := tr.Recv(ctx); err == nil {
			t.Fatalf("%s: frame accepted", name)
		}
		cancel()
		a.Close()
	}
}

func TestLANTransportRefusesBinaryBeforeCrypto(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	tr := NewLANTransport(a)
	defer tr.Close()
	if err := tr.SendBinary([]byte("x")); err == nil {
		t.Fatal("binary frame went out unencrypted")
	}
}
