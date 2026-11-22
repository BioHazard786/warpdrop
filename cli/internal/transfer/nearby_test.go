package transfer

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/BioHazard786/Warpdrop/cli/internal/nearby"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
)

// sendNearby runs RunSendNearby, waiting out the server's room-creation rate limit (earlier tests used the burst).
func sendNearby(ctx context.Context, cfg config.Config, srv config.Server, files []protocol.Source, h SendHooks, invite func(string, string) error) chan error {
	done := make(chan error, 1)
	go func() {
		for {
			err := RunSendNearby(ctx, cfg, srv, files, "Laptop", h, invite)
			if err == nil || !strings.Contains(err.Error(), "too many requests") {
				done <- err
				return
			}
			select {
			case <-time.After(6500 * time.Millisecond):
			case <-ctx.Done():
				done <- ctx.Err()
				return
			}
		}
	}()
	return done
}

// A device picked from the list receives a file with no share code, and both screens show the same 6-digit code.
func TestNearbySendReceive(t *testing.T) {
	if serverURL == "" {
		t.Skip("backend binary unavailable")
	}
	cfg := config.Config{Server: serverURL, ForceRelay: true}
	srv, err := cfg.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rcvLobby, err := nearby.Join(ctx, cfg, "Phone", false)
	if err != nil {
		t.Fatal(err)
	}
	defer rcvLobby.Close()
	sndLobby, err := nearby.Join(ctx, cfg, "Laptop", false)
	if err != nil {
		t.Fatal(err)
	}
	defer sndLobby.Close()

	devs := sndLobby.Devices()
	if len(devs) != 1 || devs[0].Name != "Phone" {
		t.Fatalf("sender should see the phone, got %v", devs)
	}

	// receiver: wait for the invite, then answer it
	dir := t.TempDir()
	rcvSAS, rcvDone := make(chan string, 1), make(chan error, 1)
	go func() {
		for e := range rcvLobby.Events {
			if e.Kind != "invite" {
				continue
			}
			sink, err := NewDiskSink(dir)
			if err != nil {
				rcvDone <- err
				return
			}
			defer sink.Close()
			if e.Device.Name != "Laptop" {
				rcvDone <- nil
				return
			}
			rcvDone <- RunReceiveNearby(ctx, cfg, srv, e.RoomID, e.Ticket, "Phone", sink, ReceiveHooks{
				OnSAS:   func(s string) { rcvSAS <- s },
				OnOffer: func(string, []protocol.NamedFile) bool { return true },
			})
			return
		}
	}()

	data := pattern(1<<20+3, 9)
	sndSAS := make(chan string, 4)
	sndDone := sendNearby(ctx, cfg, srv, []protocol.Source{source(0, "n.bin", data)}, SendHooks{
		OnPeer: func(v PeerView) {
			if v.SAS != "" {
				select {
				case sndSAS <- v.SAS:
				default:
				}
			}
		},
	}, func(roomID, token string) error { return sndLobby.Invite(devs[0].ID, roomID, token) })

	select {
	case err := <-rcvDone:
		if err != nil {
			t.Fatal(err)
		}
	case err := <-sndDone:
		t.Fatalf("sender ended first: %v", err)
	case <-ctx.Done():
		t.Fatal("nearby transfer did not finish")
	}
	if !bytes.Equal(readAll(t, dir, "n.bin"), data) {
		t.Fatal("bytes differ")
	}
	if a, b := <-rcvSAS, <-sndSAS; a != b || len(a) != 6 {
		t.Fatalf("codes differ: receiver %q sender %q", a, b)
	}
	cancel()
	<-sndDone
}

// Someone who learns the room id but was not invited cannot join a nearby room.
func TestNearbyRoomRejectsUninvitedJoin(t *testing.T) {
	if serverURL == "" {
		t.Skip("backend binary unavailable")
	}
	cfg := config.Config{Server: serverURL, ForceRelay: true}
	srv, _ := cfg.Fetch(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	room := make(chan string, 1)
	sendNearby(ctx, cfg, srv, []protocol.Source{source(0, "x", pattern(10, 1))}, SendHooks{
		OnRoom: func(id, _ string) { room <- id },
	}, func(string, string) error { return nil })
	var id string
	select {
	case id = <-room:
	case <-ctx.Done():
		t.Fatal("no room")
	}
	sink, _ := NewDiskSink(t.TempDir())
	defer sink.Close()
	err := RunReceive(ctx, cfg, srv, id, "amber-otter-kite-moth", "Mallory", sink, ReceiveHooks{
		OnOffer: func(string, []protocol.NamedFile) bool { return true },
	})
	if err == nil {
		t.Fatal("an uninvited receiver got in")
	}
}
