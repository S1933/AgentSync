package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/adapter/opencode"
	"github.com/jnuel/agentsync/internal/cli"
	"github.com/jnuel/agentsync/internal/pivot"
)

func TestGenerateOpenCode(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join("..", "adapter", "opencode", "testdata")
	data, err := os.ReadFile(filepath.Join(dir, "agentsync.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pf, err := pivot.Parse(data, dir)
	if err != nil {
		t.Fatal(err)
	}

	adapters := map[string]adapter.Adapter{
		"opencode": opencode.NewAdapterWithBaseDir(tmp, dir),
	}

	out, err := cli.Generate(pf, dir, adapters)
	if err != nil {
		t.Fatal(err)
	}

	files := out["opencode"]
	if len(files) < 4 {
		t.Fatalf("expected at least 4 files, got %d", len(files))
	}

	foundConfig := false
	for path := range files {
		if filepath.Base(path) == "opencode.json" {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Error("missing opencode.json in generated files")
	}
}

func TestGenerateConfigReadError(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join("..", "adapter", "opencode", "testdata")
	configPath := filepath.Join(tmp, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o644) })

	data, err := os.ReadFile(filepath.Join(dir, "agentsync.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pf, err := pivot.Parse(data, dir)
	if err != nil {
		t.Fatal(err)
	}

	adapters := map[string]adapter.Adapter{
		"opencode": opencode.NewAdapterWithBaseDir(tmp, dir),
	}

	_, err = cli.Generate(pf, dir, adapters)
	if err == nil {
		t.Fatal("expected error when existing config is unreadable")
	}
}

func TestResolveTargetsUnknown(t *testing.T) {
	_, err := cli.ResolveTargets("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}
