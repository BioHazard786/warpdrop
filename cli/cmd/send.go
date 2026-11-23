package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/files"
	"github.com/BioHazard786/Warpdrop/cli/internal/lan"
	"github.com/BioHazard786/Warpdrop/cli/internal/nearby"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
	"github.com/BioHazard786/Warpdrop/cli/internal/transfer"
	"github.com/BioHazard786/Warpdrop/cli/internal/ui"
	"github.com/BioHazard786/Warpdrop/cli/internal/utils"
	"github.com/BioHazard786/Warpdrop/cli/internal/version"
	"github.com/spf13/cobra"
)

var (
	flagKeepOpen   bool
	flagSendNearby bool
	flagTo         string
)

var sendCmd = &cobra.Command{
	Use:     "send <files...>",
	Aliases: []string{"s"},
	Short:   "Send files with a code, or to a nearby device (--nearby)",
	Long: `Send files directly to a receiver. Share the printed code or link; the receiver can use the
web app or "warpdrop receive". The words after the number never reach the server.

Examples:
  warpdrop send report.pdf photo.jpg
  warpdrop send --keep-open big.iso     # serve several receivers
  warpdrop send --relay file.zip        # force the encrypted relay
  warpdrop send --nearby photo.jpg      # pick a device on this network, no code needed
  warpdrop send --to phone photo.jpg    # same, by name`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runSend(cmd.Context(), args) },
}

func deviceName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		h = "terminal"
	}
	return protocol.DisplayName(h) + " (CLI " + version.Version + ")"
}

func runSend(ctx context.Context, paths []string) error {
	infos, err := files.ValidateFiles(paths)
	if err != nil {
		return err
	}
	srcs := make([]protocol.Source, len(infos))
	items := make([]ui.FileTableItem, len(infos))
	for i, f := range infos {
		fh, err := os.Open(f.Path)
		if err != nil {
			return err
		}
		defer fh.Close()
		srcs[i] = protocol.Source{Entry: protocol.FileEntry{ID: i, Name: f.Name, Size: f.Size, Type: f.Type}, R: fh}
		items[i] = ui.FileTableItem{Index: i + 1, Name: protocol.DisplayName(f.Name), Size: f.Size, Type: f.Type}
	}
	fmt.Println()
	ui.RenderFileTable(items)

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	nearbyMode := flagSendNearby || flagTo != ""
	stop := ui.RunConnectionSpinner("Connecting to server...")
	srv, err := cfg.Fetch(ctx)
	stop()
	online := err == nil
	if err != nil {
		if !nearbyMode {
			return err
		}
		ui.PrintWarningf("Server unreachable: looking on the local network only")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// nearby: choose a device from this network. One on the local network is sent to directly; one only in the
	// server's list gets an invite to our room.
	var invite func(roomID, token string) error
	var direct net.Conn
	var to nearby.Device
	if nearbyMode {
		f, err := startFinder(runCtx, cfg, online, lan.NewID())
		if err != nil {
			return err
		}
		if f.lobby != nil {
			defer f.lobby.Close()
		}
		if to, err = pickDevice(runCtx, f, flagTo); err != nil {
			return err
		}
		if to.LAN != nil {
			var derr error
			if direct, derr = lan.Dial(runCtx, *to.LAN); derr != nil {
				if !to.InLobby() || f.lobby == nil {
					return derr
				}
				ui.PrintWarningf("%v: using the server instead", derr)
			}
		}
		if direct == nil {
			lobby := f.lobby
			invite = func(roomID, token string) error { return lobby.Invite(to.ID, roomID, token) }
		}
		ui.PrintInfof("Sending to %s", to.Name)
	}
	m := &meter{}
	go m.Run(runCtx)

	var (
		mu        sync.Mutex
		delivered bool
		seen      = map[string]string{} // peer -> last printed status
		code      string
	)
	hooks := transfer.SendHooks{
		OnRoom: func(room, secret string) {
			if secret == "" { // nearby: no code to share
				return
			}
			code = room + "-" + secret
			fmt.Println()
			ui.RenderRoomInfo(code, cfg.ShareLink(room, secret))
			fmt.Println()
			ui.PrintInfo("Waiting for a receiver... (Ctrl-C to stop)")
		},
		OnStatus: func(msg string) { m.Clear(); ui.PrintWarning(msg) },
		OnPeer: func(v transfer.PeerView) {
			mu.Lock()
			defer mu.Unlock()
			if v.Status == "sending" {
				m.Update(v.Device, v.Sent, v.Total)
				return
			}
			key := v.Status + "/" + v.Transport
			if seen[v.ID] == key {
				return
			}
			seen[v.ID] = key
			m.Clear()
			via := map[string]string{"rtc": "direct", "relay": "relay", "lan": "local network", "": ""}[v.Transport]
			switch v.Status {
			case "authenticating":
				ui.PrintInfof("%s connected (%s), verifying...", v.Device, via)
			case "waiting":
				if v.SAS != "" {
					ui.PrintInfof("Waiting for %s to accept. Code %s must match on both screens", v.Device, fmtSAS(v.SAS))
				} else {
					ui.PrintInfof("Waiting for %s to accept...", v.Device)
				}
			case "done":
				delivered = true
				ui.PrintSuccessf("Delivered %d file(s), %s, to %s", len(srcs), utils.FormatSize(v.Total), v.Device)
				if !flagKeepOpen {
					go func() { time.Sleep(200 * time.Millisecond); cancel() }() // let the final frames land
				}
			case "failed":
				ui.PrintErrorf("%s: %s", v.Device, v.Reason)
			}
		},
		OnPeerGone: func(id string) { mu.Lock(); delete(seen, id); mu.Unlock() },
	}
	switch {
	case direct != nil:
		err = transfer.RunSendLAN(runCtx, direct, to.Name, srcs, deviceName(), hooks)
	case invite != nil:
		err = transfer.RunSendNearby(runCtx, cfg, srv, srcs, deviceName(), hooks, invite)
	default:
		err = transfer.RunSend(runCtx, cfg, srv, srcs, deviceName(), hooks)
	}
	m.Clear()
	_ = code
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if !delivered {
		return errInterrupted
	}
	return nil
}

func init() {
	rootCmd.AddCommand(sendCmd)
	sendCmd.Flags().BoolVarP(&flagSendNearby, "nearby", "n", false, "Pick a device on this network instead of sharing a code")
	sendCmd.Flags().StringVar(&flagTo, "to", "", "Send to the nearby device with this name (implies --nearby)")
	sendCmd.Flags().BoolVarP(&flagKeepOpen, "keep-open", "k", false, "Keep serving after the first delivery (up to 10 receivers)")
}
