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

func TestResolvePipelineSupportsExtends(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(root, "pipe.yaml")
	spec := `version: 1

pipelines:
  - name: base
    steps:
      - name: copy
        kind: shell
        run: cat input.txt > "$PIPE_STEP_OUT/copied.txt"
        inputs:
          - source: input.txt
        outputs:
          - name: text
            path: copied.txt
            type: file

  - name: derived
    extends: base
    steps:
      - name: upper
        kind: shell
        run: tr '[:lower:]' '[:upper:]' < "$PIPE_INPUT_text" > "$PIPE_STEP_OUT/result.txt"
        inputs:
          - from: copy/text
        outputs:
          - name: result
            path: result.txt
            type: file
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFrom(specPath, root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := loaded.ResolvePipeline("derived")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Steps) != 2 {
		t.Fatalf("resolved steps = %d, want 2", len(resolved.Steps))
	}
	if resolved.Steps[0].Name != "copy" || resolved.Steps[1].Name != "upper" {
		t.Fatalf("resolved steps = %+v", resolved.Steps)
	}
}
