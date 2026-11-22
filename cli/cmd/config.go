package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or change saved settings (server)",
	Long: `Settings are saved in a config file so you don't repeat them on every command. Precedence, highest first:
flag, environment variable, saved config, built-in default.

  warpdrop config set server https://drop.example.com   # once, for a self-hosted server
  warpdrop config get                                    # what is in effect, and where it comes from
  warpdrop config unset server`,
	Args: cobra.NoArgs,
	RunE: runConfigGet,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(
		&cobra.Command{Use: "get", Short: "Show the settings in effect", Args: cobra.NoArgs, RunE: runConfigGet},
		&cobra.Command{Use: "set server <url>", Short: "Save the server", Args: cobra.ExactArgs(2), RunE: runConfigSet},
		&cobra.Command{Use: "unset server", Short: "Forget the saved server", Args: cobra.ExactArgs(1), RunE: runConfigUnset},
		&cobra.Command{Use: "path", Short: "Print the config file location", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			p, err := config.Path()
			if err == nil {
				fmt.Println(p)
			}
			return err
		}},
	)
}

func runConfigGet(*cobra.Command, []string) error {
	picked, src, err := config.Pick(flagServer)
	if err != nil {
		return err
	}
	server, err := config.NormalizeServer(picked)
	if err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Printf("server  %s  (%s)\nconfig  %s\n", server, src, path)
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	if !strings.EqualFold(args[0], "server") {
		return fmt.Errorf("unknown setting %q (want server)", args[0])
	}
	server, err := config.NormalizeServer(args[1])
	if err != nil {
		return err
	}
	if err := (config.File{Server: server}).Save(); err != nil {
		return err
	}
	// Saved either way: being offline right now shouldn't block configuring a server.
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()
	info, err := config.Config{Server: server}.Fetch(ctx)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "saved %s, but could not verify it: %v\n", server, err)
		if !strings.Contains(args[1], "://") {
			fmt.Fprintln(os.Stderr, "hint: a bare host means https. For a server without TLS, include http://, e.g. http://localhost:8080")
		}
	case info.Version != "":
		fmt.Printf("saved %s (WarpDrop %s)\n", server, info.Version)
	default:
		fmt.Printf("saved %s\n", server)
	}
	return nil
}

func runConfigUnset(_ *cobra.Command, args []string) error {
	if !strings.EqualFold(args[0], "server") {
		return fmt.Errorf("unknown setting %q (want server)", args[0])
	}
	if err := (config.File{}).Save(); err != nil {
		return err
	}
	fmt.Println("removed server")
	return nil
}
