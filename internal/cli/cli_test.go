package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"pipe/internal/config"
	"pipe/internal/db"
)

func TestCLIEndToEnd(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	writeFile(t, filepath.Join(projectDir, "input.txt"), "hello pipeline\n")
	writeFile(t, filepath.Join(projectDir, "pipe.yaml"), `
version: 1

pipelines:
  - name: demo
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

      - name: upper
        kind: shell
        run: tr '[:lower:]' '[:upper:]' < "$PIPE_INPUT_text" > "$PIPE_STEP_OUT/result.txt"
        inputs:
          - from: copy/text
        outputs:
          - name: result
            path: result.txt
            type: file
            publish: out/result.txt
`)

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousWD)
	})
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	for _, args := range [][]string{
		{"init"},
		{"run", "demo"},
		{"stages", "current"},
		{"stages", "alias:current"},
		{"show", "demo:upper/result"},
		{"show", "alias:current"},
		{"provenance", "demo:upper/result"},
	} {
		if err := Run(ctx, args); err != nil {
			t.Fatalf("Run(%v): %v", args, err)
		}
	}

	autoPublishedPath := filepath.Join(projectDir, "out", "result.txt")
	data, err := os.ReadFile(autoPublishedPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "HELLO PIPELINE\n"; got != want {
		t.Fatalf("auto-published content = %q, want %q", got, want)
	}
	if info, err := os.Lstat(autoPublishedPath); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("auto-published file should be copied by default, not symlinked")
	}

	publishedPath := filepath.Join(projectDir, "build", "result.txt")
	if err := Run(ctx, []string{"publish", "demo:upper/result", publishedPath}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(publishedPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "HELLO PIPELINE\n"; got != want {
		t.Fatalf("published content = %q, want %q", got, want)
	}
	if info, err := os.Lstat(publishedPath); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("published file should be copied by default, not symlinked")
	}

	database, err := db.Open(config.DBPath(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	runs, err := database.ListRuns(10, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != "success" {
		t.Fatalf("run status = %q, want success", runs[0].Status)
	}
	artifact, err := database.GetArtifact(runs[0].ID, "upper", "result")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ObjectRef == "" {
		t.Fatal("artifact object ref was empty")
	}
	aliases, err := database.ListAliases()
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) == 0 || aliases[0].Name != "current" {
		t.Fatalf("expected current alias, got %+v", aliases)
	}
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
