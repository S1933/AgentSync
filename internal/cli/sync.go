package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/adapter/claude"
	"github.com/S1933/Shenron/internal/adapter/opencode"
	"github.com/S1933/Shenron/internal/diff"
	"github.com/S1933/Shenron/internal/pivot"
)

type pivotDirSetter interface {
	SetPivotDir(string)
}

type fragmentAccumulator interface {
	ResetFragments()
	Fragments() map[string]any
	ConfigPath() string
}

type managedPruner interface {
	PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error)
}

// Generate produces the file map for each adapter from a parsed pivot file.
// state may be nil; when set, adapters that implement managedPruner use it to
// prune leaves they previously managed but the pivot no longer generates.
func Generate(pf *pivot.PivotFile, pivotDir string, adapters map[string]adapter.Adapter, state *diff.StateFile) (map[string]map[string]string, error) {
	out := make(map[string]map[string]string, len(adapters))

	for name, adpt := range adapters {
		if setter, ok := adpt.(pivotDirSetter); ok {
			setter.SetPivotDir(pivotDir)
		}
		if acc, ok := adpt.(fragmentAccumulator); ok {
			acc.ResetFragments()
		}

		files := make(map[string]string)

		for _, agent := range pf.Agents {
			agentFiles, err := adpt.GenerateAgent(agent)
			if err != nil {
				return nil, fmt.Errorf("%s: generate agent %q: %w", name, agent.ID, err)
			}
			for path, content := range agentFiles {
				files[path] = content
			}
		}

		for _, cmd := range pf.Commands {
			cmdFiles, err := adpt.GenerateCommand(cmd)
			if err != nil {
				return nil, fmt.Errorf("%s: generate command %q: %w", name, cmd.ID, err)
			}
			for path, content := range cmdFiles {
				files[path] = content
			}
		}

		if acc, ok := adpt.(fragmentAccumulator); ok {
			configPath := acc.ConfigPath()
			var existing []byte
			data, err := os.ReadFile(configPath)
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, fmt.Errorf("%s: read %s: %w", name, filepath.Base(configPath), err)
				}
			} else {
				existing = data
			}
			var merged []byte
			if pruner, ok := adpt.(managedPruner); ok && state != nil {
				merged, err = pruner.PruneManaged(configPath, existing, state.Managed(configPath), acc.Fragments())
			} else {
				merged, err = adpt.MergeFile(configPath, existing, acc.Fragments())
			}
			if err != nil {
				return nil, fmt.Errorf("%s: merge %s: %w", name, filepath.Base(configPath), err)
			}
			if merged != nil {
				files[configPath] = string(merged)
			}
		}

		out[name] = files
	}

	return out, nil
}

// Ensure opencode.Adapter satisfies optional interfaces at compile time.
var (
	_ pivotDirSetter      = (*claude.Adapter)(nil)
	_ pivotDirSetter      = (*opencode.Adapter)(nil)
	_ fragmentAccumulator = (*opencode.Adapter)(nil)
	_ managedPruner       = (*opencode.Adapter)(nil)
)
