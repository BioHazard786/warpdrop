package signaling

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// rawClient returns a Client whose pumps are NOT running, so its out channel is never drained.
func rawClient(t *testing.T, h *Hub) *Client {
	t.Helper()
	up := websocket.Upgrader{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := up.Upgrade(w, r, nil)
		time.Sleep(5 * time.Second)
		_ = c.Close()
	}))
	t.Cleanup(ts.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(h, conn, "1.1.1.1")
}

func TestSlowClientIsDroppedWithoutStallingHub(t *testing.T) {
	h := NewHub(nil)
	sender, slow := rawClient(t, h), rawClient(t, h)
	r := &Room{id: "1234", sender: sender, receivers: map[string]*Client{"slow": slow}}
	h.rooms["1234"] = r
	sender.room, sender.peerID = r, "sender"
	slow.room, slow.peerID = r, "slow"

	done := make(chan struct{})
	go func() {
		for i := 0; i < sendBuffer*3; i++ { // far more than the buffer, and nobody reads slow.out
			h.handle(sender, []byte(`{"t":"signal","to":"slow","payload":{"i":1}}`))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hub blocked on a slow client")
	}
	select {
	case <-slow.done:
	default:
		t.Fatal("slow client must be closed")
	}
}

func TestSendAfterCloseDoesNotPanic(t *testing.T) {
	c := rawClient(t, NewHub(nil))
	c.close()
	c.close()
	for i := 0; i < sendBuffer*2; i++ {
		c.send([]byte("x"))
	}
}

func TestIDWidensWhenFourDigitsExhausted(t *testing.T) {
	h := NewHub(nil)
	for i := 0; i < 10000; i++ {
		h.rooms[fmtID(i)] = &Room{}
	}
	if id := h.newID(); len(id) != 6 {
		t.Fatalf("want 6-digit id, got %q", id)
	}
}

func fmtID(i int) string { return fmt.Sprintf("%04d", i) }
