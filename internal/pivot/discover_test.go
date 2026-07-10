package pivot

import (
	"os"
	"path/filepath"
	"testing"
)

func normalizePath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return resolved
}

func TestDiscoverFlagPathTakesPriority(t *testing.T) {
	flagPath := filepath.Join("testdata", "valid.yaml")
	got, err := Discover(flagPath)
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.Abs(flagPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Discover() = %q, want %q", got, want)
	}
}

func TestDiscoverWalkUpFindsParentDirectory(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	pivotPath := filepath.Join(root, "agentsync.yaml")
	if err := os.WriteFile(pivotPath, []byte("version: \"1\"\nagents: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}

	got, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.Abs(pivotPath)
	if err != nil {
		t.Fatal(err)
	}
	if normalizePath(t, got) != normalizePath(t, want) {
		t.Errorf("Discover() = %q, want %q", got, want)
	}
}

func TestDiscoverFallbackToHome(t *testing.T) {
	root := t.TempDir()
	homePivot := filepath.Join(root, ".agentsync", "agentsync.yaml")
	if err := os.MkdirAll(filepath.Dir(homePivot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(homePivot, []byte("version: \"1\"\nagents: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", root)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	got, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.Abs(homePivot)
	if err != nil {
		t.Fatal(err)
	}
	if normalizePath(t, got) != normalizePath(t, want) {
		t.Errorf("Discover() = %q, want %q", got, want)
	}
}

func TestDiscoverNotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	_, err = Discover("")
	if err == nil {
		t.Fatal("expected error when pivot file not found")
	}
}
