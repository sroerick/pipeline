package pipeline

import (
	"path/filepath"
	"testing"
)

func TestExampleSpecsLoad(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", "pipe.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no example specs found")
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			t.Parallel()
			if _, err := Load(path); err != nil {
				t.Fatalf("Load(%s): %v", path, err)
			}
		})
	}
}
