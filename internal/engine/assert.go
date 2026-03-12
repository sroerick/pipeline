package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"pipe/internal/pipeline"
	"pipe/internal/runner"
)

func runAssertStep(_ context.Context, workDir string, step pipeline.Step, inputs []resolvedInput) (runner.Result, error) {
	if step.Assert == nil {
		return runner.Result{}, errors.New("assert step missing assert configuration")
	}
	if len(inputs) != 2 {
		return runner.Result{}, fmt.Errorf("assert step %s requires exactly 2 inputs", step.Name)
	}
	if len(step.Outputs) == 0 {
		return runner.Result{}, fmt.Errorf("assert step %s requires at least one output", step.Name)
	}

	leftLabel := inputLabel(step.Inputs[0])
	rightLabel := inputLabel(step.Inputs[1])
	leftData, err := os.ReadFile(inputs[0].manifest.ResolvedPath)
	if err != nil {
		return runner.Result{}, err
	}
	rightData, err := os.ReadFile(inputs[1].manifest.ResolvedPath)
	if err != nil {
		return runner.Result{}, err
	}

	leftNormalized := normalizeAssertBytes(leftData, step.Assert)
	rightNormalized := normalizeAssertBytes(rightData, step.Assert)
	report := compareReport(leftLabel, rightLabel, leftNormalized, rightNormalized)

	reportPath := filepath.Join(workDir, step.Outputs[0].Path)
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return runner.Result{}, err
	}
	if err := os.WriteFile(reportPath, report, 0o644); err != nil {
		return runner.Result{}, err
	}
	if bytes.Equal(leftNormalized, rightNormalized) {
		return runner.Result{
			Stdout:   []byte(fmt.Sprintf("assert ok: %s == %s\n", leftLabel, rightLabel)),
			ExitCode: 0,
		}, nil
	}
	return runner.Result{
		Stdout:   []byte(fmt.Sprintf("assert failed: %s != %s\n", leftLabel, rightLabel)),
		Stderr:   report,
		ExitCode: 1,
	}, fmt.Errorf("assertion failed: %s != %s", leftLabel, rightLabel)
}

func inputLabel(input pipeline.InputRef) string {
	switch {
	case input.Ref != "":
		return input.Ref
	case input.From != "":
		return input.From
	case input.Source != "":
		return input.Source
	default:
		return "input"
	}
}

func normalizeAssertBytes(data []byte, spec *pipeline.AssertSpec) []byte {
	text := string(data)
	if spec.TrimSpace {
		text = strings.TrimSpace(text)
	}
	if len(spec.IgnoreLinePrefixes) > 0 {
		var kept []string
		for _, line := range strings.Split(text, "\n") {
			ignored := false
			for _, prefix := range spec.IgnoreLinePrefixes {
				if strings.HasPrefix(line, prefix) {
					ignored = true
					break
				}
			}
			if !ignored {
				kept = append(kept, line)
			}
		}
		text = strings.Join(kept, "\n")
	}
	if spec.SortLines {
		lines := strings.Split(text, "\n")
		slices.Sort(lines)
		text = strings.Join(lines, "\n")
	}
	return []byte(text)
}

func compareReport(leftLabel, rightLabel string, left, right []byte) []byte {
	if bytes.Equal(left, right) {
		return []byte(fmt.Sprintf("assert ok: %s == %s\n", leftLabel, rightLabel))
	}
	leftLines := strings.Split(string(left), "\n")
	rightLines := strings.Split(string(right), "\n")
	limit := min(len(leftLines), len(rightLines))
	lineNo := 1
	for ; lineNo <= limit; lineNo++ {
		if leftLines[lineNo-1] != rightLines[lineNo-1] {
			break
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "assert failed: %s != %s\n", leftLabel, rightLabel)
	if lineNo <= limit {
		fmt.Fprintf(&out, "first differing line: %d\n", lineNo)
		fmt.Fprintf(&out, "left : %s\n", leftLines[lineNo-1])
		fmt.Fprintf(&out, "right: %s\n", rightLines[lineNo-1])
	} else {
		fmt.Fprintf(&out, "line count differs: left=%d right=%d\n", len(leftLines), len(rightLines))
	}
	return []byte(out.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
