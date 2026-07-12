package adapter

import "github.com/S1933/Shenron/internal/pivot"

type Adapter interface {
	Name() string
	ValidateAgent(pivot.AgentDefinition) error
	GenerateAgent(pivot.AgentDefinition) (map[string]string, error)
	GenerateCommand(pivot.CommandDefinition) (map[string]string, error)
	TargetPaths() []string
	MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error)
}

// ManagedPruner is an optional capability for adapters that merge into shared
// files. It removes leaves that shenron previously managed (recorded in
// state.Managed) but that the current pivot no longer generates. Standalone-
// file adapters do not implement it.
type ManagedPruner interface {
	PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error)
}
