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
The CLI, the web app and the Android and desktop apps speak the same protocol, so any can send to any other.

Examples:
  warpdrop send report.pdf              # prints a code and a link to share
  warpdrop receive <code|link>          # receive with what the sender shows
  warpdrop send --nearby photo.jpg      # pick a device on this network, no code needed
  warpdrop send --to phone photo.jpg    # same, by name
  warpdrop receive --nearby             # show up on this network and wait for someone to send
  warpdrop config set server https://drop.example.com   # use a self-hosted server`,
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

func loadConfig() (config.Config, error) {
	server, _, err := config.Pick(flagServer)
	if err != nil {
		return config.Config{}, err
	}
	return config.Resolve(server, flagRelay)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagServer, "server", "", "WarpDrop server (default: $WARPDROP_SERVER, else the saved one, else "+config.DefaultServer+")")
	rootCmd.PersistentFlags().BoolVarP(&flagRelay, "relay", "r", false, "Skip WebRTC and use the encrypted relay")
}
