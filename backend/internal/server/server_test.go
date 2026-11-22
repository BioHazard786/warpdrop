package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/BioHazard786/Warpdrop/backend/internal/config"
	"github.com/BioHazard786/Warpdrop/backend/internal/ratelimit"
)

type env struct {
	srv *Server
	ts  *httptest.Server
}

func setup(t *testing.T, mod func(*config.Config)) env {
	t.Helper()
	cfg := config.Config{RelayEnabled: true, RelayMaxConns: 10, STUNURLs: []string{"stun:s"}}
	if mod != nil {
		mod(&cfg)
	}
	spa := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("js")},
	}
	s, h := New(cfg, spa)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return env{s, ts}
}

type conn struct {
	t *testing.T
	c *websocket.Conn
}

func (e env) dial(t *testing.T) *conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(e.ts.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &conn{t, c}
}

func (c *conn) send(v map[string]any) {
	c.t.Helper()
	if err := c.c.WriteJSON(v); err != nil {
		c.t.Fatal(err)
	}
}

func (c *conn) recv() map[string]any {
	c.t.Helper()
	_ = c.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var m map[string]any
	if err := c.c.ReadJSON(&m); err != nil {
		c.t.Fatalf("recv: %v", err)
	}
	return m
}

func (c *conn) want(typ string) map[string]any {
	c.t.Helper()
	m := c.recv()
	if m["t"] != typ {
		c.t.Fatalf("want %s, got %v", typ, m)
	}
	return m
}

func (c *conn) wantErr(code string) {
	c.t.Helper()
	if m := c.want("error"); m["code"] != code {
		c.t.Fatalf("want error %s, got %v", code, m)
	}
}

func create(t *testing.T, e env) (*conn, string, string) {
	s := e.dial(t)
	s.send(map[string]any{"t": "create_room", "v": 2, "client": map[string]any{"name": "s"}})
	m := s.want("room_created")
	return s, m["room_id"].(string), m["token"].(string)
}

func join(t *testing.T, e env, room string) (*conn, string) {
	r := e.dial(t)
	r.send(map[string]any{"t": "join_room", "v": 2, "room_id": room, "client": map[string]any{}})
	return r, r.want("joined")["peer_id"].(string)
}

func TestSignalRoutingMultipleReceivers(t *testing.T) {
	e := setup(t, nil)
	s, room, _ := create(t, e)
	if len(room) != 4 {
		t.Fatalf("want 4-digit room, got %q", room)
	}
	r1, id1 := join(t, e, room)
	r2, id2 := join(t, e, room)
	if got := s.want("peer_joined")["peer_id"]; got != id1 {
		t.Fatalf("peer_joined %v != %v", got, id1)
	}
	s.want("peer_joined")

	s.send(map[string]any{"t": "signal", "to": id2, "payload": map[string]any{"sdp": "x"}})
	m := r2.want("signal")
	if m["from"] != "sender" {
		t.Fatalf("from=%v", m["from"])
	}
	r1.send(map[string]any{"t": "signal", "to": "ignored", "payload": map[string]any{"sdp": "y"}})
	if m := s.want("signal"); m["from"] != id1 {
		t.Fatalf("from=%v want %s", m["from"], id1)
	}
	// receiver cannot reach another receiver: `to` is ignored and it always reaches the sender
	r1.send(map[string]any{"t": "signal", "to": id2, "payload": map[string]any{"sdp": "z"}})
	if m := s.want("signal"); m["from"] != id1 {
		t.Fatal("receiver signal must go to sender only")
	}
}

func TestDoubleCreateRejected(t *testing.T) {
	e := setup(t, nil)
	s, _, _ := create(t, e)
	s.send(map[string]any{"t": "create_room", "v": 2})
	s.wantErr("bad_request")
	if e.srv.Hub().Rooms() != 1 {
		t.Fatal("rejected create must not leave a second room")
	}
}

func TestSenderDisconnectAfterDoubleCreateDoesNotKillHub(t *testing.T) {
	e := setup(t, nil)
	s, room, _ := create(t, e)
	s.send(map[string]any{"t": "create_room", "v": 2})
	s.wantErr("bad_request")
	_ = s.c.Close()
	time.Sleep(50 * time.Millisecond)
	// the old bug panicked the hub here
	r := e.dial(t)
	r.send(map[string]any{"t": "join_room", "v": 2, "room_id": room})
	r.want("error") // not_found (sender gone) or joined: either way the server must answer
}

func TestVersionAndUnknown(t *testing.T) {
	e := setup(t, nil)
	c := e.dial(t)
	c.send(map[string]any{"t": "create_room", "v": 1})
	c.wantErr("unsupported_version")
	c.send(map[string]any{"t": "nope"})
	c.wantErr("bad_request")
	c.send(map[string]any{"t": "join_room", "v": 2, "room_id": "0000"})
	c.wantErr("not_found")
}

func TestRoomFull(t *testing.T) {
	e := setup(t, nil)
	e.srv.Hub().Join = ratelimit.NewIP(6000, 100) // so the cap, not the rate limiter, answers
	_, room, _ := create(t, e)
	for i := 0; i < 10; i++ {
		join(t, e, room)
	}
	r := e.dial(t)
	r.send(map[string]any{"t": "join_room", "v": 2, "room_id": room, "client": map[string]any{}})
	r.wantErr("room_full")
}

func TestJoinRateLimited(t *testing.T) {
	e := setup(t, nil)
	c := e.dial(t)
	limited := false
	for i := 0; i < 15 && !limited; i++ {
		c.send(map[string]any{"t": "join_room", "v": 2, "room_id": "9999"})
		limited = c.want("error")["code"] == "rate_limited"
	}
	if !limited {
		t.Fatal("join guessing must be rate limited")
	}
}

func TestResumeWithinGraceAndExpiry(t *testing.T) {
	e := setup(t, nil)
	e.srv.Hub().Grace = 300 * time.Millisecond
	s, room, token := create(t, e)
	r, id := join(t, e, room)
	s.want("peer_joined")
	_ = s.c.Close()
	time.Sleep(50 * time.Millisecond)

	s2 := e.dial(t)
	s2.send(map[string]any{"t": "resume_room", "room_id": room, "token": "wrong"})
	s2.wantErr("not_found")
	s2.send(map[string]any{"t": "resume_room", "room_id": room, "token": token})
	m := s2.want("room_resumed")
	if p := m["peers"].([]any); len(p) != 1 || p[0] != id {
		t.Fatalf("peers=%v", p)
	}
	// still routes after resume
	r.send(map[string]any{"t": "signal", "payload": map[string]any{"a": 1}})
	s2.want("signal")

	// sender vanishes for good: receivers learn the room is closed after the grace period
	_ = s2.c.Close()
	r.want("room_closed")
	if e.srv.Hub().Rooms() != 0 {
		t.Fatal("room must be deleted")
	}
}

func TestReceiverLeaves(t *testing.T) {
	e := setup(t, nil)
	s, room, _ := create(t, e)
	r, id := join(t, e, room)
	s.want("peer_joined")
	_ = r.c.Close()
	if got := s.want("peer_left")["peer_id"]; got != id {
		t.Fatalf("peer_left %v", got)
	}
}

func TestCloseRoom(t *testing.T) {
	e := setup(t, nil)
	s, room, _ := create(t, e)
	r, _ := join(t, e, room)
	s.want("peer_joined")
	s.send(map[string]any{"t": "close_room"})
	r.want("room_closed")
}

func TestRelayTicketAndPairing(t *testing.T) {
	e := setup(t, nil)
	s, room, _ := create(t, e)
	r, id := join(t, e, room)
	s.want("peer_joined")
	s.send(map[string]any{"t": "relay_request", "to": id})
	ts := s.want("relay_ticket")
	tr := r.want("relay_ticket")
	if ts["ticket"] != tr["ticket"] || ts["peer_id"] != id || tr["peer_id"] != "sender" {
		t.Fatalf("tickets %v %v", ts, tr)
	}
	base := "ws" + strings.TrimPrefix(e.ts.URL, "http") + "/relay/" + ts["ticket"].(string)
	a, _, err := websocket.DefaultDialer.Dial(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := websocket.DefaultDialer.Dial(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()
	_ = a.WriteMessage(websocket.BinaryMessage, []byte("ciphertext"))
	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, got, err := b.ReadMessage(); err != nil || string(got) != "ciphertext" {
		t.Fatalf("%v %q", err, got)
	}
}

func TestRelayDisabled(t *testing.T) {
	e := setup(t, func(c *config.Config) { c.RelayEnabled = false })
	s, room, _ := create(t, e)
	_, id := join(t, e, room)
	s.want("peer_joined")
	s.send(map[string]any{"t": "relay_request", "to": id})
	s.wantErr("relay_disabled")
	resp, err := http.Get(e.ts.URL + "/relay/abc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("relay path when disabled: %d", resp.StatusCode)
	}
}

func TestOriginCheck(t *testing.T) {
	e := setup(t, func(c *config.Config) { c.AllowedOrigins = []string{"https://ok.example"} })
	url := "ws" + strings.TrimPrefix(e.ts.URL, "http") + "/ws"
	dial := func(origin string) error {
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}
		c, _, err := websocket.DefaultDialer.Dial(url, h)
		if err == nil {
			_ = c.Close()
		}
		return err
	}
	if err := dial(""); err != nil {
		t.Fatalf("empty origin (CLI) must pass: %v", err)
	}
	if err := dial(e.ts.URL); err != nil {
		t.Fatalf("same host must pass: %v", err)
	}
	if err := dial("https://ok.example"); err != nil {
		t.Fatalf("allow-listed must pass: %v", err)
	}
	if err := dial("https://evil.example"); err == nil {
		t.Fatal("foreign origin must be rejected")
	}
}

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	req := func(remote, xff string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	cases := []struct {
		name, remote, xff, want string
		trusted                 []netip.Prefix
	}{
		{"no trusted proxies ignores XFF", "1.2.3.4:5", "9.9.9.9", "1.2.3.4", nil},
		{"untrusted peer ignores XFF", "1.2.3.4:5", "9.9.9.9", "1.2.3.4", trusted},
		{"trusted peer uses XFF", "10.0.0.1:5", "9.9.9.9", "9.9.9.9", trusted},
		{"spoofed left hops ignored", "10.0.0.1:5", "6.6.6.6, 9.9.9.9", "9.9.9.9", trusted},
		{"chain of trusted proxies", "10.0.0.1:5", "9.9.9.9, 10.0.0.2", "9.9.9.9", trusted},
		{"trusted peer without header", "10.0.0.1:5", "", "10.0.0.1", trusted},
	}
	for _, c := range cases {
		if got := ClientIP(req(c.remote, c.xff), c.trusted); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestSPAAndConfig(t *testing.T) {
	e := setup(t, nil)
	get := func(p string) (*http.Response, string) {
		resp, err := http.Get(e.ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}
	if r, b := get("/r/4821"); r.StatusCode != 200 || !strings.Contains(b, "app") || r.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("client route must serve index with CSP: %d %q", r.StatusCode, b)
	}
	if r, _ := get("/assets/app.js"); r.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache: %q", r.Header.Get("Cache-Control"))
	}
	for _, p := range []string{"/assets/missing.js", "/api/nope", "/favicon.ico"} {
		if r, _ := get(p); r.StatusCode != 404 {
			t.Errorf("%s: %d want 404", p, r.StatusCode)
		}
	}
	if r, b := get("/healthz"); r.StatusCode != 200 || b != "ok" {
		t.Fatal("healthz")
	}
	r, b := get("/api/config")
	var cfg struct {
		V          int
		Relay      bool
		ICEServers []struct{ URLs []string } `json:"iceServers"`
	}
	if err := json.Unmarshal([]byte(b), &cfg); err != nil || cfg.V != 2 || !cfg.Relay || len(cfg.ICEServers) != 1 || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("config: %v %+v", err, cfg)
	}
}

func TestRateLimitScale(t *testing.T) {
	create := func(e env) (ok int) {
		for i := 0; i < 30; i++ {
			c := e.dial(t)
			c.send(map[string]any{"t": "create_room", "v": 2, "client": map[string]any{}})
			if c.recv()["t"] == "room_created" {
				ok++
			}
			_ = c.c.Close() // the per-IP concurrent-connection cap (20) is a separate limit; do not trip it here
			time.Sleep(5 * time.Millisecond)
		}
		return ok
	}
	base := create(setup(t, nil))
	if base != 5 {
		t.Fatalf("default burst should allow exactly 5 rooms, got %d", base)
	}
	scaled := create(setup(t, func(c *config.Config) { c.RateScale = 10 }))
	if scaled != 30 {
		t.Fatalf("RATE_LIMIT_SCALE=10 should lift the limit (burst 50), got %d of 30", scaled)
	}
}

func TestAssetLinks(t *testing.T) {
	off := setup(t, nil)
	resp, err := http.Get(off.ts.URL + "/.well-known/assetlinks.json")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("without a configured certificate the file must 404, got %d", resp.StatusCode)
	}

	on := setup(t, func(c *config.Config) {
		c.AndroidPackage = "info.warplabs.warpdrop"
		c.AndroidCertSHA256 = []string{"AA:BB"}
	})
	resp, err = http.Get(on.ts.URL + "/.well-known/assetlinks.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []struct {
		Relation []string `json:"relation"`
		Target   struct {
			Namespace string   `json:"namespace"`
			Package   string   `json:"package_name"`
			Certs     []string `json:"sha256_cert_fingerprints"`
		} `json:"target"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || len(got) != 1 {
		t.Fatalf("bad body: %v %v", got, err)
	}
	if got[0].Target.Package != "info.warplabs.warpdrop" || got[0].Target.Certs[0] != "AA:BB" || got[0].Target.Namespace != "android_app" {
		t.Fatalf("unexpected target %+v", got[0].Target)
	}
}

// Idle lobby sockets (one per open tab) must not use up the cap that transfers need.
func TestLobbyConnectionsHaveTheirOwnPool(t *testing.T) {
	e := setup(t, func(c *config.Config) { c.RateScale = 100 })
	for i := 0; i < maxConnsPerIP; i++ {
		e.dial(t) // fill the transfer pool
	}
	if _, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(e.ts.URL, "http")+"/ws", nil); err == nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("the transfer pool should be full (err=%v)", err)
	}
	lobby, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(e.ts.URL, "http")+"/ws?lobby=1", nil)
	if err != nil {
		t.Fatalf("a lobby socket must still connect while the transfer pool is full: %v", err)
	}
	defer lobby.Close()
	c := &conn{t, lobby}
	c.send(map[string]any{"t": "nearby_join", "v": 2, "device": "Laptop"})
	c.want("nearby_joined")
	c.send(map[string]any{"t": "create_room", "v": 2}) // and it cannot be used to dodge the transfer cap
	c.wantErr("bad_request")
}
