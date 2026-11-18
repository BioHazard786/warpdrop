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
)

var receiveCmd = &cobra.Command{
	Use:     "receive <code|link>",
	Aliases: []string{"r"},
	Short:   "Receive files from a browser or another CLI",
	Long: `Receive files using the code or link the sender shows. Files are saved to the current directory
(or --dir), never overwriting existing files.

Examples:
  warpdrop receive 4821-amber-otter-kite-moth
  warpdrop receive 'https://warpdrop.qzz.io/r/4821#amber-otter-kite-moth' --dir ~/Downloads
  warpdrop receive 4821-amber-otter-kite-moth --yes --zip`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runReceive(cmd.Context(), args[0]) },
}

func runReceive(ctx context.Context, input string) error {
	share, ok := protocol.ParseShare(input)
	if !ok {
		return errors.New("not a WarpDrop code or link (expected e.g. 4821-amber-otter-kite-moth)")
	}
	if share.Secret == "" { // a bare room number or a link without its #words
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
	if err != nil {
		return err
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
	err = transfer.RunReceive(ctx, cfg, srv, share.RoomID, share.Secret, deviceName(), sink, transfer.ReceiveHooks{
		OnStatus: func(status, tr string) {
			switch status {
			case "authenticating":
				m.Clear()
				via := map[string]string{"rtc": "direct", "relay": "relay"}[tr]
				ui.PrintInfof("Connected (%s), verifying...", via)
			case "reconnecting":
				m.Clear()
				ui.PrintWarning("Connection lost, resuming...")
			}
		},
		OnOffer: func(fs []protocol.NamedFile) bool {
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
			ui.RenderFileTable(items)
			if renamed > 0 {
				ui.PrintWarningf("%d unsafe name(s) were cleaned up before saving", renamed)
			}
			return confirm(fmt.Sprintf("Receive %d file(s), %s?", len(fs), utils.FormatSize(total)))
		},
		OnProgress: func(idx int, b int64) { m.Update("receiving", prefix[idx]+b, total) },
	})
	m.Clear()
	switch {
	case errors.Is(err, context.Canceled):
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
func confirm(q string) bool {
	if flagYes {
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
	receiveCmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Accept without asking")
}
