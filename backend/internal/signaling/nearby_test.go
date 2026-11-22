package signaling

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func at(t *testing.T, h *Hub, ip string) *Client {
	t.Helper()
	c := rawClient(t, h)
	c.ip = ip
	return c
}

// got returns the next message queued for c, or fails.
func got(t *testing.T, c *Client) map[string]any {
	t.Helper()
	select {
	case b := <-c.out:
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	case <-time.After(time.Second):
		t.Fatal("no message")
		return nil
	}
}

func none(t *testing.T, c *Client) {
	t.Helper()
	select {
	case b := <-c.out:
		t.Fatalf("unexpected message %s", b)
	default:
	}
}

func join(t *testing.T, h *Hub, c *Client, device string) string {
	t.Helper()
	c.LobbyOnly = true
	h.handle(c, []byte(fmt.Sprintf(`{"t":"nearby_join","v":2,"device":%q,"client":{"n":"web"}}`, device)))
	m := got(t, c)
	if m["t"] != "nearby_joined" {
		t.Fatalf("got %v", m)
	}
	return m["peer_id"].(string)
}

func TestNetworkKey(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.5":                "lan",
		"10.0.0.2":                   "lan",
		"127.0.0.1":                  "lan",
		"fd00::1":                    "lan",
		"203.0.113.9":                "203.0.113.9",
		"::ffff:203.0.113.9":         "203.0.113.9",
		"2001:db8:1:2:aaaa:bbbb:1:2": "2001:db8:1:2::/64",
		"garbage":                    "garbage",
	} {
		if got := NetworkKey(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestLobbyListsOnlyDevicesOnTheSameNetwork(t *testing.T) {
	h := NewHub(nil)
	a, b, other := at(t, h, "203.0.113.9"), at(t, h, "203.0.113.9"), at(t, h, "198.51.100.7")
	join(t, h, a, "Laptop")
	b.LobbyOnly = true
	h.handle(b, []byte(`{"t":"nearby_join","v":2,"device":"Phone"}`))
	if m := got(t, b); len(m["devices"].([]any)) != 1 {
		t.Fatalf("b should see a: %v", m)
	}
	if m := got(t, a); m["t"] != "nearby_peer" || m["peer"].(map[string]any)["device"] != "Phone" {
		t.Fatalf("a should be told about b: %v", m)
	}
	join(t, h, other, "Stranger")
	none(t, a)
	none(t, b)

	h.leave(b)
	if m := got(t, a); m["t"] != "nearby_peer_left" {
		t.Fatalf("got %v", m)
	}
}

func TestInviteGatesTheRoomBehindASingleUseTicket(t *testing.T) {
	h := NewHub(nil)
	snd, rcv, rcv2, far := at(t, h, "203.0.113.9"), at(t, h, "203.0.113.9"), at(t, h, "203.0.113.9"), at(t, h, "198.51.100.7")
	// the sender's lobby connection and its transfer connection are separate sockets
	lobby := at(t, h, "203.0.113.9")
	join(t, h, lobby, "Laptop")
	join(t, h, rcv, "Phone")
	got(t, lobby) // sees the phone
	farID := join(t, h, far, "Far")

	h.handle(snd, []byte(`{"t":"create_room","v":2,"nearby":true}`))
	created := got(t, snd)
	room, token := created["room_id"].(string), created["token"].(string)

	// an uninvited joiner who guessed the room id gets nothing
	h.handle(rcv2, []byte(fmt.Sprintf(`{"t":"join_room","v":2,"room_id":%q}`, room)))
	if m := got(t, rcv2); m["code"] != ErrNotFound {
		t.Fatalf("uninvited join: %v", m)
	}

	var phoneID string
	for id, c := range h.lobbies["203.0.113.9"] {
		if c == rcv {
			phoneID = id
		}
	}
	// wrong token and a peer on another network are both refused
	h.handle(lobby, []byte(fmt.Sprintf(`{"t":"nearby_invite","to":%q,"room_id":%q,"token":"nope"}`, phoneID, room)))
	if m := got(t, lobby); m["code"] != ErrNotFound {
		t.Fatalf("bad token: %v", m)
	}
	h.handle(lobby, []byte(fmt.Sprintf(`{"t":"nearby_invite","to":%q,"room_id":%q,"token":%q}`, farID, room, token)))
	if m := got(t, lobby); m["code"] != ErrNotFound {
		t.Fatalf("cross-network invite: %v", m)
	}
	none(t, far)

	h.handle(lobby, []byte(fmt.Sprintf(`{"t":"nearby_invite","to":%q,"room_id":%q,"token":%q}`, phoneID, room, token)))
	inv := got(t, rcv)
	if inv["t"] != "nearby_invite" || inv["room_id"] != room || inv["peer"].(map[string]any)["device"] != "Laptop" {
		t.Fatalf("invite: %v", inv)
	}
	ticket := inv["ticket"].(string)

	conn := at(t, h, "203.0.113.9")
	h.handle(conn, []byte(fmt.Sprintf(`{"t":"join_room","v":2,"room_id":%q,"ticket":%q}`, room, ticket)))
	joined := got(t, conn)
	if joined["t"] != "joined" || joined["ticket"] == "" || joined["ticket"] == ticket {
		t.Fatalf("invited join must hand back a fresh rejoin ticket: %v", joined)
	}
	if m := got(t, snd); m["t"] != "peer_joined" || m["invited"] != phoneID {
		t.Fatalf("sender must learn which invite joined: %v", m)
	}
	// the ticket is spent
	again := at(t, h, "203.0.113.9")
	h.handle(again, []byte(fmt.Sprintf(`{"t":"join_room","v":2,"room_id":%q,"ticket":%q}`, room, ticket)))
	if m := got(t, again); m["code"] != ErrNotFound {
		t.Fatalf("reused ticket: %v", m)
	}
}

func TestPlainRoomsStillJoinWithoutATicket(t *testing.T) {
	h := NewHub(nil)
	s, r := at(t, h, "203.0.113.9"), at(t, h, "198.51.100.7")
	h.handle(s, []byte(`{"t":"create_room","v":2}`))
	room := got(t, s)["room_id"].(string)
	h.handle(r, []byte(fmt.Sprintf(`{"t":"join_room","v":2,"room_id":%q}`, room)))
	if m := got(t, r); m["t"] != "joined" {
		t.Fatalf("share-code flow must be unchanged: %v", m)
	}
}

func TestHiddenPeerSeesOthersButIsNotListedOrInvitable(t *testing.T) {
	h := NewHub(nil)
	shown, hid := at(t, h, "203.0.113.9"), at(t, h, "203.0.113.9")
	join(t, h, shown, "Laptop")
	hid.LobbyOnly = true
	h.handle(hid, []byte(`{"t":"nearby_join","v":2,"device":"Ghost","hidden":true}`))
	if m := got(t, hid); len(m["devices"].([]any)) != 1 {
		t.Fatalf("a hidden peer still sees the list: %v", m)
	}
	none(t, shown) // nobody was told about the hidden peer
	late := at(t, h, "203.0.113.9")
	late.LobbyOnly = true
	h.handle(late, []byte(`{"t":"nearby_join","v":2,"device":"Late"}`))
	if m := got(t, late); len(m["devices"].([]any)) != 1 {
		t.Fatalf("a hidden peer must not be listed: %v", m)
	}
	got(t, shown) // told about Late
	got(t, hid)   // hidden peers still get updates

	// and it cannot be invited, even by someone who learned its id
	snd := at(t, h, "203.0.113.9")
	h.handle(snd, []byte(`{"t":"create_room","v":2,"nearby":true}`))
	created := got(t, snd)
	hiddenID := hid.lobby.id
	h.handle(shown, []byte(fmt.Sprintf(`{"t":"nearby_invite","to":%q,"room_id":%q,"token":%q}`, hiddenID, created["room_id"], created["token"])))
	if m := got(t, shown); m["code"] != ErrNotFound {
		t.Fatalf("invite to a hidden peer: %v", m)
	}
}

func TestLobbySocketsAndTransferSocketsCannotDoEachOthersJobs(t *testing.T) {
	h := NewHub(nil)
	lobby, transfer := at(t, h, "203.0.113.9"), at(t, h, "203.0.113.9")
	lobby.LobbyOnly = true
	// a lobby socket is counted in its own pool, so it must not be able to run a transfer
	h.handle(lobby, []byte(`{"t":"create_room","v":2}`))
	if m := got(t, lobby); m["code"] != ErrBadRequest {
		t.Fatalf("lobby socket created a room: %v", m)
	}
	// and a transfer socket must not sit in the lobby
	h.handle(transfer, []byte(`{"t":"nearby_join","v":2,"device":"x"}`))
	if m := got(t, transfer); m["code"] != ErrBadRequest {
		t.Fatalf("transfer socket joined the lobby: %v", m)
	}
}

// Declining an invite spends the ticket and tells the sender which device said no.
func TestDeclineTellsTheSender(t *testing.T) {
	h := NewHub(nil)
	snd, rcv, lobby := at(t, h, "203.0.113.9"), at(t, h, "203.0.113.9"), at(t, h, "203.0.113.9")
	join(t, h, lobby, "Laptop")
	phoneID := join(t, h, rcv, "Phone")
	got(t, lobby)
	h.handle(snd, []byte(`{"t":"create_room","v":2,"nearby":true}`))
	created := got(t, snd)
	room, token := created["room_id"].(string), created["token"].(string)
	h.handle(lobby, []byte(fmt.Sprintf(`{"t":"nearby_invite","to":%q,"room_id":%q,"token":%q}`, phoneID, room, token)))
	ticket := got(t, rcv)["ticket"].(string)

	// a wrong ticket does nothing
	h.handle(rcv, []byte(fmt.Sprintf(`{"t":"nearby_decline","room_id":%q,"ticket":"nope"}`, room)))
	none(t, snd)

	h.handle(rcv, []byte(fmt.Sprintf(`{"t":"nearby_decline","room_id":%q,"ticket":%q,"reason":"busy"}`, room, ticket)))
	if m := got(t, snd); m["t"] != "invite_declined" || m["peer_id"] != phoneID || m["reason"] != "busy" || m["device"] != "Phone" {
		t.Fatalf("sender: %v", m)
	}
	conn := at(t, h, "203.0.113.9")
	h.handle(conn, []byte(fmt.Sprintf(`{"t":"join_room","v":2,"room_id":%q,"ticket":%q}`, room, ticket)))
	if m := got(t, conn); m["code"] != ErrNotFound {
		t.Fatalf("declined ticket must be spent: %v", m)
	}
}
