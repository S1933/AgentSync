package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/fsutil"
)

const stateFileName = ".agentsync-state.json"
const stateVersion = "1"

// FileState records the hash of content written by the last successful push.
type FileState struct {
	Hash string `json:"hash"`
	Path string `json:"path"`
}

// StateFile tracks pushed file hashes for manual edit detection.
type StateFile struct {
	Version string               `json:"version"`
	Files   map[string]FileState `json:"files"`
}

// LoadState reads the state file from pivotDir. Missing file returns empty state.
func LoadState(pivotDir string) (*StateFile, error) {
	path := filepath.Join(pivotDir, stateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return emptyState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	state := &StateFile{}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}
	if state.Files == nil {
		state.Files = make(map[string]FileState)
	}
	if state.Version == "" {
		state.Version = stateVersion
	}
	return state, nil
}

// SaveState writes the state file atomically to pivotDir.
func SaveState(pivotDir string, state *StateFile) error {
	if state == nil {
		state = emptyState()
	}
	if state.Version == "" {
		state.Version = stateVersion
	}
	if state.Files == nil {
		state.Files = make(map[string]FileState)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state file: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(pivotDir, stateFileName)
	if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

// HashContent returns the SHA-256 hex digest of data.
func HashContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SetFile records the hash of content for path in state.
func (s *StateFile) SetFile(path string, content []byte) {
	if s.Files == nil {
		s.Files = make(map[string]FileState)
	}
	s.Files[path] = FileState{
		Path: path,
		Hash: HashContent(content),
	}
}

func emptyState() *StateFile {
	return &StateFile{
		Version: stateVersion,
		Files:   make(map[string]FileState),
	}
}
