package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeDiffsCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.md")
	state := emptyState()

	results, err := ComputeDiffs(map[string]string{path: "generated"}, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Status != StatusCreated {
		t.Errorf("status = %v, want Created", results[0].Status)
	}
}

func TestComputeDiffsUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "same.md")
	content := "same content"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := ComputeDiffs(map[string]string{path: content}, emptyState(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != StatusUnchanged {
		t.Fatalf("got %+v, want unchanged", results)
	}
}

func TestComputeDiffsModified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.md")
	if err := os.WriteFile(path, []byte("on disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := emptyState()
	state.SetFile(path, "", []byte("on disk"))

	results, err := ComputeDiffs(map[string]string{path: "from pivot"}, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != StatusModified {
		t.Fatalf("got %+v, want modified", results)
	}
}

func TestComputeDiffsManuallyModified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edited.md")
	if err := os.WriteFile(path, []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := emptyState()
	state.SetFile(path, "", []byte("last push"))

	results, err := ComputeDiffs(map[string]string{path: "from pivot"}, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != StatusManuallyModified {
		t.Fatalf("got %+v, want manually modified", results)
	}
}

func TestComputeDiffsOrphaned(t *testing.T) {
	dir := t.TempDir()
	orphan := filepath.Join(dir, "old.md")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := emptyState()
	state.SetFile(orphan, "", []byte("orphan"))

	results, err := ComputeDiffs(map[string]string{}, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != StatusOrphaned {
		t.Fatalf("got %+v, want orphaned", results)
	}
}

func TestComputeDiffsOrphanScopeFiltersByAdapter(t *testing.T) {
	dir := t.TempDir()
	claudePath := filepath.Join(dir, "claude", "agents", "build.md")
	opencodePath := filepath.Join(dir, "opencode", "prompts", "build.md")
	for _, path := range []string{claudePath, opencodePath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	state := emptyState()
	state.SetFile(claudePath, "claude-code", []byte("content"))
	state.SetFile(opencodePath, "opencode", []byte("content"))

	scope := &OrphanScope{
		AdapterNames: []string{"opencode"},
		PathPrefixes: []string{filepath.Join(dir, "opencode")},
	}

	results, err := ComputeDiffs(map[string]string{}, state, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d orphans, want 1 (opencode only)", len(results))
	}
	if results[0].Path != opencodePath {
		t.Fatalf("orphan path = %q, want %q", results[0].Path, opencodePath)
	}
}

func TestComputeDiffsSortedPaths(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "c.md"),
		filepath.Join(dir, "a.md"),
		filepath.Join(dir, "b.md"),
	}
	generated := make(map[string]string, len(paths))
	for _, path := range paths {
		generated[path] = "new"
	}

	results, err := ComputeDiffs(generated, emptyState(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(paths) {
		t.Fatalf("got %d results, want %d", len(results), len(paths))
	}
	for i, want := range []string{paths[1], paths[2], paths[0]} {
		if results[i].Path != want {
			t.Fatalf("results[%d].Path = %q, want %q", i, results[i].Path, want)
		}
	}
}

func TestFormatDiff(t *testing.T) {
	results := []DiffResult{
		{
			Path:       "/tmp/foo.md",
			Status:     StatusCreated,
			NewContent: "hello",
		},
		{
			Path:       "/tmp/bar.md",
			Status:     StatusManuallyModified,
			OldContent: "old",
			NewContent: "new",
		},
	}

	out := FormatDiff(results, false)
	for _, want := range []string{"create /tmp/foo.md", "--- a/", "+++ b/", "warning: manually modified", "manual /tmp/bar.md", "-old", "+new"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
