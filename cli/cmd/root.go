package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/BioHazard786/Warpdrop/cli/internal/ui"
	"github.com/BioHazard786/Warpdrop/cli/internal/version"
	"github.com/spf13/cobra"
)

var (
	flagServer string
	flagRelay  bool
)

var rootCmd = &cobra.Command{
	Use:   "warpdrop",
	Short: "Send files directly between devices, from the terminal or the browser",
	Long: `WarpDrop sends files device to device over WebRTC, with an end-to-end encrypted relay as fallback.
The CLI and the web app speak the same protocol, so either can send to the other.`,
	Version: version.Version,
}

// errInterrupted means the user pressed Ctrl-C: exit quietly with the conventional status.
var errInterrupted = errors.New("interrupted")

// Execute runs the CLI. Ctrl-C cancels the context, so transfers stop cleanly (partial files are removed,
// the peer is told) instead of the process dying mid-write.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	err := rootCmd.ExecuteContext(ctx)
	stop()
	switch {
	case err == nil:
	case errors.Is(err, errInterrupted):
		os.Exit(130)
	default:
		ui.PrintError(err.Error())
		os.Exit(1)
	}
}

func loadConfig() (config.Config, error) { return config.Resolve(flagServer, flagRelay) }

func init() {
	rootCmd.PersistentFlags().StringVar(&flagServer, "server", "", "WarpDrop server (default $WARPDROP_SERVER or "+config.DefaultServer+")")
	rootCmd.PersistentFlags().BoolVarP(&flagRelay, "relay", "r", false, "Skip WebRTC and use the encrypted relay")
}
