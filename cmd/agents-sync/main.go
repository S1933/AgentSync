package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/pivot"
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
		newDiffCmd(),
		newPushCmd(),
		newValidateCmd(),
		newInitCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show differences between pivot and native configs",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("diff: not yet implemented")
		},
	}
}

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Push pivot config to native CLI configs",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("push: not yet implemented")
		},
	}
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the pivot file",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := pivot.Discover(configPath)
			if err != nil {
				return err
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read pivot file: %w", err)
			}

			pivotDir := filepath.Dir(path)
			if _, err := pivot.Parse(data, pivotDir); err != nil {
				return err
			}

			fmt.Printf("pivot file valid: %s\n", path)
			return nil
		},
	}
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a skeleton agentsync.yaml",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("init: not yet implemented")
		},
	}
}
