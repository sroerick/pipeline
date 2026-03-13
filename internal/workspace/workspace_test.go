package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeReplacesExistingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(source, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Materialize(source, target, ModeCopy); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "new\n"; got != want {
		t.Fatalf("target content = %q, want %q", got, want)
	}
}

func TestMaterializeRejectsExistingDirectoryTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	err := Materialize(source, target, ModeCopy)
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("Materialize() error = %v, want ErrTargetExists", err)
	}
}

func TestEnsureEmptyDirRejectsNonEmptyDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sentinel.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := EnsureEmptyDir(target)
	if !errors.Is(err, ErrTargetNotEmpty) {
		t.Fatalf("EnsureEmptyDir() error = %v, want ErrTargetNotEmpty", err)
	}
}
