package cli

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/diff"
	"github.com/spf13/cobra"
)

// DiffOptions configures diff for testing and programmatic use.
type DiffOptions struct {
	ConfigPath string
	Target     string
	Adapters   map[string]adapter.Adapter
}

// RunDiff shows differences between pivot and native configs.
func RunDiff(opts DiffOptions) error {
	return runDiff(opts.ConfigPath, opts.Target, opts.Adapters)
}

// CaptureOutput runs fn while capturing stdout.
func CaptureOutput(fn func() error) (string, error) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	runErr := fn()
	if closeErr := w.Close(); closeErr != nil && runErr == nil {
		runErr = closeErr
	}
	os.Stdout = old

	data, readErr := io.ReadAll(r)
	if readErr != nil && runErr == nil {
		runErr = readErr
	}
	return string(data), runErr
}

// NewDiffCmd creates the diff subcommand.
func NewDiffCmd(configPath *string) *cobra.Command {
	var target string

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show differences between pivot and native configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(*configPath, target, nil)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "limit to a single CLI target (e.g. opencode)")
	return cmd
}

func runDiff(configPath, target string, adapters map[string]adapter.Adapter) error {
	_, generated, state, resolved, err := prepareSync(configPath, target, adapters)
	if err != nil {
		return err
	}

	scope := buildOrphanScope(resolved)
	colored := diff.SupportsColor()
	hasChanges := false

	for _, name := range sortedAdapterNames(generated) {
		files := generated[name]
		results, err := diff.ComputeDiffs(files, state, scope)
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
	allResults, err := diff.ComputeDiffs(merged, state, scope)
	if err != nil {
		return err
	}
	orphaned := diff.OrphanedOnly(allResults)
	sort.Slice(orphaned, func(i, j int) bool {
		return orphaned[i].Path < orphaned[j].Path
	})
	for _, r := range orphaned {
		hasChanges = true
		fmt.Printf("warning: orphaned %s (removed from pivot, still on disk)\n", r.Path)
	}

	if !hasChanges {
		fmt.Println("No changes")
	}

	return nil
}
