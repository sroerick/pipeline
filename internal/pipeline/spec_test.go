package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromValidatesSourcesAgainstProjectRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Quotes.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(root, ".pipe", "pipe.yaml")
	spec := `version: 1

pipelines:
  - name: demo
    steps:
      - name: render
        kind: shell
        run: cat Quotes.md > "$PIPE_STEP_OUT/out.txt"
        inputs:
          - source: Quotes.md
        outputs:
          - name: out
            path: out.txt
            type: file
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFrom(specPath, root); err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
}
