package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/adapter/opencode"
	"github.com/jnuel/agentsync/internal/cli"
	"github.com/jnuel/agentsync/internal/diff"
	"github.com/jnuel/agentsync/internal/pivot"
)

func TestPushStateAndManualEditDetection(t *testing.T) {
	tmp := t.TempDir()
	pivotDir := filepath.Join("..", "adapter", "opencode", "testdata")
	pivotPath := filepath.Join(tmp, "agentsync.yaml")
	copyFile(t, filepath.Join(pivotDir, "agentsync.yaml"), pivotPath)

	opencodeDir := filepath.Join(tmp, "opencode")
	adapters := map[string]adapter.Adapter{
		"opencode": opencode.NewAdapterWithBaseDir(opencodeDir, pivotDir),
	}

	data, err := os.ReadFile(pivotPath)
	if err != nil {
		t.Fatal(err)
	}
	pf, err := pivot.Parse(data, tmp)
	if err != nil {
		t.Fatal(err)
	}

	generated, err := cli.Generate(pf, tmp, adapters)
	if err != nil {
		t.Fatal(err)
	}

	state := &diff.StateFile{Version: "1", Files: map[string]diff.FileState{}}
	files := generated["opencode"]

	writeGenerated(t, files)
	for path, content := range files {
		state.SetFile(path, []byte(content))
	}
	if err := diff.SaveState(tmp, state); err != nil {
		t.Fatal(err)
	}

	results, err := diff.ComputeDiffs(files, state)
	if err != nil {
		t.Fatal(err)
	}
	if diff.HasChanges(diff.FilterOrphaned(results)) {
		t.Fatalf("expected no changes after push, got %+v", results)
	}

	var targetPath string
	for path := range files {
		if strings.HasSuffix(path, "build.md") {
			targetPath = path
			break
		}
	}
	if targetPath == "" {
		t.Fatal("missing build prompt path")
	}
	if err := os.WriteFile(targetPath, []byte("manual edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err = diff.ComputeDiffs(files, state)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.HasManualEdits(results) {
		t.Fatal("expected manual edit detection")
	}

	out := diff.FormatDiff(diff.FilterOrphaned(results), false)
	if !strings.Contains(out, "warning: manually modified") {
		t.Fatalf("diff output missing manual warning:\n%s", out)
	}
}

func writeGenerated(t *testing.T, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
