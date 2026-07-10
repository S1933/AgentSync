package diff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStateMissing(t *testing.T) {
	dir := t.TempDir()
	state, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != stateVersion {
		t.Errorf("version = %q, want %q", state.Version, stateVersion)
	}
	if len(state.Files) != 0 {
		t.Errorf("expected empty files map, got %d entries", len(state.Files))
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	original := &StateFile{
		Version: stateVersion,
		Files: map[string]FileState{
			"/tmp/foo.md": {Path: "/tmp/foo.md", Hash: "abc123"},
		},
	}

	if err := SaveState(dir, original); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != stateVersion {
		t.Errorf("version = %q, want %q", loaded.Version, stateVersion)
	}
	got, ok := loaded.Files["/tmp/foo.md"]
	if !ok {
		t.Fatal("missing file entry after load")
	}
	if got.Hash != "abc123" {
		t.Errorf("hash = %q, want abc123", got.Hash)
	}

	path := filepath.Join(dir, stateFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}
}

func TestHashContent(t *testing.T) {
	a := HashContent([]byte("hello"))
	b := HashContent([]byte("hello"))
	c := HashContent([]byte("world"))

	if a != b {
		t.Errorf("hash not deterministic: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("expected different hashes for different content")
	}
	if len(a) != 64 {
		t.Errorf("expected 64-char hex SHA-256, got len %d", len(a))
	}
}
