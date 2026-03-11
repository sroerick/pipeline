package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpecPathPrefersHiddenPipeSpec(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pipe.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(root, ".pipe", "pipe.yaml")
	if err := os.WriteFile(hidden, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := SpecPath(root); got != hidden {
		t.Fatalf("SpecPath() = %s, want %s", got, hidden)
	}
}
