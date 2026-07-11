package shenronpackage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDirectoryAcceptsValidPackage(t *testing.T) {
	root := writePackage(t, `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
skills:
  required: [verification-before-completion]
  optional: [requesting-code-review]
`, `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: prompts/review.md
`)

	pkg, err := LoadDirectory(root)
	if err != nil {
		t.Fatalf("LoadDirectory() error = %v", err)
	}
	if pkg.Manifest.Name != "acme-reviewers" {
		t.Errorf("name = %q, want acme-reviewers", pkg.Manifest.Name)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Root != resolvedRoot {
		t.Errorf("root = %q, want %q", pkg.Root, resolvedRoot)
	}
}

func TestLoadDirectoryRejectsInvalidManifest(t *testing.T) {
	root := writePackage(t, `schemaVersion: "2"
name: Acme Reviewers
version: not-a-version
description: ""
skills:
  required: [valid-skill, duplicate]
  optional: [duplicate, Invalid Skill]
`, validPivot())

	_, err := LoadDirectory(root)
	if err == nil {
		t.Fatal("LoadDirectory() error = nil, want validation failure")
	}
	for _, want := range []string{
		"schemaVersion must be \"1\"", "name must match", "version must be a semantic version",
		"description is required", "skills.optional[1] must match", "skills.required and skills.optional overlap",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
}

func TestLoadDirectoryRejectsUnknownManifestField(t *testing.T) {
	root := writePackage(t, `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
unexpected: value
`, validPivot())

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("LoadDirectory() error = %v, want unknown-field error", err)
	}
}

func TestLoadDirectoryRequiresRootPivot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ManifestFileName), validManifest())

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), PivotFileName) {
		t.Fatalf("LoadDirectory() error = %v, want missing pivot error", err)
	}
}

func TestLoadDirectoryRejectsPromptFileOutsidePackageRoot(t *testing.T) {
	root := writePackage(t, validManifest(), `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: ../outside.md
`)
	writeFile(t, filepath.Join(filepath.Dir(root), "outside.md"), "outside")

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "escapes package root") {
		t.Fatalf("LoadDirectory() error = %v, want containment error", err)
	}
}

func TestLoadDirectoryRejectsPromptFileSymlinkEscape(t *testing.T) {
	root := writePackage(t, validManifest(), `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: prompts/review.md
`)
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeFile(t, outside, "outside")
	promptPath := filepath.Join(root, "prompts", "review.md")
	if err := os.Remove(promptPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, promptPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("LoadDirectory() error = %v, want symlink rejection", err)
	}
}

func TestLoadDirectoryRejectsContainedSymlinks(t *testing.T) {
	tests := []struct {
		name           string
		linkPath       string
		target         string
		absoluteTarget bool
	}{
		{name: "manifest", linkPath: ManifestFileName, target: "manifest-source.yaml"},
		{name: "pivot", linkPath: PivotFileName, target: "pivot-source.yaml", absoluteTarget: true},
		{name: "prompt", linkPath: "prompts/review.md", target: "review-source.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writePackage(t, validManifest(), validPivot())
			link := filepath.Join(root, tt.linkPath)
			target := filepath.Join(filepath.Dir(link), tt.target)
			if tt.linkPath == ManifestFileName || tt.linkPath == PivotFileName {
				target = filepath.Join(root, tt.target)
			}
			contents, err := os.ReadFile(link)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, target, string(contents))
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			linkTarget := tt.target
			if tt.absoluteTarget {
				linkTarget = target
			}
			if err := os.Symlink(linkTarget, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			_, err = LoadDirectory(root)
			if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
				t.Fatalf("LoadDirectory() error = %v, want symlink rejection", err)
			}
		})
	}
}

func TestStoreInstallLocalRejectsContainedSymlink(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	prompt := filepath.Join(source, "prompts", "review.md")
	target := filepath.Join(source, "prompts", "review-source.md")
	contents, err := os.ReadFile(prompt)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, string(contents))
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("review-source.md", prompt); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err = NewStore(filepath.Join(t.TempDir(), "cache")).InstallLocal(source)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("InstallLocal() error = %v, want symlink rejection", err)
	}
}

func TestStoreInstallLocalCreatesIndependentContentAddressedSnapshot(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))

	installed, err := store.InstallLocal(source)
	if err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if installed.Digest == "" || !strings.Contains(installed.Root, installed.Digest) {
		t.Errorf("installed = %+v, want content-addressed root", installed)
	}
	if got, err := os.ReadFile(filepath.Join(installed.Root, PivotFileName)); err != nil || string(got) != validPivot() {
		t.Fatalf("snapshot pivot = %q, %v", got, err)
	}

	writeFile(t, filepath.Join(source, PivotFileName), `version: "1"
agents: []
`)
	got, err := os.ReadFile(filepath.Join(installed.Root, PivotFileName))
	if err != nil || string(got) != validPivot() {
		t.Fatalf("snapshot changed with source: %q, %v", got, err)
	}
}

func TestStoreInstallLocalValidatesExistingSnapshotBeforeIndexing(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	pkg, err := LoadDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		t.Fatal(err)
	}
	stagedRoot := filepath.Join(store.root, "packages", pkg.Manifest.Name, digest)
	writeFile(t, filepath.Join(stagedRoot, ManifestFileName), validManifest())
	writeFile(t, filepath.Join(stagedRoot, PivotFileName), validPivot())
	prompt := filepath.Join(stagedRoot, "prompts", "review.md")
	writeFile(t, prompt, "Review this change.")
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("review-source.md", prompt); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeFile(t, filepath.Join(stagedRoot, "prompts", "review-source.md"), "Review this change.")

	_, err = store.InstallLocal(source)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("InstallLocal() error = %v, want staged snapshot validation failure", err)
	}
	installed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 0 {
		t.Fatalf("List() = %+v, want no indexed packages", installed)
	}
}

func TestStoreInstallLocalWaitsForIndexLock(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	unlock, err := store.lockIndex()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	result := make(chan error, 1)
	go func() {
		_, err := store.InstallLocal(writePackage(t, validManifest(), validPivot()))
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("InstallLocal() completed while index lock held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	unlock = func() error { return nil }
	if err := <-result; err != nil {
		t.Fatalf("InstallLocal() after unlocking = %v", err)
	}
}

func TestParseManifestValidatesPrereleaseNumericIdentifiers(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{version: "1.2.3-01", valid: false},
		{version: "1.2.3-01a", valid: true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			_, err := ParseManifest([]byte(strings.Replace(validManifest(), "1.2.3", tt.version, 1)))
			if tt.valid && err != nil {
				t.Fatalf("ParseManifest() error = %v, want valid semantic version", err)
			}
			if !tt.valid && (err == nil || !strings.Contains(err.Error(), "version must be a semantic version")) {
				t.Fatalf("ParseManifest() error = %v, want semantic version error", err)
			}
		})
	}
}

func TestParseManifestRejectsSecondDocument(t *testing.T) {
	_, err := ParseManifest([]byte(validManifest() + "---\nname: ignored\n"))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("ParseManifest() error = %v, want multiple-document error", err)
	}
}

func TestStoreInstallLocalRefusesDuplicatePackageName(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	if _, err := store.InstallLocal(writePackage(t, validManifest(), validPivot())); err != nil {
		t.Fatalf("first InstallLocal() error = %v", err)
	}

	other := writePackage(t, validManifest(), `version: "1"
agents:
  - id: another
    description: Another agent.
    mode: primary
`)
	_, err := store.InstallLocal(other)
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("second InstallLocal() error = %v, want duplicate-name error", err)
	}
}

func TestStoreListReturnsInstalledLocalPackages(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	installed, err := store.InstallLocal(source)
	if err != nil {
		t.Fatal(err)
	}

	packages, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Name != "acme-reviewers" || packages[0].Digest != installed.Digest || packages[0].Source != resolvedSource {
		t.Fatalf("List() = %+v, want installed package", packages)
	}
}

func writePackage(t *testing.T, manifest, pivot string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "package")
	writeFile(t, filepath.Join(root, ManifestFileName), manifest)
	writeFile(t, filepath.Join(root, PivotFileName), pivot)
	writeFile(t, filepath.Join(root, "prompts", "review.md"), "Review this change.")
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validManifest() string {
	return `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
`
}

func validPivot() string {
	return `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: prompts/review.md
`
}
