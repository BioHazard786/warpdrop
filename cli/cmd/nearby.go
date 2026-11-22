package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/BioHazard786/Warpdrop/cli/internal/lan"
	"github.com/BioHazard786/Warpdrop/cli/internal/nearby"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
	"github.com/BioHazard786/Warpdrop/cli/internal/transfer"
	"github.com/BioHazard786/Warpdrop/cli/internal/ui"
	"golang.org/x/term"
)

// joinNearby makes this terminal visible to other devices on the same network.
func joinNearby(ctx context.Context, cfg config.Config, hidden bool) (*nearby.Lobby, error) {
	return nearby.Join(ctx, cfg, deviceName(), hidden)
}

// fmtSAS shows the 6-digit code as "482 193" so it is easy to compare out loud.
func fmtSAS(s string) string {
	if len(s) == 6 {
		return s[:3] + " " + s[3:]
	}
	return s
}

// finder is the list of devices a sender can pick from: the server's list (when it can be reached) plus whatever
// answers on the local network over mDNS. Both work alone, so nearby sending still works with no internet.
type finder struct {
	mu      sync.Mutex
	lobby   *nearby.Lobby
	peers   []lan.Peer
	changed chan struct{} // pokes whenever either list may have changed
}

func (f *finder) poke() {
	select {
	case f.changed <- struct{}{}:
	default:
	}
}

func (f *finder) devices() []nearby.Device {
	f.mu.Lock()
	defer f.mu.Unlock()
	var l []nearby.Device
	if f.lobby != nil {
		l = f.lobby.Devices()
	}
	return nearby.Merge(l, f.peers)
}

// startFinder begins both kinds of discovery. A kind that cannot start is reported and skipped; it is an error
// only when neither works.
func startFinder(ctx context.Context, cfg config.Config, online bool, selfID string) (*finder, error) {
	f := &finder{changed: make(chan struct{}, 1)}
	var lobbyErr error
	if online {
		if f.lobby, lobbyErr = joinNearby(ctx, cfg, true); lobbyErr != nil {
			ui.PrintWarningf("Server list unavailable: %v", lobbyErr)
		} else {
			go func() {
				for range f.lobby.Events {
					f.poke()
				}
				f.mu.Lock()
				f.lobby = nil // the connection ended
				f.mu.Unlock()
				f.poke()
			}()
		}
	}
	lanUp := make(chan error, 1)
	go func() {
		lanUp <- lan.Browse(ctx, selfID, func(ps []lan.Peer) {
			f.mu.Lock()
			f.peers = ps
			f.mu.Unlock()
			f.poke()
		})
	}()
	select { // Browse only returns early when the network cannot do mDNS
	case err := <-lanUp:
		if err != nil {
			ui.PrintWarningf("Local network discovery unavailable: %v", err)
			if f.lobby == nil {
				return nil, errors.New("cannot look for devices: no server and no local network discovery")
			}
		}
	case <-time.After(300 * time.Millisecond):
	}
	return f, nil
}

// pickDevice chooses who to send to: the one named by --to, or one chosen from a numbered list.
func pickDevice(ctx context.Context, f *finder, to string) (nearby.Device, error) {
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	if to == "" && !interactive {
		return nearby.Device{}, errors.New("not a terminal: pass --to <device name>")
	}
	var giveUp <-chan time.Time
	if to != "" {
		giveUp = time.After(5 * time.Second) // the name may just not have shown up yet
	}
	for announced := false; ; {
		devs := f.devices()
		if to != "" {
			var hit []nearby.Device
			for _, d := range devs {
				if strings.EqualFold(d.Name, to) || strings.Contains(strings.ToLower(d.Name), strings.ToLower(to)) {
					hit = append(hit, d)
				}
			}
			if len(hit) == 1 {
				return hit[0], nil
			}
			if len(hit) > 1 {
				return nearby.Device{}, fmt.Errorf("%q matches %d devices: be more specific", to, len(hit))
			}
		} else if len(devs) > 0 {
			fmt.Println()
			for i, d := range devs {
				fmt.Printf("  %d) %s\n", i+1, d.Name)
			}
			fmt.Print("Send to which device? ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= 1 && n <= len(devs) {
				return devs[n-1], nil
			}
			return nearby.Device{}, errors.New("no device chosen")
		} else if !announced {
			announced = true
			ui.PrintInfo("No devices found on this network yet. Waiting... (Ctrl-C to stop)")
		}
		select {
		case <-f.changed: // the list changed: look again
		case <-giveUp:
			return nearby.Device{}, fmt.Errorf("no device named %q found on this network (is it open and set to be visible?)", to)
		case <-ctx.Done():
			return nearby.Device{}, errInterrupted
		}
	}
}

// receiveNearby waits for the first transfer that finishes, from either a device on the local network (ln, may be
// nil) or an invite through the server's list (lobby, may be nil). A direct attempt that fails or is declined is
// reported and the wait goes on; a server invite is answered once, like a share code.
func receiveNearby(ctx context.Context, cfg config.Config, srv config.Server, ln *lan.Listener, lobby *nearby.Lobby, sink protocol.Sink, h transfer.ReceiveHooks) error {
	var session sync.Mutex // the screen can only ask one question at a time
	lanDone, once := make(chan struct{}), sync.Once{}
	lctx, stopLAN := context.WithCancel(ctx)
	defer stopLAN()
	if ln != nil {
		go transfer.ServeLAN(lctx, ln, func(c net.Conn) (bool, error) {
			session.Lock()
			defer session.Unlock()
			offered, err := transfer.ReceiveLAN(lctx, c, deviceName(), sink, h)
			switch {
			case err == nil:
				once.Do(func() { close(lanDone) })
			case lctx.Err() == nil:
				ui.PrintErrorf("%v", err)
				ui.PrintInfo("Waiting for someone to send to you... (Ctrl-C to stop)")
			}
			return offered, err
		})
	}
	var events <-chan nearby.Event
	if lobby != nil {
		events = lobby.Events
	}
	for {
		select {
		case <-lanDone:
			return nil
		case e, ok := <-events:
			if !ok {
				events = nil // the server connection ended; direct transfers still work
				if ln == nil {
					return errors.New("lost the connection to the server")
				}
				continue
			}
			if e.Kind != "invite" {
				continue
			}
			stopLAN() // an invite is answered once, like a share code
			session.Lock()
			defer session.Unlock()
			return transfer.RunReceiveNearby(ctx, cfg, srv, e.RoomID, e.Ticket, deviceName(), sink, h)
		case <-ctx.Done():
			return errInterrupted
		}
	}
}
