package relay

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newServer(t *testing.T, r *Relay) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/relay/{ticket}", r.Handler)
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func dial(t *testing.T, s *httptest.Server, ticket string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/relay/"+ticket, nil)
}

func TestPairsAndPipesBothWays(t *testing.T) {
	r := New(0, 10, websocket.Upgrader{})
	s := newServer(t, r)
	tk := r.Issue()
	a, _, err := dial(t, s, tk)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := dial(t, s, tk)
	if err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte{7}, 300<<10)
	if err := a.WriteMessage(websocket.BinaryMessage, big); err != nil {
		t.Fatal(err)
	}
	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, got, err := b.ReadMessage()
	if err != nil || mt != websocket.BinaryMessage || !bytes.Equal(got, big) {
		t.Fatalf("a→b failed: %v len=%d", err, len(got))
	}
	if err := b.WriteMessage(websocket.TextMessage, []byte("pong")); err != nil {
		t.Fatal(err)
	}
	_ = a.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, got, err = a.ReadMessage(); err != nil || string(got) != "pong" {
		t.Fatalf("b→a failed: %v %q", err, got)
	}
}

func TestTicketSingleUseAndUnknown(t *testing.T) {
	r := New(0, 10, websocket.Upgrader{})
	s := newServer(t, r)
	if _, resp, err := dial(t, s, "nope"); err == nil || resp.StatusCode != 404 {
		t.Fatal("unknown ticket must 404")
	}
	tk := r.Issue()
	a, _, _ := dial(t, s, tk)
	b, _, _ := dial(t, s, tk)
	defer a.Close()
	defer b.Close()
	if _, resp, err := dial(t, s, tk); err == nil || resp.StatusCode != 404 {
		t.Fatal("third socket on a used ticket must 404")
	}
}

func TestClosingOneSideClosesOther(t *testing.T) {
	r := New(0, 10, websocket.Upgrader{})
	s := newServer(t, r)
	tk := r.Issue()
	a, _, _ := dial(t, s, tk)
	b, _, _ := dial(t, s, tk)
	_ = a.Close()
	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := b.ReadMessage(); err == nil {
		t.Fatal("peer must observe close")
	}
}

func TestIdleTimeoutClosesPair(t *testing.T) {
	r := New(0, 10, websocket.Upgrader{})
	r.IdleAfter = 100 * time.Millisecond
	s := newServer(t, r)
	tk := r.Issue()
	a, _, _ := dial(t, s, tk)
	b, _, _ := dial(t, s, tk)
	defer a.Close()
	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := b.ReadMessage(); err == nil {
		t.Fatal("idle pair must be closed")
	}
}

func TestBusyWhenAtCap(t *testing.T) {
	r := New(0, 0, websocket.Upgrader{})
	s := newServer(t, r)
	if _, resp, err := dial(t, s, r.Issue()); err == nil || resp.StatusCode != 503 {
		t.Fatal("must refuse at cap")
	}
}
