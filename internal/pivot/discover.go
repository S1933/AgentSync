package pivot

import (
	"fmt"
	"os"
	"path/filepath"
)

const pivotFileName = "agentsync.yaml"

// Discover resolves the pivot file path from an explicit flag, walk-up search, or home fallback.
func Discover(flagPath string) (string, error) {
	if flagPath != "" {
		abs, err := filepath.Abs(flagPath)
		if err != nil {
			return "", fmt.Errorf("resolve flag path: %w", err)
		}
		return abs, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	dir := cwd
	for {
		candidate := filepath.Join(dir, pivotFileName)
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Abs(candidate)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	fallback := filepath.Join(home, ".agentsync", pivotFileName)
	if _, err := os.Stat(fallback); err == nil {
		return filepath.Abs(fallback)
	}

	return "", fmt.Errorf("agentsync.yaml not found")
}
