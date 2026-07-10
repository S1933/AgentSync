package main

import (
	"os"

	"github.com/jnuel/agentsync/internal/cli"
	"github.com/spf13/cobra"
)

var configPath string

func main() {
	rootCmd := &cobra.Command{
		Use:   "agents-sync",
		Short: "Sync agent configurations across AI coding assistants",
	}

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to agentsync.yaml pivot file")

	rootCmd.AddCommand(
		cli.NewDiffCmd(&configPath),
		cli.NewPushCmd(&configPath),
		cli.NewValidateCmd(&configPath),
		cli.NewInitCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

