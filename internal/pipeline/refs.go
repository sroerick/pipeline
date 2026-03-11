package pipeline

import (
	"fmt"
	"strings"
)

type RefKind string

const (
	RefPipeline RefKind = "pipeline"
	RefRun      RefKind = "run"
	RefAlias    RefKind = "alias"
)

type Ref struct {
	Kind     RefKind
	Pipeline string
	RunID    string
	Step     string
	Output   string
	Alias    string
}

func ParseRef(value string) (Ref, error) {
	if strings.HasPrefix(value, "alias:") {
		name := strings.TrimPrefix(value, "alias:")
		if name == "" {
			return Ref{}, fmt.Errorf("invalid alias ref %q", value)
		}
		return Ref{Kind: RefAlias, Alias: name}, nil
	}
	if strings.HasPrefix(value, "run:") {
		rest := strings.TrimPrefix(value, "run:")
		if !strings.Contains(rest, ":") {
			if rest == "" {
				return Ref{}, fmt.Errorf("invalid run ref %q", value)
			}
			return Ref{Kind: RefRun, RunID: rest}, nil
		}
		runID, remainder, ok := strings.Cut(rest, ":")
		if !ok || runID == "" || remainder == "" {
			return Ref{}, fmt.Errorf("invalid run ref %q", value)
		}
		step, output, _ := strings.Cut(remainder, "/")
		if step == "" {
			return Ref{}, fmt.Errorf("invalid run ref %q", value)
		}
		return Ref{Kind: RefRun, RunID: runID, Step: step, Output: output}, nil
	}
	pipelineName, remainder, ok := strings.Cut(value, ":")
	if !ok || pipelineName == "" || remainder == "" {
		return Ref{}, fmt.Errorf("invalid ref %q", value)
	}
	step, output, _ := strings.Cut(remainder, "/")
	if step == "" {
		return Ref{}, fmt.Errorf("invalid ref %q", value)
	}
	return Ref{Kind: RefPipeline, Pipeline: pipelineName, Step: step, Output: output}, nil
}

func (r Ref) String() string {
	switch r.Kind {
	case RefAlias:
		return "alias:" + r.Alias
	case RefRun:
		if r.Output != "" {
			return fmt.Sprintf("run:%s:%s/%s", r.RunID, r.Step, r.Output)
		}
		return fmt.Sprintf("run:%s:%s", r.RunID, r.Step)
	default:
		if r.Output != "" {
			return fmt.Sprintf("%s:%s/%s", r.Pipeline, r.Step, r.Output)
		}
		return fmt.Sprintf("%s:%s", r.Pipeline, r.Step)
	}
}
