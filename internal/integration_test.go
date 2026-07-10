package integration_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnuel/agentsync/internal/adapter"
	"github.com/jnuel/agentsync/internal/adapter/claude"
	"github.com/jnuel/agentsync/internal/adapter/opencode"
	"github.com/jnuel/agentsync/internal/cli"
)

const integrationFixtureDir = "../testdata/integration"

func TestEndToEnd_PushOpenCode(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("push: %v", err)
	}

	assertOpenCodePush(t, env)
	assertStateFile(t, env.pivotDir)

	out, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(out, "No changes") {
		t.Fatalf("expected no changes after push, got:\n%s", out)
	}
}

func TestEndToEnd_ManualEditDetection(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("opencode")); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	targetPath := filepath.Join(env.opencodeDir, "prompts", "build.md")
	if err := os.WriteFile(targetPath, []byte("manual edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff after manual edit: %v", err)
	}
	if !strings.Contains(out, "manually modified") {
		t.Fatalf("expected manual modification warning, got:\n%s", out)
	}

	err = cli.RunPush(env.pushOpts("opencode"))
	if err == nil {
		t.Fatal("expected push to refuse without --force")
	}
	if !errors.Is(err, cli.ErrManualEdits) {
		t.Fatalf("expected ErrManualEdits, got: %v", err)
	}

	opts := env.pushOpts("opencode")
	opts.Force = true
	if err := cli.RunPush(opts); err != nil {
		t.Fatalf("force push: %v", err)
	}

	out, err = cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts("opencode"))
	})
	if err != nil {
		t.Fatalf("diff after force push: %v", err)
	}
	if !strings.Contains(out, "No changes") {
		t.Fatalf("expected no changes after force push, got:\n%s", out)
	}
}

func TestEndToEnd_PushClaudeCode(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("claude-code")); err != nil {
		t.Fatalf("push: %v", err)
	}

	buildPath := filepath.Join(env.claudeDir, "agents", "build.md")
	buildContent, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatalf("read build agent: %v", err)
	}
	content := string(buildContent)
	for _, want := range []string{
		"name: build",
		"description: Build and deploy agent",
		"permissionMode: default",
		"- Read",
		"- Bash",
		"You are a build agent responsible for CI/CD tasks.",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("build agent missing %q:\n%s", want, content)
		}
	}

	reviewPath := filepath.Join(env.claudeDir, "agents", "review.md")
	reviewContent, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("read review agent: %v", err)
	}
	if !strings.Contains(string(reviewContent), "permissionMode: plan") {
		t.Errorf("review agent should map edit deny to plan:\n%s", reviewContent)
	}

	shipPath := filepath.Join(env.claudeDir, "commands", "ship.md")
	shipContent, err := os.ReadFile(shipPath)
	if err != nil {
		t.Fatalf("read ship command: %v", err)
	}
	if !strings.Contains(string(shipContent), "<!-- agent: build -->") {
		t.Errorf("ship command missing agent reference:\n%s", shipContent)
	}

	lintPath := filepath.Join(env.claudeDir, "commands", "lint.md")
	if _, err := os.Stat(lintPath); err != nil {
		t.Fatalf("expected standalone lint command: %v", err)
	}
}

func TestEndToEnd_PushBothTargets(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("")); err != nil {
		t.Fatalf("push both targets: %v", err)
	}

	if _, err := os.Stat(filepath.Join(env.opencodeDir, "opencode.json")); err != nil {
		t.Fatalf("opencode config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.claudeDir, "agents", "build.md")); err != nil {
		t.Fatalf("claude build agent missing: %v", err)
	}

	out, err := cli.CaptureOutput(func() error {
		return cli.RunDiff(env.diffOpts(""))
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(out, "[opencode] No changes") {
		t.Fatalf("expected opencode no changes, got:\n%s", out)
	}
	if !strings.Contains(out, "[claude-code] No changes") {
		t.Fatalf("expected claude-code no changes, got:\n%s", out)
	}
}

func TestEndToEnd_PermissionMapping(t *testing.T) {
	env := newIntegrationEnv(t)

	if err := cli.RunPush(env.pushOpts("")); err != nil {
		t.Fatalf("push: %v", err)
	}

	configData, err := os.ReadFile(filepath.Join(env.opencodeDir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(configData, &root); err != nil {
		t.Fatal(err)
	}

	build, ok := root["agent.build"].(map[string]any)
	if !ok {
		t.Fatalf("missing agent.build fragment: %#v", root)
	}
	perms, ok := build["permission"].(map[string]any)
	if !ok {
		t.Fatalf("missing permission block: %#v", build)
	}
	if perms["edit"] != "ask" {
		t.Errorf("edit = %v, want ask", perms["edit"])
	}
	bash, ok := perms["bash"].(map[string]any)
	if !ok {
		t.Fatalf("bash permission = %#v", perms["bash"])
	}
	if bash["go *"] != "allow" || bash["npm *"] != "allow" {
		t.Errorf("unexpected bash patterns: %#v", bash)
	}
	for _, key := range []string{"glob", "grep", "list"} {
		if perms[key] != "allow" {
			t.Errorf("%s = %v, want allow", key, perms[key])
		}
	}
	if perms["lsp"] != "deny" {
		t.Errorf("lsp = %v, want deny", perms["lsp"])
	}

	buildAgent, err := os.ReadFile(filepath.Join(env.claudeDir, "agents", "build.md"))
	if err != nil {
		t.Fatal(err)
	}
	claudeContent := string(buildAgent)
	if !strings.Contains(claudeContent, "permissionMode: default") {
		t.Errorf("expected permissionMode default for edit ask:\n%s", claudeContent)
	}
	if !strings.Contains(claudeContent, "- Bash") {
		t.Errorf("expected Bash tool for bash patterns:\n%s", claudeContent)
	}
	if !strings.Contains(claudeContent, "- Read") {
		t.Errorf("expected Read tool for read allow:\n%s", claudeContent)
	}
}

func TestEndToEnd_Validate(t *testing.T) {
	env := newIntegrationEnv(t)

	out, err := cli.CaptureOutput(func() error {
		return cli.RunValidate(env.pivotPath)
	})
	if err != nil {
		t.Fatalf("valid pivot: %v", err)
	}
	if !strings.Contains(out, "pivot file valid") {
		t.Fatalf("unexpected validate output: %s", out)
	}

	invalidPath := filepath.Join(env.pivotDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte("version: \"1\"\nagents:\n  - id: BAD\n    description: x\n    mode: primary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cli.RunValidate(invalidPath); err == nil {
		t.Fatal("expected validation error for invalid agent id")
	}
}

func TestEndToEnd_Init(t *testing.T) {
	tmp := t.TempDir()
	opencodeDir := filepath.Join(tmp, "opencode")
	if err := copyFile(filepath.Join(integrationFixtureDir, "existing_opencode.json"), filepath.Join(opencodeDir, "opencode.json")); err != nil {
		t.Fatal(err)
	}

	if err := cli.RunInit(cli.InitOptions{WorkDir: tmp, OpenCodeDir: opencodeDir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	outPath := filepath.Join(tmp, "agentsync.yaml")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"version:", "agents:", "id: legacy-helper", "commands:"} {
		if !strings.Contains(content, want) {
			t.Errorf("init output missing %q:\n%s", want, content)
		}
	}
}

type integrationEnv struct {
	pivotDir    string
	pivotPath   string
	opencodeDir string
	claudeDir   string
}

func newIntegrationEnv(t *testing.T) integrationEnv {
	t.Helper()

	tmp := t.TempDir()
	pivotPath := filepath.Join(tmp, "agentsync.yaml")
	if err := copyFile(filepath.Join(integrationFixtureDir, "agentsync.yaml"), pivotPath); err != nil {
		t.Fatal(err)
	}
	if err := copyDir(filepath.Join(integrationFixtureDir, "prompts"), filepath.Join(tmp, "prompts")); err != nil {
		t.Fatal(err)
	}

	opencodeDir := filepath.Join(tmp, "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(integrationFixtureDir, "existing_opencode.json"), filepath.Join(opencodeDir, "opencode.json")); err != nil {
		t.Fatal(err)
	}

	claudeDir := filepath.Join(tmp, "claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	return integrationEnv{
		pivotDir:    tmp,
		pivotPath:   pivotPath,
		opencodeDir: opencodeDir,
		claudeDir:   claudeDir,
	}
}

func (env integrationEnv) adapters() map[string]adapter.Adapter {
	return map[string]adapter.Adapter{
		"opencode":    opencode.NewAdapterWithBaseDir(env.opencodeDir, env.pivotDir),
		"claude-code": claude.NewAdapterWithBaseDir(env.claudeDir, env.pivotDir),
	}
}

func (env integrationEnv) pushOpts(target string) cli.PushOptions {
	adapters := env.adapters()
	if target != "" {
		adapters = map[string]adapter.Adapter{target: env.adapters()[target]}
	}
	return cli.PushOptions{
		ConfigPath: env.pivotPath,
		Target:     target,
		Adapters:   adapters,
	}
}

func (env integrationEnv) diffOpts(target string) cli.DiffOptions {
	adapters := env.adapters()
	if target != "" {
		adapters = map[string]adapter.Adapter{target: env.adapters()[target]}
	}
	return cli.DiffOptions{
		ConfigPath: env.pivotPath,
		Target:     target,
		Adapters:   adapters,
	}
}

func assertOpenCodePush(t *testing.T, env integrationEnv) {
	t.Helper()

	configPath := filepath.Join(env.opencodeDir, "opencode.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(configData, &root); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"agent.build", "agent.review", "agent.scan", "command.ship", "command.lint"} {
		if _, ok := root[key]; !ok {
			t.Errorf("opencode.json missing %q", key)
		}
	}
	if root["theme"] != "dark" {
		t.Errorf("theme = %v, want dark (preserved from existing config)", root["theme"])
	}
	if _, ok := root["agent.legacy-helper"]; !ok {
		t.Error("expected legacy helper agent preserved during merge")
	}

	for _, prompt := range []string{"build.md", "review.md", "scan.md"} {
		path := filepath.Join(env.opencodeDir, "prompts", prompt)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing prompt file %s: %v", path, err)
		}
	}
}

func assertStateFile(t *testing.T, pivotDir string) {
	t.Helper()
	statePath := filepath.Join(pivotDir, ".agentsync-state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func copyDir(src, dst string) error {
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
		return copyFile(path, target)
	})
}
