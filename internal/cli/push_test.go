package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/adapter/opencode"
	"github.com/jnuel/agentsync/internal/cli"
)

func TestRunPushManualEditDetection(t *testing.T) {
	tmp := t.TempDir()
	fixtureDir := filepath.Join("..", "adapter", "opencode", "testdata")
	pivotPath := filepath.Join(tmp, "agentsync.yaml")
	copyFile(t, filepath.Join(fixtureDir, "agentsync.yaml"), pivotPath)

	opencodeDir := filepath.Join(tmp, "opencode")
	adapters := map[string]adapter.Adapter{
		"opencode": opencode.NewAdapterWithBaseDir(opencodeDir, tmp),
	}
	opts := cli.PushOptions{
		ConfigPath: pivotPath,
		Target:     "opencode",
		Adapters:   adapters,
	}

	if err := cli.RunPush(opts); err != nil {
		t.Fatalf("first push: %v", err)
	}

	targetPath := filepath.Join(opencodeDir, "prompts", "build.md")
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected generated build prompt at %s: %v", targetPath, err)
	}

	if err := os.WriteFile(targetPath, []byte("manual edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cli.RunPush(opts)
	if err == nil {
		t.Fatal("expected error when pushing with manual edits and no --force")
	}
	if !errors.Is(err, cli.ErrManualEdits) {
		t.Fatalf("expected ErrManualEdits, got: %v", err)
	}
	if !strings.Contains(err.Error(), targetPath) {
		t.Fatalf("error should list edited path %q: %v", targetPath, err)
	}

	opts.Force = true
	if err := cli.RunPush(opts); err != nil {
		t.Fatalf("force push: %v", err)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "manual edit" {
		t.Fatal("force push did not overwrite manual edit")
	}
	if !strings.Contains(string(data), "build agent") {
		t.Fatalf("expected restored pivot content, got: %q", string(data))
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
