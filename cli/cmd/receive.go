package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/lan"
	"github.com/BioHazard786/Warpdrop/cli/internal/nearby"
	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
	"github.com/BioHazard786/Warpdrop/cli/internal/transfer"
	"github.com/BioHazard786/Warpdrop/cli/internal/ui"
	"github.com/BioHazard786/Warpdrop/cli/internal/utils"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	flagDir string
	flagZip bool
	flagYes bool

	flagRecvNearby bool
)

var receiveCmd = &cobra.Command{
	Use:     "receive <code|link> | --nearby",
	Aliases: []string{"r"},
	Short:   "Receive files from a browser or another CLI",
	Long: `Receive files using the code or link the sender shows. Files are saved to the current directory
(or --dir), never overwriting existing files.

Examples:
  warpdrop receive 4821-amber-otter-kite-moth
  warpdrop receive 'https://drop.warplabs.info/r/4821#amber-otter-kite-moth' --dir ~/Downloads
  warpdrop receive 4821-amber-otter-kite-moth --yes --zip
  warpdrop receive --nearby             # show up on this network and wait for someone to send to you`,
	Args: func(cmd *cobra.Command, args []string) error {
		if flagRecvNearby {
			return cobra.NoArgs(cmd, args)
		}
		return cobra.ExactArgs(1)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		input := ""
		if len(args) > 0 {
			input = args[0]
		}
		return runReceive(cmd.Context(), input)
	},
}

func runReceive(ctx context.Context, input string) error {
	var share protocol.Share
	if !flagRecvNearby {
		var ok bool
		if share, ok = protocol.ParseShare(input); !ok {
			return errors.New("not a WarpDrop code or link (expected e.g. 4821-amber-otter-kite-moth)")
		}
	}
	if !flagRecvNearby && share.Secret == "" { // a bare room number or a link without its #words
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("the secret words are missing: pass the full code, e.g. 4821-amber-otter-kite-moth")
		}
		fmt.Print("Secret words (four words from the sender): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		words := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(line)), " ", "-")
		if !protocol.IsValidSecret(words) {
			return errors.New("those are not four valid words")
		}
		share.Secret = words
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	stop := ui.RunConnectionSpinner("Connecting to server...")
	srv, err := cfg.Fetch(ctx)
	stop()
	if err != nil && !flagRecvNearby {
		return err
	}
	online := err == nil

	run := func(sink protocol.Sink, h transfer.ReceiveHooks) error {
		return transfer.RunReceive(ctx, cfg, srv, share.RoomID, share.Secret, deviceName(), sink, h)
	}
	if flagRecvNearby {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("not a terminal: nearby transfers always ask before accepting, so they need one")
		}
		// show up on the local network (works with no internet) and, when the server is reachable, in its list
		ln, lanErr := lan.Listen(lan.NewID(), deviceName(), "cli")
		var lobby *nearby.Lobby
		if online {
			var lerr error
			if lobby, lerr = joinNearby(ctx, cfg, false); lerr != nil {
				ui.PrintWarningf("Not listed on the server: %v", lerr)
			} else {
				defer lobby.Close()
			}
		} else {
			ui.PrintWarning("Server unreachable: visible on the local network only")
		}
		if lanErr != nil {
			ui.PrintWarningf("Not visible on the local network: %v", lanErr)
		} else {
			defer ln.Close()
		}
		if lanErr != nil && lobby == nil {
			return errors.New("cannot be seen by anyone: no server and no local network")
		}
		ui.PrintInfof("Visible as %s. Waiting for someone to send to you... (Ctrl-C to stop)", deviceName())
		run = func(sink protocol.Sink, h transfer.ReceiveHooks) error {
			return receiveNearby(ctx, cfg, srv, ln, lobby, sink, h)
		}
	}

	outDir := flagDir
	if flagZip { // receive into a temp dir, then zip
		if outDir, err = os.MkdirTemp("", "warpdrop-receive-*"); err != nil {
			return err
		}
		defer os.RemoveAll(outDir)
	}
	sink, err := transfer.NewDiskSink(outDir)
	if err != nil {
		return err
	}
	defer sink.Close()

	m := &meter{}
	mctx, stopMeter := context.WithCancel(ctx)
	defer stopMeter()
	go m.Run(mctx)

	var prefix []int64
	var total int64
	var names []string
	start := time.Now()
	var sas string
	err = run(sink, transfer.ReceiveHooks{
		OnSAS: func(s string) { sas = s },
		OnStatus: func(status, tr string) {
			switch status {
			case "authenticating":
				m.Clear()
				via := map[string]string{"rtc": "direct", "relay": "relay", "lan": "local network"}[tr]
				ui.PrintInfof("Connected (%s), verifying...", via)
			case "reconnecting":
				m.Clear()
				ui.PrintWarning("Connection lost, resuming...")
			}
		},
		OnOffer: func(from string, fs []protocol.NamedFile) bool {
			total = 0
			prefix, names = nil, nil
			items := make([]ui.FileTableItem, len(fs))
			renamed := 0
			for i, f := range fs {
				prefix = append(prefix, total)
				total += f.Size
				names = append(names, f.SafeName)
				items[i] = ui.FileTableItem{Index: i + 1, Name: f.SafeName, Size: f.Size, Type: f.Type}
				if f.SafeName != f.Name {
					renamed++
				}
			}
			fmt.Println()
			ui.PrintInfof("From %s", from)
			ui.RenderFileTable(items)
			if renamed > 0 {
				ui.PrintWarningf("%d unsafe name(s) were cleaned up before saving", renamed)
			}
			if sas != "" { // nearby: no code was typed, so the people compare these digits instead
				ui.PrintInfof("Code %s: it must match the code on %s's screen", fmtSAS(sas), from)
			}
			return confirm(fmt.Sprintf("Receive %d file(s), %s, from %s?", len(fs), utils.FormatSize(total), from), sas == "")
		},
		OnProgress: func(idx int, b int64) { m.Update("receiving", prefix[idx]+b, total) },
	})
	m.Clear()
	switch {
	case ctx.Err() != nil: // Ctrl-C: the transfer was cancelled cleanly (partial files removed, sender told)
		return errInterrupted
	case err != nil:
		return err
	}

	if flagZip {
		return zipResult(outDir, flagDir)
	}
	ui.RenderTransferSummary(ui.TransferSummary{
		Status: "Complete", Files: len(names), TotalSize: utils.FormatSize(total),
		Duration: utils.FormatTimeDuration(time.Since(start)),
		Speed:    utils.FormatSpeed(float64(total) / max(time.Since(start).Seconds(), 0.001)),
	})
	return nil
}

func zipResult(tmp, dest string) error {
	if dest == "" {
		dest = "."
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	zipName := filepath.Join(dest, fmt.Sprintf("warpdrop-%d.zip", time.Now().UnixMilli()))
	if err := utils.ZipDirectory(tmp, zipName); err != nil {
		return fmt.Errorf("zip files: %w", err)
	}
	ui.PrintSuccessf("Saved %s", zipName)
	return nil
}

// confirm asks the user unless --yes. Without a terminal and without --yes the answer is no: never accept silently.
// --yes is ignored for nearby offers (allowYes=false): anyone on the network can send one.
func confirm(q string, allowYes bool) bool {
	if flagYes && allowYes {
		return true
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		ui.PrintError("not a terminal: pass --yes to accept without asking")
		return false
	}
	fmt.Printf("%s [Y/n] ", q)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}
	return false
}

func init() {
	rootCmd.AddCommand(receiveCmd)
	receiveCmd.Flags().StringVarP(&flagDir, "dir", "d", "", "Directory to save into (default: current directory)")
	receiveCmd.Flags().BoolVarP(&flagZip, "zip", "z", false, "Save everything as one zip")
	receiveCmd.Flags().BoolVarP(&flagRecvNearby, "nearby", "n", false, "Show up to devices on this network and wait for one to send")
	receiveCmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Accept without asking")
}
