package cli

import (
	"fmt"

	"github.com/jnuel/agentsync/internal/diff"
	"github.com/spf13/cobra"
)

// NewDiffCmd creates the diff subcommand.
func NewDiffCmd(configPath *string) *cobra.Command {
	var target string

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show differences between pivot and native configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(*configPath, target)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "limit to a single CLI target (e.g. opencode)")
	return cmd
}

func runDiff(configPath, target string) error {
	_, generated, state, err := prepareSync(configPath, target)
	if err != nil {
		return err
	}

	colored := diff.SupportsColor()
	hasChanges := false

	for name, files := range generated {
		results, err := diff.ComputeDiffs(files, state)
		if err != nil {
			return err
		}
		results = diff.FilterOrphaned(results)

		if !diff.HasChanges(results) {
			fmt.Printf("[%s] No changes\n", name)
			continue
		}

		hasChanges = true
		fmt.Printf("[%s]\n", name)
		fmt.Print(diff.FormatDiff(results, colored))
	}

	merged := mergeGenerated(generated)
	allResults, err := diff.ComputeDiffs(merged, state)
	if err != nil {
		return err
	}
	for _, r := range diff.OrphanedOnly(allResults) {
		hasChanges = true
		fmt.Printf("warning: orphaned %s (removed from pivot, still on disk)\n", r.Path)
	}

	if !hasChanges {
		fmt.Println("No changes")
	}

	return nil
}
