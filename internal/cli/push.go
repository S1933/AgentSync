package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/diff"
	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
	"github.com/spf13/cobra"
)

// ErrManualEdits indicates push was refused due to manual native edits.
var ErrManualEdits = errors.New("manual edits detected")

// PushOptions configures push for testing and programmatic use.
type PushOptions struct {
	ConfigPath string
	Target     string
	Force      bool
	Adapters   map[string]adapter.Adapter
}

// RunPush pushes pivot config to native CLI configs.
func RunPush(opts PushOptions) error {
	return runPush(opts.ConfigPath, opts.Target, opts.Force, opts.Adapters)
}

// NewPushCmd creates the push subcommand.
func NewPushCmd(configPath *string) *cobra.Command {
	var target string
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push pivot config to native CLI configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun {
				return runDiff(*configPath, target, nil)
			}
			return runPush(*configPath, target, force, nil)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "limit to a single CLI target (e.g. opencode)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show changes without writing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite manually edited native files")
	return cmd
}

func runPush(configPath, target string, force bool, adapters map[string]adapter.Adapter) error {
	pivotDir, generated, state, adapters, err := prepareSync(configPath, target, adapters)
	if err != nil {
		return err
	}

	scope := buildOrphanScope(adapters)
	merged := mergeGenerated(generated)
	results, err := diff.ComputeDiffs(merged, state, scope)
	if err != nil {
		return err
	}

	if diff.HasManualEdits(results) && !force {
		paths := diff.ManualEditPaths(results)
		sort.Strings(paths)
		var b strings.Builder
		b.WriteString("refusing to push: manually edited files detected:\n")
		for _, p := range paths {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		b.WriteString("use --force to overwrite")
		return fmt.Errorf("%w: %s", ErrManualEdits, strings.TrimSpace(b.String()))
	}

	printOrphanWarnings(diff.OrphanedOnly(results))

	wroteAny := false
	for _, name := range sortedAdapterNames(generated) {
		files := generated[name]
		adapterResults, err := diff.ComputeDiffs(files, state, scope)
		if err != nil {
			return err
		}
		adapterResults = diff.FilterOrphaned(adapterResults)

		for _, r := range adapterResults {
			switch r.Status {
			case diff.StatusCreated, diff.StatusModified, diff.StatusManuallyModified:
				content := files[r.Path]
				if err := fsutil.WriteFileAtomic(r.Path, []byte(content), 0o644); err != nil {
					return fmt.Errorf("write %s: %w", r.Path, err)
				}
				state.SetFile(r.Path, name, []byte(content))
				fmt.Printf("[%s] wrote %s (%s)\n", name, r.Path, diffStatusName(r.Status))
				wroteAny = true
			case diff.StatusUnchanged:
				state.SetFile(r.Path, name, []byte(r.NewContent))
			}
		}
	}

	if err := diff.SaveState(pivotDir, state); err != nil {
		return err
	}

	if !wroteAny {
		fmt.Println("No changes")
	} else {
		fmt.Printf("state updated: %s\n", filepath.Join(pivotDir, ".agentsync-state.json"))
	}

	return nil
}

func diffStatusName(status diff.DiffStatus) string {
	switch status {
	case diff.StatusCreated:
		return "created"
	case diff.StatusModified:
		return "modified"
	case diff.StatusManuallyModified:
		return "forced"
	default:
		return "updated"
	}
}

func printOrphanWarnings(results []diff.DiffResult) {
	for _, r := range results {
		if r.Status == diff.StatusOrphaned {
			fmt.Printf("warning: orphaned %s (removed from pivot, still on disk)\n", r.Path)
		}
	}
}

func mergeGenerated(generated map[string]map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, files := range generated {
		for path, content := range files {
			merged[path] = content
		}
	}
	return merged
}

func prepareSync(configPath, target string, adapters map[string]adapter.Adapter) (pivotDir string, generated map[string]map[string]string, state *diff.StateFile, resolved map[string]adapter.Adapter, err error) {
	path, err := pivot.Discover(configPath)
	if err != nil {
		return "", nil, nil, nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil, nil, fmt.Errorf("read pivot file: %w", err)
	}

	pivotDir = filepath.Dir(path)
	pf, err := pivot.Parse(data, pivotDir)
	if err != nil {
		return "", nil, nil, nil, err
	}

	if adapters == nil {
		adapters, err = ResolveTargets(target)
		if err != nil {
			return "", nil, nil, nil, err
		}
	}

	generated, err = Generate(pf, pivotDir, adapters)
	if err != nil {
		return "", nil, nil, nil, err
	}

	state, err = diff.LoadState(pivotDir)
	if err != nil {
		return "", nil, nil, nil, err
	}

	return pivotDir, generated, state, adapters, nil
}

func buildOrphanScope(adapters map[string]adapter.Adapter) *diff.OrphanScope {
	if len(adapters) == 0 {
		return nil
	}
	scope := &diff.OrphanScope{}
	for name, adpt := range adapters {
		scope.AdapterNames = append(scope.AdapterNames, name)
		scope.PathPrefixes = append(scope.PathPrefixes, adpt.TargetPaths()...)
	}
	sort.Strings(scope.AdapterNames)
	sort.Strings(scope.PathPrefixes)
	return scope
}

func sortedAdapterNames(generated map[string]map[string]string) []string {
	names := make([]string, 0, len(generated))
	for name := range generated {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
