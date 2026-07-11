package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/adapter/claude"
	"github.com/S1933/Shenron/internal/adapter/codex"
	"github.com/S1933/Shenron/internal/adapter/opencode"
	"github.com/S1933/Shenron/internal/cli"
	shenronpackage "github.com/S1933/Shenron/internal/package"
)

func TestRunPackageInstallListAndUpdateLocal(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	var output bytes.Buffer

	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source, Output: &output}); err != nil {
		t.Fatalf("RunPackageInstall() error = %v", err)
	}
	if !strings.Contains(output.String(), "installed package acme-reviewers@1.2.3") {
		t.Fatalf("install output = %q", output.String())
	}

	output.Reset()
	if err := cli.RunPackageList(cli.PackageListOptions{Store: store, Output: &output}); err != nil {
		t.Fatalf("RunPackageList() error = %v", err)
	}
	if !strings.Contains(output.String(), "acme-reviewers\t1.2.3") {
		t.Fatalf("list output = %q", output.String())
	}

	writeCLIFile(t, filepath.Join(source, shenronpackage.ManifestFileName), `schemaVersion: "1"
name: acme-reviewers
version: 1.2.4
description: Shared reviewers.
`)
	output.Reset()
	if err := cli.RunPackageUpdate(cli.PackageUpdateOptions{Store: store, Name: "acme-reviewers", Source: source, Output: &output}); err != nil {
		t.Fatalf("RunPackageUpdate() error = %v", err)
	}
	if !strings.Contains(output.String(), "updated package acme-reviewers@1.2.4") {
		t.Fatalf("update output = %q", output.String())
	}
}

func TestRunPackagePushRequiresApprovalAndStoresStateOutsideSnapshot(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	writeCLIFile(t, filepath.Join(source, shenronpackage.PivotFileName), `version: "1"
agents:
  - id: build
    description: Build the project.
    mode: subagent
    systemPrompt: Build carefully.
    permissions:
      edit: allow
commands: []
`)
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source}); err != nil {
		t.Fatalf("install: %v", err)
	}

	claudeDir := filepath.Join(t.TempDir(), "claude")
	adapters := map[string]adapter.Adapter{
		"claude-code": claude.NewAdapterWithBaseDir(claudeDir, ""),
	}
	err := cli.RunPackagePush(cli.PackagePushOptions{
		Store: store, Name: "acme-reviewers", Adapters: adapters,
	})
	if !errors.Is(err, cli.ErrPackagePermissions) {
		t.Fatalf("push without approval error = %v, want ErrPackagePermissions", err)
	}

	if err := cli.RunPackagePush(cli.PackagePushOptions{
		Store: store, Name: "acme-reviewers", Adapters: adapters, AllowPermissions: true,
	}); err != nil {
		t.Fatalf("approved push: %v", err)
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "agents", "build.md")); err != nil {
		t.Fatalf("generated agent: %v", err)
	}
	installed := installedCLIPackage(t, store, "acme-reviewers")
	if _, err := os.Stat(filepath.Join(installed.Root, ".shenron-state.json")); !os.IsNotExist(err) {
		t.Fatalf("state must not be written into package snapshot, stat error = %v", err)
	}
	if _, err := os.Stat(store.StatePath("acme-reviewers")); err != nil {
		t.Fatalf("package state missing: %v", err)
	}

	if err := cli.RunPackagePush(cli.PackagePushOptions{
		Store: store, Name: "acme-reviewers", Adapters: adapters,
	}); err != nil {
		t.Fatalf("approved revision should not need a second flag: %v", err)
	}
}

func TestRunPackagePushRequiresFreshApprovalAfterPackageUpdate(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	writeCLIFile(t, filepath.Join(source, shenronpackage.PivotFileName), `version: "1"
agents:
  - id: build
    description: Build the project.
    mode: subagent
    systemPrompt: Build carefully.
    permissions:
      bash: allow
commands: []
`)
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source}); err != nil {
		t.Fatalf("install: %v", err)
	}
	claudeDir := filepath.Join(t.TempDir(), "claude")
	adapters := map[string]adapter.Adapter{"claude-code": claude.NewAdapterWithBaseDir(claudeDir, "")}
	if err := cli.RunPackagePush(cli.PackagePushOptions{Store: store, Name: "acme-reviewers", Adapters: adapters, AllowPermissions: true}); err != nil {
		t.Fatalf("initial approved push: %v", err)
	}
	writeCLIFile(t, filepath.Join(source, shenronpackage.ManifestFileName), `schemaVersion: "1"
name: acme-reviewers
version: 1.2.4
description: Shared reviewers.
`)
	if err := cli.RunPackageUpdate(cli.PackageUpdateOptions{Store: store, Name: "acme-reviewers", Source: source}); err != nil {
		t.Fatalf("update: %v", err)
	}
	err := cli.RunPackagePush(cli.PackagePushOptions{Store: store, Name: "acme-reviewers", Adapters: adapters})
	if !errors.Is(err, cli.ErrPackagePermissions) {
		t.Fatalf("updated package push error = %v, want ErrPackagePermissions", err)
	}
}

func TestRunPackagePushBlocksMissingRequiredSkillsAndWarnsAboutOptional(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	writeCLIFile(t, filepath.Join(source, shenronpackage.ManifestFileName), `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
skills:
  required: [required-skill]
  optional: [optional-skill]
`)
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source}); err != nil {
		t.Fatalf("install: %v", err)
	}
	var output bytes.Buffer
	err := cli.RunPackagePush(cli.PackagePushOptions{
		Store: store, Name: "acme-reviewers", SkillsDir: filepath.Join(t.TempDir(), "skills"), Output: &output,
	})
	if !errors.Is(err, cli.ErrPackageSkills) {
		t.Fatalf("push error = %v, want ErrPackageSkills", err)
	}
	if !strings.Contains(output.String(), "optional package skills unavailable: optional-skill") {
		t.Fatalf("optional skill warning = %q", output.String())
	}
}

func TestRunPackageDiffReportsPermissionGrantsAndMissingSkills(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	writeCLIFile(t, filepath.Join(source, shenronpackage.ManifestFileName), `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
skills:
  required: [required-skill]
  optional: [optional-skill]
`)
	writeCLIFile(t, filepath.Join(source, shenronpackage.PivotFileName), `version: "1"
agents:
  - id: build
    description: Build the project.
    mode: subagent
    systemPrompt: Build carefully.
    permissions:
      edit: ask
commands: []
`)
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source}); err != nil {
		t.Fatalf("install: %v", err)
	}
	var output bytes.Buffer
	if err := cli.RunPackageDiff(cli.PackageDiffOptions{
		Store: store, Name: "acme-reviewers", SkillsDir: filepath.Join(t.TempDir(), "skills"), Adapters: map[string]adapter.Adapter{}, Output: &output,
	}); err != nil {
		t.Fatalf("diff: %v", err)
	}
	for _, want := range []string{
		"permissions requiring approval: build.edit=ask",
		"required skills unavailable: required-skill",
		"optional skills unavailable: optional-skill",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("diff output = %q, want %q", output.String(), want)
		}
	}
}

func TestRunPackagePushRefusesForeignTargetResources(t *testing.T) {
	tests := []struct {
		name     string
		adapters func(base string) map[string]adapter.Adapter
		foreign  func(base string) string
	}{
		{
			name: "claude",
			adapters: func(base string) map[string]adapter.Adapter {
				return map[string]adapter.Adapter{"claude-code": claude.NewAdapterWithBaseDir(base, "")}
			},
			foreign: func(base string) string { return filepath.Join(base, "agents", "build.md") },
		},
		{
			name: "codex",
			adapters: func(base string) map[string]adapter.Adapter {
				return map[string]adapter.Adapter{"codex": codex.NewAdapterWithBaseDir(base, "")}
			},
			foreign: func(base string) string { return filepath.Join(base, "agents", "build.toml") },
		},
		{
			name: "opencode",
			adapters: func(base string) map[string]adapter.Adapter {
				return map[string]adapter.Adapter{"opencode": opencode.NewAdapterWithBaseDir(base, "")}
			},
			foreign: func(base string) string { return filepath.Join(base, "opencode.json") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := writeCLIPackage(t, "1.2.3")
			writeCLIFile(t, filepath.Join(source, shenronpackage.PivotFileName), `version: "1"
agents:
  - id: build
    description: Build the project.
    mode: subagent
    systemPrompt: Build carefully.
commands: []
`)
			store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
			if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source}); err != nil {
				t.Fatalf("install: %v", err)
			}
			base := filepath.Join(t.TempDir(), tt.name)
			foreignPath := tt.foreign(base)
			foreignContent := "foreign"
			if tt.name == "opencode" {
				foreignContent = `{"agent":{"build":{"description":"foreign"}}}`
			}
			writeCLIFile(t, foreignPath, foreignContent)

			err := cli.RunPackagePush(cli.PackagePushOptions{Store: store, Name: "acme-reviewers", Adapters: tt.adapters(base)})
			if !errors.Is(err, cli.ErrPackageCollision) {
				t.Fatalf("push error = %v, want ErrPackageCollision", err)
			}
		})
	}
}

func TestRunPackagePushPreservesUnrelatedOpenCodeConfiguration(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	writeCLIFile(t, filepath.Join(source, shenronpackage.PivotFileName), `version: "1"
agents:
  - id: build
    description: Build the project.
    mode: subagent
    systemPrompt: Build carefully.
commands: []
`)
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source}); err != nil {
		t.Fatalf("install: %v", err)
	}
	base := filepath.Join(t.TempDir(), "opencode")
	writeCLIFile(t, filepath.Join(base, "opencode.json"), `{"model":"native-model","agent":{"native":{"description":"native"}}}`)
	if err := cli.RunPackagePush(cli.PackagePushOptions{
		Store: store, Name: "acme-reviewers", Adapters: map[string]adapter.Adapter{"opencode": opencode.NewAdapterWithBaseDir(base, "")},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(base, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"model": "native-model"`) || !strings.Contains(string(data), `"native"`) || !strings.Contains(string(data), `"build"`) {
		t.Fatalf("OpenCode config did not preserve native configuration: %s", data)
	}
	if err := cli.RunPackagePush(cli.PackagePushOptions{
		Store: store, Name: "acme-reviewers", Adapters: map[string]adapter.Adapter{"opencode": opencode.NewAdapterWithBaseDir(base, "")},
	}); err != nil {
		t.Fatalf("second package push: %v", err)
	}
}

func writeCLIPackage(t *testing.T, version string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "package")
	writeCLIFile(t, filepath.Join(root, shenronpackage.ManifestFileName), `schemaVersion: "1"
name: acme-reviewers
version: `+version+`
description: Shared reviewers.
`)
	writeCLIFile(t, filepath.Join(root, shenronpackage.PivotFileName), `version: "1"
agents: []
`)
	return root
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installedCLIPackage(t *testing.T, store *shenronpackage.Store, name string) shenronpackage.InstalledPackage {
	t.Helper()
	packages, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, installed := range packages {
		if installed.Name == name {
			return installed
		}
	}
	t.Fatalf("package %q is not installed", name)
	return shenronpackage.InstalledPackage{}
}
