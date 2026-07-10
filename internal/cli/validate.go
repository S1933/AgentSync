package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/pivot"
	"github.com/spf13/cobra"
)

// RunValidate validates the pivot file at configPath.
func RunValidate(configPath string) error {
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
}

// NewValidateCmd creates the validate subcommand.
func NewValidateCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the pivot file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunValidate(*configPath)
		},
	}
}
