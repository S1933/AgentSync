package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/adapter/claude"
	"github.com/S1933/Shenron/internal/adapter/opencode"
	"github.com/S1933/Shenron/internal/cli"
)

func TestRunPushManualEditDetection(t *testing.T) {
	tmp := t.TempDir()
	fixtureDir := filepath.Join("..", "adapter", "opencode", "testdata")
	pivotPath := filepath.Join(tmp, "shenron.yaml")
	copyFile(t, filepath.Join(fixtureDir, "shenron.yaml"), pivotPath)

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

func TestRunPushTargetDoesNotWarnClaudeOrphans(t *testing.T) {
	tmp := t.TempDir()
	fixtureDir := filepath.Join("..", "..", "testdata", "integration")
	pivotPath := filepath.Join(tmp, "shenron.yaml")
	copyFile(t, filepath.Join(fixtureDir, "shenron.yaml"), pivotPath)
	if err := copyTree(filepath.Join(fixtureDir, "prompts"), filepath.Join(tmp, "prompts")); err != nil {
		t.Fatal(err)
	}

	opencodeDir := filepath.Join(tmp, "opencode")
	claudeDir := filepath.Join(tmp, "claude")
	for _, dir := range []string{opencodeDir, claudeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(t, filepath.Join(fixtureDir, "existing_opencode.json"), filepath.Join(opencodeDir, "opencode.json"))

	adapters := map[string]adapter.Adapter{
		"opencode":    opencode.NewAdapterWithBaseDir(opencodeDir, tmp),
		"claude-code": claude.NewAdapterWithBaseDir(claudeDir, tmp),
	}

	if err := cli.RunPush(cli.PushOptions{ConfigPath: pivotPath, Adapters: adapters}); err != nil {
		t.Fatalf("full push: %v", err)
	}

	out, err := cli.CaptureOutput(func() error {
		return cli.RunPush(cli.PushOptions{
			ConfigPath: pivotPath,
			Target:     "opencode",
			Adapters:   map[string]adapter.Adapter{"opencode": adapters["opencode"]},
		})
	})
	if err != nil {
		t.Fatalf("targeted push: %v", err)
	}
	if strings.Contains(out, "warning: orphaned") && strings.Contains(out, claudeDir) {
		t.Fatalf("targeted push should not warn about Claude orphans, got:\n%s", out)
	}
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
