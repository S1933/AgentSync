package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
)

const configFileName = "opencode.json"

// Adapter implements the OpenCode target adapter.
type Adapter struct {
	baseDir   string
	pivotDir  string
	fragments map[string]any
}

// NewAdapter creates an OpenCode adapter writing to the default config directory.
func NewAdapter() *Adapter {
	return &Adapter{
		baseDir:   fsutil.OpenCodePath(),
		fragments: make(map[string]any),
	}
}

// NewAdapterWithBaseDir creates an adapter with a custom base directory (for tests).
func NewAdapterWithBaseDir(baseDir, pivotDir string) *Adapter {
	return &Adapter{
		baseDir:   baseDir,
		pivotDir:  pivotDir,
		fragments: make(map[string]any),
	}
}

// SetPivotDir sets the pivot directory for promptFile resolution.
func (a *Adapter) SetPivotDir(dir string) {
	a.pivotDir = dir
}

// Name returns the adapter identifier.
func (a *Adapter) Name() string {
	return "opencode"
}

// ValidateAgent checks that an agent definition is valid for OpenCode.
func (a *Adapter) ValidateAgent(agent pivot.AgentDefinition) error {
	if agent.Mode != "primary" && agent.Mode != "subagent" {
		return fmt.Errorf("agent %q: mode must be primary or subagent", agent.ID)
	}
	return nil
}

// Fragments returns accumulated JSON fragments for opencode.json merge.
func (a *Adapter) Fragments() map[string]any {
	return a.fragments
}

// ResetFragments clears accumulated fragments before a new generation pass.
func (a *Adapter) ResetFragments() {
	a.fragments = make(map[string]any)
}

// GenerateAgent produces prompt files and accumulates the JSON fragment.
func (a *Adapter) GenerateAgent(agent pivot.AgentDefinition) (map[string]string, error) {
	if err := a.ValidateAgent(agent); err != nil {
		return nil, err
	}

	fragment, promptRel, promptContent, err := GenerateAgentFragment(agent, a.pivotDir)
	if err != nil {
		return nil, err
	}

	a.fragments["agent."+agent.ID] = fragment

	files := map[string]string{}
	if promptContent != "" || agent.SystemPrompt != "" || agent.PromptFile != "" {
		files[filepath.Join(a.baseDir, promptRel)] = promptContent
	}
	return files, nil
}

// GenerateCommand produces command template files and accumulates the JSON fragment.
func (a *Adapter) GenerateCommand(cmd pivot.CommandDefinition) (map[string]string, error) {
	fragment, cmdRel, cmdContent, err := GenerateCommandFragment(cmd)
	if err != nil {
		return nil, err
	}

	a.fragments["command."+cmd.ID] = fragment

	return map[string]string{
		filepath.Join(a.baseDir, cmdRel): cmdContent,
	}, nil
}

// TargetPaths returns paths this adapter writes to.
func (a *Adapter) TargetPaths() []string {
	return []string{
		filepath.Join(a.baseDir, configFileName),
		filepath.Join(a.baseDir, "prompts"),
		filepath.Join(a.baseDir, "command"),
	}
}

// ConfigPath returns the full path to opencode.json.
func (a *Adapter) ConfigPath() string {
	return filepath.Join(a.baseDir, configFileName)
}

// fragmentGroups are the nested containers agentsync-managed fragments live under.
var fragmentGroups = []string{"agent", "command"}

// MergeFile upserts fragments into the nested agent/command objects of an existing
// opencode.json. Managed entries are created or updated in place; every other key —
// including native-only agents/commands and unrelated top-level config — is preserved
// verbatim, and the original key ordering is kept so pushes produce minimal diffs.
func (a *Adapter) MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error) {
	if !strings.HasSuffix(filepath.Base(path), configFileName) {
		return nil, nil
	}

	root, err := parseOrderedObject(existing)
	if err != nil {
		return nil, fmt.Errorf("parse existing JSON: %w", err)
	}

	// Group fragments by their container ("agent"/"command") and leaf id, seeding each
	// container from the existing object so native entries and their order survive.
	containers := map[string]*orderedObject{}
	for _, key := range sortedKeys(fragments) {
		group, leaf, ok := splitFragmentKey(key)
		if !ok {
			raw, err := json.Marshal(fragments[key])
			if err != nil {
				return nil, fmt.Errorf("marshal fragment %q: %w", key, err)
			}
			root.set(key, raw)
			continue
		}

		container := containers[group]
		if container == nil {
			container = newOrderedObject()
			if existingRaw, ok := root.get(group); ok {
				if container, err = parseOrderedObject(existingRaw); err != nil {
					return nil, fmt.Errorf("parse existing %q object: %w", group, err)
				}
			}
			containers[group] = container
		}

		raw, err := json.Marshal(fragments[key])
		if err != nil {
			return nil, fmt.Errorf("marshal fragment %q: %w", key, err)
		}
		container.set(leaf, raw)
	}

	groupNames := make([]string, 0, len(containers))
	for group := range containers {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)
	for _, group := range groupNames {
		raw, err := containers[group].compact()
		if err != nil {
			return nil, fmt.Errorf("serialize %q object: %w", group, err)
		}
		root.set(group, raw)
	}

	compact, err := root.compact()
	if err != nil {
		return nil, fmt.Errorf("serialize JSON: %w", err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil, fmt.Errorf("indent JSON: %w", err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func splitFragmentKey(key string) (group, leaf string, ok bool) {
	for _, group := range fragmentGroups {
		if strings.HasPrefix(key, group+".") {
			return group, key[len(group)+1:], true
		}
	}
	return "", "", false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
