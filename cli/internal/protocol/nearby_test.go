package protocol

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

var sasRe = regexp.MustCompile(`^\d{6}$`)

// nearbyRun runs a nearby sender and receiver over the given transports and returns their codes and results.
func nearbyRun(t *testing.T, st, rt Transport, files []Source) (sSAS, rSAS string, sRes, rRes Result) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	sCh, rCh := make(chan Result, 1), make(chan Result, 1)
	state := &ReceiveState{}
	sink := newSink()
	go func() {
		sCh <- RunSender(ctx, st, SenderOptions{RoomID: "lobby", Device: "s", Files: files, OnSAS: func(s string) { sSAS = s }})
	}()
	go func() {
		rCh <- RunReceiver(ctx, rt, ReceiverOptions{RoomID: "lobby", Device: "r", Sink: sink, State: state,
			OnOffer: func([]NamedFile) bool { return true }})
	}()
	sRes, rRes = wait(t, sCh), wait(t, rCh)
	return sSAS, state.SAS, sRes, rRes
}

func TestNearbyHandshakeTransfersAndBothShowTheSameCode(t *testing.T) {
	st, rt := NewMemPair(true)
	data := pattern(300_000, 7)
	sSAS, rSAS, sRes, rRes := nearbyRun(t, st, rt, []Source{src(0, "x.bin", data)})
	expect(t, sRes, StatusDone, "", false)
	expect(t, rRes, StatusDone, "", false)
	if !sasRe.MatchString(sSAS) || sSAS != rSAS {
		t.Fatalf("codes differ or malformed: sender %q receiver %q", sSAS, rSAS)
	}
}

func TestNearbyCodeDiffersBetweenSessions(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		st, rt := NewMemPair(true)
		s, _, _, _ := nearbyRun(t, st, rt, []Source{src(0, "x", pattern(10, i))})
		seen[s] = true
	}
	if len(seen) < 2 {
		t.Fatalf("code does not depend on the fresh keys: %v", seen)
	}
}

// A man-in-the-middle can complete both handshakes (it is a genuine peer to each side) but the two codes differ.
func TestNearbyMITMGetsDifferentCodes(t *testing.T) {
	s1, m1 := NewMemPair(true) // sender <-> mallory
	m2, r2 := NewMemPair(true) // mallory <-> receiver
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sasS, sasR := make(chan string, 1), make(chan string, 1)
	state := &ReceiveState{}
	go RunSender(ctx, s1, SenderOptions{RoomID: "lobby", Device: "s", Files: []Source{src(0, "x", pattern(10, 1))},
		OnSAS: func(s string) { sasS <- s }})
	go RunReceiver(ctx, r2, ReceiverOptions{RoomID: "lobby", Device: "r", Sink: newSink(), State: state,
		OnOffer: func([]NamedFile) bool { return true }})
	go handshake(ctx, RoleReceiver, m1, "lobby", "", "mallory")
	go func() {
		_, sas, _ := handshake(ctx, RoleSender, m2, "lobby", "", "mallory")
		sasR <- sas
	}()
	var a, b string
	for a == "" || b == "" {
		select {
		case a = <-sasS:
		case b = <-sasR:
		case <-ctx.Done():
			t.Fatal("handshakes did not finish")
		}
	}
	if a == b {
		t.Fatalf("MITM sees matching codes %s: the check proves nothing", a) // 1-in-a-million flake is acceptable
	}
}

func TestNearbyRevealThatDoesNotMatchCommitIsRejected(t *testing.T) {
	st, rt := NewMemPair(false)
	other, _ := NewEphemeral()
	st.Tamper = func(w Wire) (Wire, bool) {
		if w.IsText && strings.Contains(w.Text, `"t":"reveal"`) {
			return Wire{Text: Encode(NewReveal(fmt.Sprintf("%x", other.Public()))), IsText: true}, true
		}
		return w, true
	}
	_, _, _, rRes := nearbyRun(t, st, rt, []Source{src(0, "x", pattern(10, 2))})
	expect(t, rRes, StatusFailed, ReasonProtocolError, false)
}

// A sender that skips the commit (plain hello) must not be accepted by a code-less receiver.
func TestNearbyReceiverRefusesUncommittedHello(t *testing.T) {
	st, rt := NewMemPair(false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go RunSender(ctx, st, SenderOptions{RoomID: "lobby", Secret: testSecret, Device: "s", Files: []Source{src(0, "x", pattern(10, 3))}})
	res := RunReceiver(ctx, rt, ReceiverOptions{RoomID: "lobby", Device: "r", Sink: newSink(), State: &ReceiveState{},
		OnOffer: func([]NamedFile) bool { return true }})
	expect(t, res, StatusFailed, ReasonProtocolError, false)
}

func TestParseHelloCommitRules(t *testing.T) {
	c := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, frame string
		ok          bool
	}{
		{"commit only", `{"t":"hello","v":2,"client":"c","device":"d","pub":"","commit":"` + c + `"}`, true},
		{"pub only", `{"t":"hello","v":2,"client":"c","device":"d","pub":"` + c + `"}`, true},
		{"both", `{"t":"hello","v":2,"client":"c","device":"d","pub":"` + c + `","commit":"` + c + `"}`, false},
		{"neither", `{"t":"hello","v":2,"client":"c","device":"d","pub":""}`, false},
		{"bad reveal", `{"t":"reveal","pub":"zz"}`, false},
		{"reveal", `{"t":"reveal","pub":"` + c + `"}`, true},
	} {
		if _, err := ParseFrame(tc.frame); (err == nil) != tc.ok {
			t.Errorf("%s: err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}
