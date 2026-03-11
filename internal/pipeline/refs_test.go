package pipeline

import "testing"

func TestParseRunRefWithoutStep(t *testing.T) {
	t.Parallel()

	ref, err := ParseRef("run:20260311_120000")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != RefRun || ref.RunID != "20260311_120000" || ref.Step != "" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
}
