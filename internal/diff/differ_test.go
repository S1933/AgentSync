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

	results, err := ComputeDiffs(map[string]string{path: "generated"}, state)
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

	results, err := ComputeDiffs(map[string]string{path: content}, emptyState())
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
	state.SetFile(path, []byte("on disk"))

	results, err := ComputeDiffs(map[string]string{path: "from pivot"}, state)
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
	state.SetFile(path, []byte("last push"))

	results, err := ComputeDiffs(map[string]string{path: "from pivot"}, state)
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
	state.SetFile(orphan, []byte("orphan"))

	results, err := ComputeDiffs(map[string]string{}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != StatusOrphaned {
		t.Fatalf("got %+v, want orphaned", results)
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
