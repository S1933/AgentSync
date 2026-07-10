package claude

import (
	"fmt"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
)

// Adapter implements the Claude Code target adapter.
type Adapter struct {
	baseDir  string
	pivotDir string
}

// NewAdapter creates a Claude Code adapter.
func NewAdapter() *Adapter {
	return &Adapter{baseDir: fsutil.ClaudePath()}
}

// NewAdapterWithBaseDir creates an adapter with a custom base directory (for tests).
func NewAdapterWithBaseDir(baseDir, pivotDir string) *Adapter {
	return &Adapter{baseDir: baseDir, pivotDir: pivotDir}
}

// SetPivotDir sets the pivot directory for promptFile resolution.
func (a *Adapter) SetPivotDir(dir string) {
	a.pivotDir = dir
}

// Name returns the adapter identifier.
func (a *Adapter) Name() string {
	return "claude-code"
}

// ValidateAgent checks that an agent definition is valid for Claude Code.
func (a *Adapter) ValidateAgent(agent pivot.AgentDefinition) error {
	if agent.Mode != "primary" && agent.Mode != "subagent" {
		return fmt.Errorf("agent %q: mode must be primary or subagent", agent.ID)
	}
	return nil
}

// GenerateAgent produces a Claude Code agent Markdown file.
func (a *Adapter) GenerateAgent(agent pivot.AgentDefinition) (map[string]string, error) {
	if err := a.ValidateAgent(agent); err != nil {
		return nil, err
	}
	return generateAgentFile(agent, a.pivotDir, a.baseDir)
}

// GenerateCommand produces a Claude Code command Markdown file.
func (a *Adapter) GenerateCommand(cmd pivot.CommandDefinition) (map[string]string, error) {
	return generateCommandFile(cmd, a.baseDir)
}

// TargetPaths returns paths this adapter writes to.
func (a *Adapter) TargetPaths() []string {
	return []string{
		filepath.Join(a.baseDir, "agents"),
		filepath.Join(a.baseDir, "commands"),
	}
}

// MergeFile returns nil — Claude Code uses one file per agent/command.
func (a *Adapter) MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error) {
	return nil, nil
}
