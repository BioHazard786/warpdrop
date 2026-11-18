package protocol

import (
	"encoding/hex"
	"testing"

	"github.com/cespare/xxhash/v2"
)

type vectors struct {
	X25519                                       struct{ SenderSeed, ReceiverSeed, SenderPub, ReceiverPub, Shared string }
	RoomID, Secret, RootKey, AuthKey, SessionKey string
	SenderFingerprint, ReceiverFingerprint       string
	TranscriptRtc, TranscriptRelay               string
	Macs                                         struct{ Rtc, Relay struct{ Recv, Send string } }
	RelayFrames                                  []struct {
		Dir     uint32
		Counter int
		Kind    byte
		Payload string
		Frame   string
	}
	Xxhash64 []struct{ Input, Xxhash64 string }
}

func hx(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCryptoVectorsMatchNodeAndWebCrypto(t *testing.T) {
	var v vectors
	fixture(t, "crypto-vectors", &v)
	s, _ := EphemeralFromSeed(hx(t, v.X25519.SenderSeed))
	r, _ := EphemeralFromSeed(hx(t, v.X25519.ReceiverSeed))
	if hex.EncodeToString(s.Public()) != v.X25519.SenderPub || hex.EncodeToString(r.Public()) != v.X25519.ReceiverPub {
		t.Fatal("public keys differ")
	}
	ks, err := DeriveKeys(v.Secret, v.RoomID, s, r.Public())
	if err != nil {
		t.Fatal(err)
	}
	kr, _ := DeriveKeys(v.Secret, v.RoomID, r, s.Public())
	for _, k := range []Keys{ks, kr} {
		if hex.EncodeToString(k.Root) != v.RootKey || hex.EncodeToString(k.Auth) != v.AuthKey || hex.EncodeToString(k.Session) != v.SessionKey {
			t.Fatal("derived keys differ from vectors")
		}
	}
	rtc := Transcript(s.Public(), r.Public(), hx(t, v.SenderFingerprint), hx(t, v.ReceiverFingerprint))
	relay := Transcript(s.Public(), r.Public(), nil, nil)
	if hex.EncodeToString(rtc) != v.TranscriptRtc || hex.EncodeToString(relay) != v.TranscriptRelay {
		t.Fatal("transcripts differ")
	}
	if MAC(ks.Auth, "recv", rtc) != v.Macs.Rtc.Recv || MAC(ks.Auth, "send", rtc) != v.Macs.Rtc.Send || MAC(ks.Auth, "recv", relay) != v.Macs.Relay.Recv {
		t.Fatal("MACs differ")
	}
	if !VerifyMAC(ks.Auth, "recv", rtc, v.Macs.Rtc.Recv) || VerifyMAC(ks.Auth, "send", rtc, v.Macs.Rtc.Recv) || VerifyMAC(ks.Auth, "recv", relay, v.Macs.Rtc.Recv) || VerifyMAC(ks.Auth, "recv", rtc, "zz") {
		t.Fatal("MAC must bind label and transcript and reject junk")
	}
	// relay frames: byte-for-byte and round-trip
	seal := map[uint32]*FrameCipher{}
	open := map[uint32]*FrameCipher{}
	for _, d := range []uint32{1, 2} {
		seal[d], _ = NewFrameCipher(ks.Session, d)
		open[d], _ = NewFrameCipher(ks.Session, d)
	}
	for _, f := range v.RelayFrames {
		wire := seal[f.Dir].Seal(f.Kind, hx(t, f.Payload))
		if hex.EncodeToString(wire) != f.Frame {
			t.Fatalf("relay frame dir=%d ctr=%d differs", f.Dir, f.Counter)
		}
		kind, pl, err := open[f.Dir].Open(wire)
		if err != nil || kind != f.Kind || hex.EncodeToString(pl) != f.Payload {
			t.Fatalf("open failed: %v", err)
		}
	}
	for _, c := range v.Xxhash64 {
		h := xxhash.New()
		h.Write([]byte(c.Input))
		if got := hexU64(h.Sum64()); got != c.Xxhash64 {
			t.Errorf("xxhash64(%.8q) = %s want %s", c.Input, got, c.Xxhash64)
		}
	}
}

func TestRelayCipherRejectsTamperReplayReorderWrongDirection(t *testing.T) {
	key := make([]byte, 32)
	tx, _ := NewFrameCipher(key, 1)
	a := tx.Seal(KindText, []byte{1})
	b := tx.Seal(KindText, []byte{2})
	bad := append([]byte(nil), a...)
	bad[0] ^= 1
	if _, _, err := mustCipher(key, 1).Open(bad); err == nil {
		t.Error("tampered frame accepted")
	}
	if _, _, err := mustCipher(key, 1).Open(b); err == nil {
		t.Error("counter gap accepted")
	}
	rx := mustCipher(key, 1)
	if _, _, err := rx.Open(a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rx.Open(a); err == nil {
		t.Error("replay accepted")
	}
	if _, _, err := mustCipher(key, 2).Open(a); err == nil {
		t.Error("wrong direction accepted")
	}
}

func mustCipher(key []byte, dir uint32) *FrameCipher {
	c, err := NewFrameCipher(key, dir)
	if err != nil {
		panic(err)
	}
	return c
}

func TestDTLSFingerprint(t *testing.T) {
	sdp := "v=0\r\na=fingerprint:sha-256 AB:CD:EF:01\r\na=other\r\n"
	if got := hex.EncodeToString(DTLSFingerprint(sdp)); got != "abcdef01" {
		t.Fatal(got)
	}
	if DTLSFingerprint("v=0") != nil {
		t.Fatal("must be nil without a fingerprint")
	}
}
